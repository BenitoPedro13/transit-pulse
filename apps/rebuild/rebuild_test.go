package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/BenitoPedro13/transit-pulse/packages/event"
	"github.com/BenitoPedro13/transit-pulse/packages/its"
	"github.com/BenitoPedro13/transit-pulse/packages/raw"
	"github.com/BenitoPedro13/transit-pulse/packages/samples"
	"github.com/BenitoPedro13/transit-pulse/packages/store"
	"github.com/BenitoPedro13/transit-pulse/packages/testharness"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// appendPage stores a captured response body as one attempt, the way the adapter does: the
// header's next_cursor is copied from the body.
func appendPage(t *testing.T, log *raw.Log, minute string, page int, cursor *string, status int, body []byte) {
	t.Helper()
	var pg struct {
		NextCursor *string `json:"next_cursor"`
	}
	if status == 200 {
		if err := json.Unmarshal(body, &pg); err != nil {
			t.Fatal(err)
		}
	}
	fetched, _ := time.Parse(time.RFC3339, minute)
	if _, err := log.Append(raw.Header{
		URL: "https://its.mobilidade.rio/v1/geolocalizacao/veiculos", MinuteUTC: minute, Page: page, Attempt: 1,
		Cursor: cursor, FetchedAt: fetched.Add(2 * time.Minute), Status: status, NextCursor: pg.NextCursor,
	}, body); err != nil {
		t.Fatal(err)
	}
}

// The raw log below is made only of captured provider bytes: the three pages of one real paged
// minute (14:36), page 0 of the same capture as an incomplete minute (14:37) plus the real 422
// body as a failed attempt, and nothing at all for 14:38.
func TestRebuildFromTheRawLog(t *testing.T) {
	const (
		m0 = "2026-10-12T14:36:00Z" // complete: 3 pages
		m1 = "2026-10-12T14:37:00Z" // incomplete
		m2 = "2026-10-12T14:38:00Z" // not in the raw log
	)
	log := raw.Open(t.TempDir(), "its")
	var pages [3][]byte
	for i := range pages {
		pages[i] = samples.MustRead(t, "smtr-its-20261012-paged-p"+string(rune('0'+i))+".json")
	}
	var cursor *string
	for i, body := range pages {
		appendPage(t, log, m0, i, cursor, 200, body)
		var pg struct {
			NextCursor *string `json:"next_cursor"`
		}
		_ = json.Unmarshal(body, &pg)
		cursor = pg.NextCursor
	}
	appendPage(t, log, m1, 0, nil, 200, pages[0])
	var p0 struct {
		NextCursor *string `json:"next_cursor"`
	}
	_ = json.Unmarshal(pages[0], &p0)
	appendPage(t, log, m1, 1, p0.NextCursor, 422, samples.MustRead(t, "smtr-its-20261012-error-422.json"))

	conn := testharness.StartTimescale(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	testharness.SettleRetention(t, conn)

	from, _ := time.Parse(time.RFC3339, m0)
	to, _ := time.Parse(time.RFC3339, m2)
	sum, err := Rebuild(ctx, log, pool, from, to, quiet)
	if err != nil {
		t.Fatal(err)
	}
	// Expected rows: the same mapping function over the same stored envelopes.
	var envs []raw.Envelope
	res, err := log.Scan(from, to, false)
	if err != nil {
		t.Fatal(err)
	}
	chain, ok := res.Index.Chain(m0)
	if !ok || len(chain) != 3 {
		t.Fatalf("test setup: chain of %s has %d pages, complete=%v", m0, len(chain), ok)
	}
	for _, e := range chain {
		env, err := log.Read(e.Loc)
		if err != nil {
			t.Fatal(err)
		}
		envs = append(envs, env)
	}
	want, _, err := its.MinuteEvents(envs)
	if err != nil {
		t.Fatal(err)
	}
	if len(want) != 14 {
		t.Fatalf("test setup: %d events, want 5+5+4", len(want))
	}

	if sum.Minutes != 3 || sum.Complete != 1 || sum.Gaps != 2 || sum.RowsWritten != 14 || sum.StatusesWritten != 12 {
		t.Errorf("summary = %+v, want 3 minutes, 1 complete, 2 gaps, 14 rows, 12 statuses", sum)
	}

	got, err := store.ReadPositions(ctx, pool, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("%d rows, want %d", len(got), len(want))
	}
	byID := map[string]event.Position{}
	for _, e := range want {
		byID[e.Identity()] = e
	}
	for _, g := range got {
		if w, ok := byID[g.Identity()]; !ok || !reflect.DeepEqual(g, w) {
			t.Errorf("row %s differs from the mapped event:\n got  %+v\n want %+v", g.Identity(), g, byID[g.Identity()])
		}
	}

	sts, err := store.ReadFeedStatus(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if len(sts) != 12 {
		t.Fatalf("%d feed_status rows, want 12 (4 feeds x 3 minutes)", len(sts))
	}
	for _, s := range sts {
		switch s.MinuteUTC {
		case m0:
			// The provider said ok for every supplier; only maxtrack reported vehicles. No vehicles
			// is ok with zero records, not a failure and not a gap (invariant 6).
			if s.Status != event.StatusOK {
				t.Errorf("%s %s = %s, want ok", s.Feed, s.MinuteUTC, s.Status)
			}
			if (s.Feed == "its:maxtrack") != (s.Records == 14) || (s.Feed != "its:maxtrack" && s.Records != 0) {
				t.Errorf("%s records = %d", s.Feed, s.Records)
			}
		case m1:
			if s.Status != event.StatusGap || len(s.RawKeys) != 1 {
				t.Errorf("%s %s = %s keys %v, want gap with the key of the attempts", s.Feed, s.MinuteUTC, s.Status, s.RawKeys)
			}
		case m2:
			if s.Status != event.StatusGap || s.RawKeys == nil || len(s.RawKeys) != 0 {
				t.Errorf("%s %s = %s keys %v, want gap with no keys", s.Feed, s.MinuteUTC, s.Status, s.RawKeys)
			}
		}
	}

	// Running it again over the same log changes nothing.
	_, sumRows1, _ := store.Checksum(ctx, pool, "positions")
	_, sumStat1, _ := store.Checksum(ctx, pool, "feed_status")
	again, err := Rebuild(ctx, log, pool, from, to, quiet)
	if err != nil {
		t.Fatal(err)
	}
	_, sumRows2, _ := store.Checksum(ctx, pool, "positions")
	_, sumStat2, _ := store.Checksum(ctx, pool, "feed_status")
	if again.RowsWritten != 0 || again.StatusesWritten != 0 || sumRows1 != sumRows2 || sumStat1 != sumStat2 {
		t.Errorf("second run wrote %d rows and %d statuses, or the checksums changed", again.RowsWritten, again.StatusesWritten)
	}
}

func TestParseRange(t *testing.T) {
	for _, c := range []struct {
		from, to string
		ok       bool
	}{
		{"2026-10-12T14:27:00Z", "2026-10-12T16:24:00Z", true},
		{"2026-10-12T14:27:00Z", "2026-10-12T14:27:00Z", true},
		{"2026-10-12T14:27:30Z", "2026-10-12T16:24:00Z", false}, // not a minute boundary
		{"2026-10-12T16:24:00Z", "2026-10-12T14:27:00Z", false}, // reversed
		{"", "2026-10-12T14:27:00Z", false},
		{"yesterday", "2026-10-12T14:27:00Z", false},
	} {
		if _, _, err := parseRange(c.from, c.to); (err == nil) != c.ok {
			t.Errorf("parseRange(%q, %q) error = %v, want ok=%v", c.from, c.to, err, c.ok)
		}
	}
}
