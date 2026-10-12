package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// windowSeconds is how old a fix may be and still be shown, counted back from the end of the
// newest minute with positions. It is sent as window_seconds so a reader never has to guess.
const windowSeconds = 300

// querier is the part of pgxpool.Pool the API uses. Tests wrap it to count queries.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// feedRow is the newest feed_status row of one feed, plus the newest minute of that feed that has
// positions (ok or provider_not_ok). The refresher compares these to see whether anything moved.
type feedRow struct {
	Feed          string
	Operator      string
	Minute        time.Time
	Status        string
	LastPositions *time.Time
}

func (a feedRow) equal(b feedRow) bool {
	same := func(x, y *time.Time) bool {
		if x == nil || y == nil {
			return x == y
		}
		return x.Equal(*y)
	}
	return a.Feed == b.Feed && a.Operator == b.Operator && a.Minute.Equal(b.Minute) &&
		a.Status == b.Status && same(a.LastPositions, b.LastPositions)
}

// The probe is tiny: one row per feed, from the primary key (feed, minute_utc). It runs every
// refresh interval whether or not anyone is reading.
const stateQuery = `
SELECT s.feed, s.operator, s.minute_utc, s.status,
       (SELECT max(p.minute_utc) FROM feed_status p
         WHERE p.feed = s.feed AND p.status IN ('ok', 'provider_not_ok'))
FROM (SELECT DISTINCT ON (feed) feed, operator, minute_utc, status
        FROM feed_status ORDER BY feed, minute_utc DESC) s
ORDER BY s.feed COLLATE "C"`

func readState(ctx context.Context, db querier) ([]feedRow, error) {
	rows, err := db.Query(ctx, stateQuery)
	if err != nil {
		return nil, fmt.Errorf("read feed status: %w", err)
	}
	defer rows.Close()
	var out []feedRow
	for rows.Next() {
		var r feedRow
		if err := rows.Scan(&r.Feed, &r.Operator, &r.Minute, &r.Status, &r.LastPositions); err != nil {
			return nil, fmt.Errorf("read feed status: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read feed status: %w", err)
	}
	return out, nil
}

func sameState(a, b []feedRow) bool {
	return slices.EqualFunc(a, b, feedRow.equal)
}

// asOfMinute is the newest minute that has positions in any feed. A newer failed or gap minute
// does not move it: that shows up in coverage, never as "zero vehicles" (invariant 6).
func asOfMinute(state []feedRow) (time.Time, error) {
	var asOf time.Time
	for _, r := range state {
		if r.LastPositions != nil && r.LastPositions.After(asOf) {
			asOf = *r.LastPositions
		}
	}
	if asOf.IsZero() {
		return time.Time{}, errors.New("no minute with positions in feed_status yet")
	}
	return asOf.UTC(), nil
}

// The final feed ordering only makes the choice deterministic when two feeds report the same
// vehicle at the same instant; nothing interprets its value (architecture §10 #9).
const vehiclesQuery = `
SELECT DISTINCT ON (operator COLLATE "C", vehicle COLLATE "C")
       operator, mode, vehicle, line, lat, lon, event_time, speed_kmh, heading_deg
FROM positions
WHERE event_time > $1 AND event_time <= $2
ORDER BY operator COLLATE "C", vehicle COLLATE "C", event_time DESC, received_time DESC, feed COLLATE "C"`

// vehicleJSON is one row of the response. Pointers keep null apart from zero.
type vehicleJSON struct {
	Operator   string    `json:"operator"`
	Mode       string    `json:"mode"`
	Vehicle    string    `json:"vehicle"`
	Line       *string   `json:"line"`
	Lat        float64   `json:"lat"`
	Lon        float64   `json:"lon"`
	EventTime  time.Time `json:"event_time"`
	SpeedKmh   *float64  `json:"speed_kmh"`
	HeadingDeg *float64  `json:"heading_deg"`
}

type coverageJSON struct {
	Operator      string         `json:"operator"`
	Label         string         `json:"label"`
	Mode          string         `json:"mode"`
	Tier          string         `json:"tier"`
	MinuteUTC     time.Time      `json:"minute_utc"`
	FeedsTotal    int            `json:"feeds_total"`
	FeedsByStatus map[string]int `json:"feeds_by_status"`
}

type latestJSON struct {
	AsOfMinute    time.Time      `json:"as_of_minute"`
	GeneratedAt   time.Time      `json:"generated_at"`
	WindowSeconds int            `json:"window_seconds"`
	Coverage      []coverageJSON `json:"coverage"`
	Vehicles      []vehicleJSON  `json:"vehicles"`
}

// Snapshot is one answer, fully encoded. Requests only ever copy these bytes.
type Snapshot struct {
	AsOfMinute  time.Time
	GeneratedAt time.Time
	Vehicles    int
	ETag        string
	JSON        []byte
	Gzip        []byte
}

// coverageOf builds one entry per operator for its newest minute. feeds_total counts the feeds
// known for the operator; feeds_by_status counts those with a row at that minute, so a feed that is
// behind shows as a sum below the total instead of as a made-up status. feed itself is never sent.
func coverageOf(state []feedRow) ([]coverageJSON, error) {
	type group struct {
		info  operatorInfo
		feeds []feedRow
	}
	groups := map[string]*group{}
	for _, r := range state {
		g := groups[r.Operator]
		if g == nil {
			info, err := operatorOf(r.Operator)
			if err != nil {
				return nil, err
			}
			g = &group{info: info}
			groups[r.Operator] = g
		}
		g.feeds = append(g.feeds, r)
	}
	out := make([]coverageJSON, 0, len(groups))
	for id, g := range groups {
		var newest time.Time
		for _, f := range g.feeds {
			if f.Minute.After(newest) {
				newest = f.Minute
			}
		}
		c := coverageJSON{
			Operator: id, Label: g.info.Label, Mode: g.info.Mode, Tier: g.info.Tier,
			MinuteUTC: newest.UTC(), FeedsTotal: len(g.feeds), FeedsByStatus: map[string]int{},
		}
		for _, f := range g.feeds {
			if f.Minute.Equal(newest) {
				c.FeedsByStatus[f.Status]++
			}
		}
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b coverageJSON) int {
		return operators[a.Operator].rank - operators[b.Operator].rank
	})
	return out, nil
}

// compute runs the one snapshot query and encodes the result. It is the only place the API reads
// positions.
func compute(ctx context.Context, db querier, state []feedRow, now time.Time) (*Snapshot, error) {
	asOf, err := asOfMinute(state)
	if err != nil {
		return nil, err
	}
	coverage, err := coverageOf(state)
	if err != nil {
		return nil, err
	}
	to := asOf.Add(time.Minute)
	from := to.Add(-windowSeconds * time.Second)

	rows, err := db.Query(ctx, vehiclesQuery, from, to)
	if err != nil {
		return nil, fmt.Errorf("read vehicles: %w", err)
	}
	defer rows.Close()
	vehicles := []vehicleJSON{} // [] and not null when empty
	for rows.Next() {
		var v vehicleJSON
		if err := rows.Scan(&v.Operator, &v.Mode, &v.Vehicle, &v.Line, &v.Lat, &v.Lon, &v.EventTime,
			&v.SpeedKmh, &v.HeadingDeg); err != nil {
			return nil, fmt.Errorf("read vehicles: %w", err)
		}
		v.EventTime = v.EventTime.UTC()
		vehicles = append(vehicles, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read vehicles: %w", err)
	}
	return encode(asOf, now, coverage, vehicles)
}

func encode(asOf, now time.Time, coverage []coverageJSON, vehicles []vehicleJSON) (*Snapshot, error) {
	generated := now.UTC().Truncate(time.Second)
	body, err := json.Marshal(latestJSON{
		AsOfMinute: asOf, GeneratedAt: generated, WindowSeconds: windowSeconds,
		Coverage: coverage, Vehicles: vehicles,
	})
	if err != nil {
		return nil, fmt.Errorf("encode snapshot: %w", err)
	}
	var zipped bytes.Buffer
	zw, err := gzip.NewWriterLevel(&zipped, gzip.BestCompression) // once a minute, so spend the CPU
	if err != nil {
		return nil, err
	}
	if _, err := zw.Write(body); err != nil {
		return nil, fmt.Errorf("gzip snapshot: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("gzip snapshot: %w", err)
	}
	return &Snapshot{
		AsOfMinute: asOf, GeneratedAt: generated, Vehicles: len(vehicles),
		// Weak: the same snapshot is served as plain and as gzip bytes.
		ETag: `W/"` + asOf.Format(time.RFC3339) + `.` + generated.Format(time.RFC3339) + `"`,
		JSON: body, Gzip: zipped.Bytes(),
	}, nil
}

// etagMatches implements If-None-Match for GET: weak comparison against a comma-separated list.
func etagMatches(header, etag string) bool {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "*" || strings.TrimPrefix(part, "W/") == strings.TrimPrefix(etag, "W/") {
			return true
		}
	}
	return false
}

// acceptsGzip reports whether Accept-Encoding allows gzip (an explicit q=0 refuses it).
func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(name), "gzip") {
			continue
		}
		q := strings.ReplaceAll(strings.ToLower(params), " ", "")
		return q != "q=0" && q != "q=0.0" && q != "q=0.00" && q != "q=0.000"
	}
	return false
}
