package store_test

import (
	"cmp"
	"context"
	"reflect"
	"slices"
	"strings"
	"sync"
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

const (
	minute0 = "2026-10-12T13:38:00Z"
	minute1 = "2026-10-12T13:39:00Z"
)

var suppliers = []string{"conecta", "zirix", "maxtrack", "sonda"}

// minuteOf maps the four captured samples as if they were the single page of the given raw
// minute. The bodies are the real provider bytes, untouched; only the header's minute and
// fetched_at say when "we got them".
func minuteOf(t *testing.T, minute string) ([]event.Position, []event.FeedStatus) {
	t.Helper()
	at, err := time.Parse(time.RFC3339, minute)
	if err != nil {
		t.Fatal(err)
	}
	var all []event.Position
	var statuses []event.FeedStatus
	for _, s := range suppliers {
		body := samples.MustRead(t, "smtr-its-20261012-veiculos-"+s+".json")
		env := raw.Envelope{
			Header: raw.Header{V: 1, Source: "its", MinuteUTC: minute, Page: 0, Status: 200,
				FetchedAt: at.Add(90 * time.Second), BodyLen: len(body)},
			Body: body,
		}
		evs, st, err := its.MinuteEvents([]raw.Envelope{env})
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, evs...)
		statuses = append(statuses, its.MinuteStatuses(minute, st, []string{raw.HourKey("its", at)})...)
	}
	return all, statuses
}

func newPool(t *testing.T, connString string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), connString)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// migrated returns a pool on a fresh, migrated database inside the shared container.
func migrated(t *testing.T, container, name string) *pgxpool.Pool {
	t.Helper()
	connString := testharness.NewDatabase(t, container, name)
	pool := newPool(t, connString)
	if err := store.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	testharness.SettleRetention(t, connString)
	return pool
}

func TestStore(t *testing.T) {
	container := testharness.StartTimescale(t)
	ctx := context.Background()

	// E1
	t.Run("migrate", func(t *testing.T) {
		pool := newPool(t, testharness.NewDatabase(t, container, "migrate"))

		// Two processes migrating a fresh database at once: the session lock serialises them.
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i := range errs {
			wg.Add(1)
			go func() { defer wg.Done(); errs[i] = store.Migrate(ctx, pool) }()
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatalf("concurrent migrate: %v", err)
			}
		}

		count := func() (n int) {
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM goose_db_version`).Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n
		}
		before := count()
		if err := store.Migrate(ctx, pool); err != nil {
			t.Fatal(err)
		}
		if after := count(); after != before || before != 3 { // version 0 + two migrations
			t.Errorf("goose_db_version rows: %d then %d, want 3 and unchanged", before, after)
		}

		var interval, column string
		if err := pool.QueryRow(ctx, `SELECT column_name, time_interval::text FROM timescaledb_information.dimensions
			WHERE hypertable_name = 'positions'`).Scan(&column, &interval); err != nil {
			t.Fatalf("positions is not a hypertable: %v", err)
		}
		if column != "event_time" || interval != "1 day" {
			t.Errorf("dimension = %s every %s, want event_time every 1 day", column, interval)
		}
		var dropAfter string
		if err := pool.QueryRow(ctx, `SELECT config->>'drop_after' FROM timescaledb_information.jobs
			WHERE proc_name = 'policy_retention' AND hypertable_name = 'positions'`).Scan(&dropAfter); err != nil {
			t.Fatalf("no retention job on positions: %v", err)
		}
		if dropAfter != "3 days" {
			t.Errorf("retention drop_after = %q, want 3 days", dropAfter)
		}
		var compressed bool
		if err := pool.QueryRow(ctx, `SELECT compression_enabled FROM timescaledb_information.hypertables
			WHERE hypertable_name = 'positions'`).Scan(&compressed); err != nil || compressed {
			t.Errorf("compression_enabled = %v (err %v), want false (TASK-positions-consumer §2.1 #2)", compressed, err)
		}
		var feedIsHypertable int
		_ = pool.QueryRow(ctx, `SELECT count(*) FROM timescaledb_information.hypertables WHERE hypertable_name = 'feed_status'`).Scan(&feedIsHypertable)
		if feedIsHypertable != 0 {
			t.Error("feed_status must be a plain table")
		}
		for _, col := range []string{"speed_kmh", "heading_deg"} {
			var typ string
			if err := pool.QueryRow(ctx, `SELECT data_type FROM information_schema.columns
				WHERE table_name = 'positions' AND column_name = $1`, col).Scan(&typ); err != nil || typ != "double precision" {
				t.Errorf("%s is %q (err %v), want double precision: the provider sends fractional values", col, typ, err)
			}
		}
	})

	t.Run("a row round-trips and a second write changes nothing", func(t *testing.T) {
		pool := migrated(t, container, "roundtrip")
		evs, _ := minuteOf(t, minute0)

		n, err := store.WritePositions(ctx, pool, evs)
		if err != nil || n != int64(len(evs)) {
			t.Fatalf("first write = %d, %v; want %d", n, err, len(evs))
		}
		_, sum1, _ := store.Checksum(ctx, pool, "positions")
		n, err = store.WritePositions(ctx, pool, evs)
		if err != nil || n != 0 {
			t.Errorf("second write = %d, %v; want 0 rows written", n, err)
		}
		rows, sum2, _ := store.Checksum(ctx, pool, "positions")
		if sum1 != sum2 || rows != int64(len(evs)) {
			t.Errorf("checksum changed or wrong count: %d rows, %s then %s", rows, sum1, sum2)
		}

		got, err := store.ReadPositions(ctx, pool, time.Time{}, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		want := slices.Clone(evs)
		slices.SortFunc(want, func(a, b event.Position) int { return compareKey(a, b) })
		if !reflect.DeepEqual(got, want) {
			t.Errorf("rows read back differ from the events written (first difference: %s)", firstDiff(got, want))
		}
		// Nulls stay nulls: the samples have null lines, route ids and one null heading.
		var nulls int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM positions WHERE line IS NULL OR route_id IS NULL OR heading_deg IS NULL`).Scan(&nulls); err != nil || nulls == 0 {
			t.Errorf("no null line/route/heading rows stored (count %d, err %v)", nulls, err)
		}
	})

	// E3, store level
	t.Run("order independence across minutes", func(t *testing.T) {
		a := migrated(t, container, "order_a")
		b := migrated(t, container, "order_b")
		ev0, st0 := minuteOf(t, minute0)
		ev1, st1 := minuteOf(t, minute1)

		// The same real bodies under two minutes: every identity of minute 0 repeats in minute 1,
		// with a different raw_minute and received_time (this is what the provider does for 4.6%).
		ids := map[string]bool{}
		for _, e := range ev0 {
			ids[e.Identity()] = true
		}
		repeats := 0
		for _, e := range ev1 {
			if ids[e.Identity()] {
				repeats++
			}
		}
		if repeats == 0 || repeats != len(ev1) {
			t.Fatalf("%d of %d identities repeat across the two minutes, want all", repeats, len(ev1))
		}

		write := func(pool *pgxpool.Pool, evs []event.Position, sts []event.FeedStatus) (int64, int64) {
			t.Helper()
			n, err := store.WritePositions(ctx, pool, evs)
			if err != nil {
				t.Fatal(err)
			}
			m, err := store.WriteFeedStatus(ctx, pool, sts)
			if err != nil {
				t.Fatal(err)
			}
			return n, m
		}
		// A: minute order. B: reverse order, so the later copy arrives first and is replaced.
		nA0, _ := write(a, ev0, st0)
		nA1, _ := write(a, ev1, st1)
		nB1, _ := write(b, ev1, st1)
		nB0, _ := write(b, ev0, st0)
		if nA0 != int64(len(ev0)) || nA1 != 0 || nB1 != int64(len(ev1)) || nB0 != int64(len(ev0)) {
			t.Errorf("rows written: A %d then %d, B %d then %d; want %d, 0, %d, %d (B replaces the later copy)",
				nA0, nA1, nB1, nB0, len(ev0), len(ev1), len(ev0))
		}

		for _, name := range []string{"positions", "feed_status"} {
			ra, sa, _ := store.Checksum(ctx, a, name)
			rb, sb, _ := store.Checksum(ctx, b, name)
			if ra != rb || sa != sb {
				t.Errorf("%s differs: A %d rows %s, B %d rows %s", name, ra, sa, rb, sb)
			}
		}
		pa, _ := store.ReadPositions(ctx, a, time.Time{}, time.Time{})
		pb, _ := store.ReadPositions(ctx, b, time.Time{}, time.Time{})
		if !reflect.DeepEqual(pa, pb) {
			t.Errorf("positions differ on some column (first difference: %s)", firstDiff(pa, pb))
		}
		fa, _ := store.ReadFeedStatus(ctx, a)
		fb, _ := store.ReadFeedStatus(ctx, b)
		if !reflect.DeepEqual(fa, fb) || len(fa) != 8 {
			t.Errorf("feed_status differs or has %d rows, want 8 (4 feeds x 2 minutes)", len(fa))
		}
		// The kept copy is the one from the earlier raw minute, in both databases.
		for _, p := range append(pa, pb...) {
			if p.RawRef.MinuteUTC != minute0 {
				t.Fatalf("%s kept raw_minute %s, want %s", p.Identity(), p.RawRef.MinuteUTC, minute0)
			}
		}
		if len(pa) != len(ev0) {
			t.Errorf("%d rows, want %d: one per identity", len(pa), len(ev0))
		}
	})

	t.Run("feed status: a complete chain is final, gap beats failed", func(t *testing.T) {
		_, ok := minuteOf(t, minute0)
		ok = ok[:4] // one message per mapped feed for minute0
		failed := its.EmptyStatuses(minute0, event.StatusFailed, []string{"k"})
		gap := its.EmptyStatuses(minute0, event.StatusGap, []string{"k"})

		cases := []struct {
			name string
			seq  [][]event.FeedStatus
			want string
		}{
			{"failed then ok", [][]event.FeedStatus{failed, ok}, event.StatusOK},
			{"ok then failed", [][]event.FeedStatus{ok, failed}, event.StatusOK},
			{"ok then gap", [][]event.FeedStatus{ok, gap}, event.StatusOK},
			{"failed then gap", [][]event.FeedStatus{failed, gap}, event.StatusGap},
			{"gap then failed", [][]event.FeedStatus{gap, failed}, event.StatusGap},
		}
		for i, c := range cases {
			pool := migrated(t, container, "status_"+string(rune('a'+i)))
			for _, batch := range c.seq {
				if _, err := store.WriteFeedStatus(ctx, pool, batch); err != nil {
					t.Fatalf("%s: %v", c.name, err)
				}
			}
			got, _ := store.ReadFeedStatus(ctx, pool)
			if len(got) != 4 {
				t.Fatalf("%s: %d rows, want 4", c.name, len(got))
			}
			for _, s := range got {
				if s.Status != c.want && (c.want != event.StatusOK || s.Status != event.StatusProviderNotOK) {
					t.Errorf("%s: %s is %s, want %s", c.name, s.Feed, s.Status, c.want)
				}
			}
			// Writing the last batch again changes nothing.
			if n, err := store.WriteFeedStatus(ctx, pool, c.seq[len(c.seq)-1]); err != nil || n != 0 {
				t.Errorf("%s: rewriting the last batch wrote %d rows (err %v), want 0", c.name, n, err)
			}
		}

		// A batch holding the same (feed, minute) twice must not fail the statement.
		pool := migrated(t, container, "status_dup")
		if _, err := store.WriteFeedStatus(ctx, pool, slices.Concat(failed, failed, ok)); err != nil {
			t.Errorf("batch with repeated keys: %v", err)
		}
	})
}

func compareKey(a, b event.Position) int {
	return cmp.Or(
		strings.Compare(a.Operator, b.Operator),
		strings.Compare(a.Feed, b.Feed),
		strings.Compare(a.Vehicle, b.Vehicle),
		a.EventTime.Compare(b.EventTime),
	)
}

func firstDiff(got, want []event.Position) string {
	if len(got) != len(want) {
		return "row counts differ"
	}
	for i := range got {
		if !reflect.DeepEqual(got[i], want[i]) {
			return got[i].Identity() + " vs " + want[i].Identity()
		}
	}
	return "none"
}
