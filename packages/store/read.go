package store

import (
	"context"
	"fmt"
	"time"

	"github.com/BenitoPedro13/transit-pulse/packages/event"
)

// ReadPositions returns the rows of positions as events, ordered by primary key (bytewise, so the
// order does not depend on the database collation). A non-zero from
// or to bounds raw_minute (from inclusive, to exclusive). It exists for tests and for comparing
// two databases, not for serving readers.
func ReadPositions(ctx context.Context, q Querier, from, to time.Time) ([]event.Position, error) {
	rows, err := q.Query(ctx, `
SELECT operator, feed, vehicle, event_time, city, mode, authority, source_kind, line, route_id,
       lat, lon, speed_kmh, heading_deg, received_time, raw_key, raw_minute, raw_page
FROM positions
WHERE ($1::timestamptz IS NULL OR raw_minute >= $1) AND ($2::timestamptz IS NULL OR raw_minute < $2)
ORDER BY operator COLLATE "C", feed COLLATE "C", vehicle COLLATE "C", event_time`, nullTime(from), nullTime(to))
	if err != nil {
		return nil, fmt.Errorf("store: read positions: %w", err)
	}
	defer rows.Close()
	var out []event.Position
	for rows.Next() {
		var p event.Position
		var rawMinute time.Time
		if err := rows.Scan(&p.Operator, &p.Feed, &p.Vehicle, &p.EventTime, &p.City, &p.Mode, &p.Authority,
			&p.SourceKind, &p.Line, &p.RouteID, &p.Lat, &p.Lon, &p.SpeedKmh, &p.HeadingDeg, &p.ReceivedTime,
			&p.RawRef.Key, &rawMinute, &p.RawRef.Page); err != nil {
			return nil, fmt.Errorf("store: read positions: %w", err)
		}
		p.EventTime, p.ReceivedTime = p.EventTime.UTC(), p.ReceivedTime.UTC()
		p.RawRef.MinuteUTC = rawMinute.UTC().Format(minuteLayout)
		out = append(out, p)
	}
	return out, rows.Err()
}

// ReadFeedStatus returns the rows of feed_status as messages, ordered by primary key.
func ReadFeedStatus(ctx context.Context, q Querier) ([]event.FeedStatus, error) {
	rows, err := q.Query(ctx, `
SELECT feed, minute_utc, operator, status, records, events, invalid, unmapped, conflicting_duplicates,
       provider_status, raw_keys
FROM feed_status ORDER BY feed COLLATE "C", minute_utc`)
	if err != nil {
		return nil, fmt.Errorf("store: read feed status: %w", err)
	}
	defer rows.Close()
	var out []event.FeedStatus
	for rows.Next() {
		var s event.FeedStatus
		var minute time.Time
		if err := rows.Scan(&s.Feed, &minute, &s.Operator, &s.Status, &s.Records, &s.Events, &s.Invalid,
			&s.Unmapped, &s.ConflictingDuplicates, &s.ProviderStatus, &s.RawKeys); err != nil {
			return nil, fmt.Errorf("store: read feed status: %w", err)
		}
		s.MinuteUTC = minute.UTC().Format(minuteLayout)
		out = append(out, s)
	}
	return out, rows.Err()
}

// Checksum is an md5 over every column of every row of a table, in primary-key order, plus the
// row count. Two tables with the same checksum hold the same data. table is "positions" or
// "feed_status".
func Checksum(ctx context.Context, q Querier, table string) (rows int64, sum string, err error) {
	var orderBy string
	switch table {
	case "positions":
		orderBy = `t.operator COLLATE "C", t.feed COLLATE "C", t.vehicle COLLATE "C", t.event_time`
	case "feed_status":
		orderBy = `t.feed COLLATE "C", t.minute_utc`
	default:
		return 0, "", fmt.Errorf("store: checksum: unknown table %q", table)
	}
	// A row's text form lists every column, writes NULL as nothing and '' as "", and prints floats
	// exactly, so any difference in any column changes the md5.
	err = q.QueryRow(ctx, fmt.Sprintf(
		`SELECT count(*), coalesce(md5(string_agg(t::text, E'\n' ORDER BY %s)), '') FROM %s t`, orderBy, table)).Scan(&rows, &sum)
	return rows, sum, err
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
