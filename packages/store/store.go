// Package store owns the database shape (migrations) and the two writes that fill it, so the
// consumer and the rebuild cannot drift apart.
//
// Both writes are idempotent and independent of the order they are called in: writing the same
// events or statuses twice, or in any order, leaves the same table (invariants 2 and 3).
//
//   - positions keeps one row per identity (operator, feed, vehicle, event_time). When the
//     provider re-serves a fix in a later minute, the copy from the earliest raw minute is kept.
//   - feed_status keeps one row per (feed, minute). A complete chain (ok, provider_not_ok) is
//     final; gap beats failed; otherwise the latest message wins.
package store

import (
	"cmp"
	"context"
	"embed"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/BenitoPedro13/transit-pulse/packages/event"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies the pending migrations. It is safe to call from several processes at once: the
// goose provider holds a Postgres session advisory lock while it migrates. The extensions are
// created by infra/postgres/initdb, not here.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("store: migration locker: %w", err)
	}
	db := stdlib.OpenDBFromPool(pool) // closing db does not close the pool
	defer func() { _ = db.Close() }()
	p, err := goose.NewProvider(goose.DialectPostgres, db, sub, goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("store: goose provider: %w", err)
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	return nil
}

// Querier is what pgxpool.Pool and pgx.Tx have in common.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
}

// insertChunk is how many rows one INSERT ... SELECT FROM unnest statement carries.
const insertChunk = 5000

// minuteLayout is the format of event.RawRef.MinuteUTC.
const minuteLayout = "2006-01-02T15:04:05Z"

type positionRow struct {
	p         event.Position
	rawMinute time.Time
}

func cmpRows(a, b positionRow) int {
	return cmp.Or(
		strings.Compare(a.p.Operator, b.p.Operator),
		strings.Compare(a.p.Feed, b.p.Feed),
		strings.Compare(a.p.Vehicle, b.p.Vehicle),
		a.p.EventTime.Compare(b.p.EventTime),
	)
}

// dedupe keeps the copy with the earliest raw minute per identity (the first one on a tie). One
// INSERT ... ON CONFLICT DO UPDATE must not touch a row twice. The result is sorted by primary
// key, so concurrent writers lock rows in the same order.
func dedupe(ps []event.Position) ([]positionRow, error) {
	best := make(map[string]positionRow, len(ps))
	for _, p := range ps {
		m, err := time.Parse(time.RFC3339, p.RawRef.MinuteUTC)
		if err != nil {
			return nil, fmt.Errorf("store: position %s: raw_ref.minute_utc %q: %w", p.Identity(), p.RawRef.MinuteUTC, err)
		}
		id := p.Identity()
		if cur, ok := best[id]; !ok || m.Before(cur.rawMinute) {
			best[id] = positionRow{p: p, rawMinute: m.UTC()}
		}
	}
	rows := make([]positionRow, 0, len(best))
	for _, r := range best {
		rows = append(rows, r)
	}
	slices.SortFunc(rows, cmpRows)
	return rows, nil
}

const upsertPositions = `
INSERT INTO positions (operator, feed, vehicle, event_time, city, mode, authority, source_kind,
                       line, route_id, lat, lon, speed_kmh, heading_deg,
                       received_time, raw_key, raw_minute, raw_page)
SELECT * FROM unnest($1::text[], $2::text[], $3::text[], $4::timestamptz[], $5::text[], $6::text[],
                     $7::text[], $8::text[], $9::text[], $10::text[], $11::float8[], $12::float8[],
                     $13::float8[], $14::float8[], $15::timestamptz[], $16::text[],
                     $17::timestamptz[], $18::int4[])
ON CONFLICT (operator, feed, vehicle, event_time) DO UPDATE SET
  city = excluded.city, mode = excluded.mode, authority = excluded.authority,
  source_kind = excluded.source_kind, line = excluded.line, route_id = excluded.route_id,
  lat = excluded.lat, lon = excluded.lon, speed_kmh = excluded.speed_kmh,
  heading_deg = excluded.heading_deg, received_time = excluded.received_time,
  raw_key = excluded.raw_key, raw_minute = excluded.raw_minute, raw_page = excluded.raw_page
WHERE excluded.raw_minute < positions.raw_minute`

// WritePositions upserts the events and returns how many rows were inserted or replaced. A
// repeat of an identity already stored changes nothing unless it comes from an earlier raw
// minute, in which case that copy replaces the stored one. The result therefore does not depend
// on the order events arrive in.
func WritePositions(ctx context.Context, q Querier, ps []event.Position) (written int64, err error) {
	rows, err := dedupe(ps)
	if err != nil {
		return 0, err
	}
	for chunk := range slices.Chunk(rows, insertChunk) {
		var (
			operator, feed, vehicle, city, mode, authority, sourceKind, rawKey []string
			line, routeID                                                      []*string
			eventTime, receivedTime, rawMinute                                 []time.Time
			lat, lon                                                           []float64
			speed, heading                                                     []*float64
			rawPage                                                            []int32
		)
		for _, r := range chunk {
			p := r.p
			operator, feed, vehicle = append(operator, p.Operator), append(feed, p.Feed), append(vehicle, p.Vehicle)
			city, mode, authority = append(city, p.City), append(mode, p.Mode), append(authority, p.Authority)
			sourceKind, rawKey = append(sourceKind, p.SourceKind), append(rawKey, p.RawRef.Key)
			line, routeID = append(line, p.Line), append(routeID, p.RouteID)
			eventTime, receivedTime = append(eventTime, p.EventTime.UTC()), append(receivedTime, p.ReceivedTime.UTC())
			rawMinute = append(rawMinute, r.rawMinute)
			lat, lon = append(lat, p.Lat), append(lon, p.Lon)
			speed, heading = append(speed, p.SpeedKmh), append(heading, p.HeadingDeg)
			rawPage = append(rawPage, int32(p.RawRef.Page))
		}
		tag, err := q.Exec(ctx, upsertPositions, operator, feed, vehicle, eventTime, city, mode, authority,
			sourceKind, line, routeID, lat, lon, speed, heading, receivedTime, rawKey, rawMinute, rawPage)
		if err != nil {
			return written, fmt.Errorf("store: write positions: %w", err)
		}
		written += tag.RowsAffected()
	}
	return written, nil
}

// statusRank orders (upsertFeedStatus repeats the same ranking in SQL) the feed-status values: a complete chain is final, a gap is the end state of
// a minute that was never captured, and failed is the state of a minute still being retried.
func statusRank(s string) int {
	switch s {
	case event.StatusOK, event.StatusProviderNotOK:
		return 3
	case event.StatusGap:
		return 2
	default: // failed
		return 1
	}
}

const upsertFeedStatus = `
INSERT INTO feed_status (feed, minute_utc, operator, status, records, events, invalid, unmapped,
                         conflicting_duplicates, provider_status, raw_keys)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (feed, minute_utc) DO UPDATE SET
  operator = excluded.operator, status = excluded.status, records = excluded.records,
  events = excluded.events, invalid = excluded.invalid, unmapped = excluded.unmapped,
  conflicting_duplicates = excluded.conflicting_duplicates,
  provider_status = excluded.provider_status, raw_keys = excluded.raw_keys
WHERE (CASE excluded.status WHEN 'ok' THEN 3 WHEN 'provider_not_ok' THEN 3 WHEN 'gap' THEN 2 ELSE 1 END)
   >= (CASE feed_status.status WHEN 'ok' THEN 3 WHEN 'provider_not_ok' THEN 3 WHEN 'gap' THEN 2 ELSE 1 END)
  AND excluded IS DISTINCT FROM feed_status`

// WriteFeedStatus upserts the statuses and returns how many rows were inserted or changed. The
// message with the higher rank wins (complete chain over gap over failed); among equal ranks the
// latest wins. Writing the same messages again changes nothing.
func WriteFeedStatus(ctx context.Context, q Querier, sts []event.FeedStatus) (written int64, err error) {
	// One statement must not touch a row twice, so fold the batch first, in message order.
	type key struct{ feed, minute string }
	folded := map[key]event.FeedStatus{}
	var order []key
	for _, s := range sts {
		k := key{s.Feed, s.MinuteUTC}
		cur, ok := folded[k]
		if !ok {
			order = append(order, k)
		}
		if !ok || statusRank(s.Status) >= statusRank(cur.Status) {
			folded[k] = s
		}
	}
	slices.SortFunc(order, func(a, b key) int {
		if c := strings.Compare(a.feed, b.feed); c != 0 {
			return c
		}
		return strings.Compare(a.minute, b.minute)
	})

	batch := &pgx.Batch{}
	for _, k := range order {
		s := folded[k]
		minute, err := time.Parse(time.RFC3339, s.MinuteUTC)
		if err != nil {
			return 0, fmt.Errorf("store: feed status %s: minute_utc %q: %w", s.Key(), s.MinuteUTC, err)
		}
		keys := s.RawKeys
		if keys == nil {
			keys = []string{}
		}
		batch.Queue(upsertFeedStatus, s.Feed, minute.UTC(), s.Operator, s.Status, s.Records, s.Events, s.Invalid,
			s.Unmapped, s.ConflictingDuplicates, s.ProviderStatus, keys)
	}
	if batch.Len() == 0 {
		return 0, nil
	}
	br := q.SendBatch(ctx, batch)
	defer func() {
		if cerr := br.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("store: write feed status: %w", cerr)
		}
	}()
	for range batch.Len() {
		tag, err := br.Exec()
		if err != nil {
			return written, fmt.Errorf("store: write feed status: %w", err)
		}
		written += tag.RowsAffected()
	}
	return written, nil
}
