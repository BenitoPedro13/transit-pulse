package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/BenitoPedro13/transit-pulse/packages/contracts"
	"github.com/BenitoPedro13/transit-pulse/packages/event"
	"github.com/BenitoPedro13/transit-pulse/packages/its"
	"github.com/BenitoPedro13/transit-pulse/packages/store"
	"github.com/BenitoPedro13/transit-pulse/packages/testharness"
)

const schemaName = "history-hour.v1.schema.json"

func hourPath(h string) string { return "/v1/history/" + h }

func fetchHour(t *testing.T, h http.Handler, hour string) (hourJSON, []byte, *httptest.ResponseRecorder) {
	t.Helper()
	rec := get(t, h, hourPath(hour))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", hourPath(hour), rec.Code, rec.Body)
	}
	var out hourJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v", hour, err)
	}
	return out, rec.Body.Bytes(), rec
}

// checkHour validates a served file against the schema, plus what JSON Schema cannot say: equal
// column lengths, fixes ordered by (v, t), indexes inside the vehicle table.
func checkHour(t *testing.T, body []byte) {
	t.Helper()
	contracts.MustValidate(t, schemaName, body)
	var f hourJSON
	if err := json.Unmarshal(body, &f); err != nil {
		t.Fatal(err)
	}
	if err := hourConsistency(f); err != nil {
		t.Error(err)
	}
}

func hourConsistency(f hourJSON) error {
	n := len(f.Fixes.V)
	if len(f.Fixes.T) != n || len(f.Fixes.Lat) != n || len(f.Fixes.Lon) != n || len(f.Fixes.Spd) != n {
		return fmt.Errorf("fix columns have different lengths: %d %d %d %d %d", n, len(f.Fixes.T), len(f.Fixes.Lat), len(f.Fixes.Lon), len(f.Fixes.Spd))
	}
	for i := range n {
		if int(f.Fixes.V[i]) >= len(f.Vehicles) {
			return fmt.Errorf("fix %d points at vehicle %d of %d", i, f.Fixes.V[i], len(f.Vehicles))
		}
		if i > 0 && (f.Fixes.V[i] < f.Fixes.V[i-1] || (f.Fixes.V[i] == f.Fixes.V[i-1] && f.Fixes.T[i] <= f.Fixes.T[i-1])) {
			return fmt.Errorf("fix %d is out of (v, t) order", i)
		}
	}
	total := 0
	for _, m := range f.Minutes {
		total += m.Vehicles
	}
	if total != n {
		return fmt.Errorf("minutes count %d vehicles but the file has %d fixes", total, n)
	}
	return nil
}

// sqlSample is the independent query for T2: a window function instead of the Go fold. It returns
// the newest fix per (operator, vehicle, minute) in [from, to) with the snapshot's tie rule, keyed
// by "operator/vehicle/line@t" and valued with the three rounded columns.
func sqlSample(t *testing.T, ctx context.Context, pool *pgxpool.Pool, hour time.Time) map[string][3]int32 {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT operator, vehicle, line, event_time, lat, lon, speed_kmh FROM (
  SELECT *, row_number() OVER (PARTITION BY operator, vehicle, date_trunc('minute', event_time)
              ORDER BY event_time DESC, received_time DESC, feed COLLATE "C") AS rn
  FROM positions WHERE event_time >= $1 AND event_time < $2) x
WHERE rn = 1`, hour, hour.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string][3]int32{}
	for rows.Next() {
		var op, veh string
		var line *string
		var ev time.Time
		var lat, lon float64
		var speed *float64
		if err := rows.Scan(&op, &veh, &line, &ev, &lat, &lon, &speed); err != nil {
			t.Fatal(err)
		}
		l := "<null>"
		if line != nil {
			l = *line
		}
		spd := int32(-1)
		if speed != nil {
			spd = int32(math.Round(*speed * 10))
		}
		out[fmt.Sprintf("%s/%s/%s@%d", op, veh, l, ev.Unix()-hour.Unix())] = [3]int32{int32(math.Round(lat * 1e5)), int32(math.Round(lon * 1e5)), spd}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func fileSample(f hourJSON) map[string][3]int32 {
	out := map[string][3]int32{}
	for i := range f.Fixes.V {
		v := f.Vehicles[f.Fixes.V[i]]
		l := "<null>"
		if v.Line != nil {
			l = *v.Line
		}
		out[fmt.Sprintf("%s/%s/%s@%d", v.Operator, v.Vehicle, l, f.Fixes.T[i])] = [3]int32{f.Fixes.Lat[i], f.Fixes.Lon[i], f.Fixes.Spd[i]}
	}
	return out
}

// shifted is minuteOf moved back by d: the same real fixes and statuses, stamped d earlier, as if
// that older minute had been fetched late. It is how a backfill is simulated; the values are not
// touched.
func shifted(t *testing.T, minute string, d time.Duration) ([]event.Position, []event.FeedStatus) {
	t.Helper()
	evs, sts := minuteOf(t, minute)
	rawMinute := mustTime(t, minute).Add(-d).Format(time.RFC3339)
	for i := range evs {
		evs[i].EventTime = evs[i].EventTime.Add(-d)
		evs[i].ReceivedTime = evs[i].ReceivedTime.Add(-d)
		evs[i].RawRef.MinuteUTC = rawMinute
	}
	for i := range sts {
		sts[i].MinuteUTC = rawMinute
	}
	return evs, sts
}

func write(t *testing.T, ctx context.Context, pool *pgxpool.Pool, evs []event.Position, sts []event.FeedStatus) {
	t.Helper()
	if _, err := store.WritePositions(ctx, pool, evs); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteFeedStatus(ctx, pool, sts); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryContract(t *testing.T) {
	good := hourJSON{
		Schema: "history-hour.v1", HourUTC: mustTime(t, "2026-10-12T13:00:00Z"), Sampling: historySampling,
		Vehicles: []hourVehicle{{Operator: "rio-sppo", Vehicle: "A1", Mode: "bus"}},
		Fixes:    hourFixes{V: []int32{0}, T: []int32{12}, Lat: []int32{-2290123}, Lon: []int32{-4321234}, Spd: []int32{-1}},
		Minutes:  make([]hourMinute, 60),
	}
	enc := func(f hourJSON) []byte {
		b, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	if err := contracts.Validate(schemaName, enc(good)); err != nil {
		t.Fatalf("a good file was rejected: %v", err)
	}
	bad := good
	bad.Minutes = make([]hourMinute, 59)
	if err := contracts.Validate(schemaName, enc(bad)); err == nil {
		t.Error("a file with 59 minutes was accepted")
	}
	bad = good
	bad.Fixes.T = []int32{3600}
	if err := contracts.Validate(schemaName, enc(bad)); err == nil {
		t.Error("a fix at t=3600 was accepted")
	}
	bad = good
	bad.Fixes.Spd = []int32{-2}
	if err := contracts.Validate(schemaName, enc(bad)); err == nil {
		t.Error("spd -2 was accepted: only -1 means missing")
	}
	bad = good
	bad.Sampling = "every_fix"
	if err := contracts.Validate(schemaName, enc(bad)); err == nil {
		t.Error("an unknown sampling was accepted")
	}
	// What the schema cannot say, hourConsistency does.
	bad = good
	bad.Fixes.Lat = nil
	if hourConsistency(bad) == nil {
		t.Error("columns of different lengths passed the consistency check")
	}
	if err := hourConsistency(good); err == nil {
		t.Error("minutes say 0 vehicles but the file has a fix; the check should have failed")
	}
}

func TestIsComplete(t *testing.T) {
	end := mustTime(t, "2026-10-13T14:30:00Z")
	for _, c := range []struct {
		hour string
		want bool
	}{
		{"2026-10-13T14:00:00Z", false}, // the current hour
		{"2026-10-13T13:00:00Z", false}, // closed, but a backfill may still fill it
		{"2026-10-12T14:00:00Z", false}, // 24 h old exactly: closes at 15:00, +24 h is tomorrow 15:00
		{"2026-10-12T13:00:00Z", true},
		{"2026-10-01T00:00:00Z", true},
	} {
		if got := isComplete(mustTime(t, c.hour), end); got != c.want {
			t.Errorf("isComplete(%s, %s) = %v, want %v", c.hour, end, got, c.want)
		}
	}
}

// T1 to T5 on real sample positions in a real TimescaleDB.
func TestHistoryOnRealData(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	ctr := testharness.StartTimescaleContainer(t)
	conn := testharness.ConnString(t, ctr)
	pool := newPool(t, conn)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	testharness.SettleRetention(t, conn)
	load(t, pool, minute0)
	load(t, pool, minute1)
	// Two older stretches, in hours 11 and 12 when the window is 3 hours: the samples again, 25 h and
	// 1 h earlier. Hour 12 (shifted 1 h) is filled later by the backfill step.
	oldEvs, oldSts := shifted(t, minute0, 25*time.Hour)
	write(t, ctx, pool, oldEvs, oldSts)

	// The samples carry no null speed in this hour, so five vehicles lose theirs here: the "-1 means
	// none" rule needs something to be checked against. (Null, never 0: invariant 6.)
	if _, err := pool.Exec(ctx, `UPDATE positions SET speed_kmh = NULL WHERE (operator, vehicle) IN
	  (SELECT DISTINCT operator, vehicle FROM positions
	    WHERE event_time >= '2026-10-12T13:00:00Z' AND event_time < '2026-10-12T14:00:00Z' ORDER BY vehicle LIMIT 5)`); err != nil {
		t.Fatal(err)
	}

	// A failed minute: every feed failed at 13:40, nothing else written (gap).
	failedMinute := "2026-10-12T13:40:00Z"
	if _, err := store.WriteFeedStatus(ctx, pool, its.EmptyStatuses(failedMinute, event.StatusFailed, nil)); err != nil {
		t.Fatal(err)
	}

	counting := &countingDB{inner: pool}
	srv := newServer(counting, quiet, options{historyHours: 3, now: stepClock(mustTime(t, minute0))})
	if _, err := srv.refresh(ctx); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	h := srv.routes()

	idx := get(t, h, "/v1/history")
	if idx.Code != http.StatusOK {
		t.Fatalf("GET /v1/history = %d: %s", idx.Code, idx.Body)
	}
	var index historyIndexJSON
	if err := json.Unmarshal(idx.Body.Bytes(), &index); err != nil {
		t.Fatal(err)
	}
	wantHours := []string{"2026-10-12T11:00:00Z", "2026-10-12T12:00:00Z", "2026-10-12T13:00:00Z"}
	if !slices.Equal(index.Hours, wantHours) || index.WindowHours != 3 || !index.NewestMinute.Equal(mustTime(t, failedMinute)) {
		t.Errorf("index = %+v, want hours %v, window 3, newest %s", index, wantHours, failedMinute)
	}
	if got := idx.Header().Get("Cache-Control"); got != "public, max-age=10, s-maxage=10" {
		t.Errorf("index Cache-Control = %q", got)
	}

	hour13, body13, rec13 := fetchHour(t, h, "2026-10-12T13")

	t.Run("T1 every file validates against history-hour.v1", func(t *testing.T) {
		for _, hr := range []string{"2026-10-12T11", "2026-10-12T12", "2026-10-12T13"} {
			_, body, _ := fetchHour(t, h, hr)
			checkHour(t, body)
		}
		if hour13.Sampling != historySampling || hour13.Complete {
			t.Errorf("hour 13: sampling %q, complete %v", hour13.Sampling, hour13.Complete)
		}
	})

	t.Run("T2 sampling equals an independent SQL query on every value", func(t *testing.T) {
		got, want := fileSample(hour13), sqlSample(t, ctx, pool, mustTime(t, "2026-10-12T13:00:00Z"))
		if len(want) < 100 {
			t.Fatalf("the fixture has only %d samples in hour 13; the test would check nothing", len(want))
		}
		if !reflect.DeepEqual(got, want) {
			missing, extra, differ := 0, 0, 0
			for k, v := range want {
				if g, ok := got[k]; !ok {
					missing++
				} else if g != v {
					differ++
				}
			}
			for k := range got {
				if _, ok := want[k]; !ok {
					extra++
				}
			}
			t.Errorf("file has %d samples, SQL %d: %d missing, %d extra, %d differ", len(got), len(want), missing, extra, differ)
		}
		// spd is -1 exactly where speed_kmh IS NULL.
		var nullSpeeds int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM (
  SELECT DISTINCT ON (operator, vehicle, date_trunc('minute', event_time)) speed_kmh
  FROM positions WHERE event_time >= '2026-10-12T13:00:00Z' AND event_time < '2026-10-12T14:00:00Z'
  ORDER BY operator, vehicle, date_trunc('minute', event_time), event_time DESC, received_time DESC, feed COLLATE "C") x
  WHERE speed_kmh IS NULL`).Scan(&nullSpeeds); err != nil {
			t.Fatal(err)
		}
		minus1 := 0
		for _, s := range hour13.Fixes.Spd {
			if s == -1 {
				minus1++
			}
		}
		if minus1 != nullSpeeds {
			t.Errorf("%d fixes have spd -1, %d sampled rows have a null speed", minus1, nullSpeeds)
		}
		if nullSpeeds == 0 {
			t.Error("no sampled row has a null speed; the -1 rule is not being checked")
		}
		for _, v := range hour13.Vehicles {
			if info := operators[v.Operator]; v.Mode != info.Mode {
				t.Errorf("%s/%s: mode %q but the operators table says %q", v.Operator, v.Vehicle, v.Mode, info.Mode)
			}
		}
		// Every sample lies inside its minute: t / 60 is the minute the file counts it in.
		perMinute := make([]int, 60)
		for _, tt := range hour13.Fixes.T {
			perMinute[tt/60]++
		}
		for i, m := range hour13.Minutes {
			if m.Vehicles != perMinute[i] {
				t.Errorf("minute %d says %d vehicles, the fixes put %d in it", i, m.Vehicles, perMinute[i])
			}
		}
	})

	t.Run("T3 a minute where every feed failed is counted as failed, with no vehicles", func(t *testing.T) {
		m := hour13.Minutes[40]
		if m.Vehicles != 0 || m.Feeds.Failed != 4 || m.Feeds.OK != 0 || m.Feeds.Missing != 0 {
			t.Errorf("13:40 = %+v, want 0 vehicles and failed=4", m)
		}
		if m := hour13.Minutes[38]; m.Feeds.OK != 4 || m.Vehicles == 0 {
			t.Errorf("13:38 = %+v, want ok=4 and vehicles", m)
		}
		// A minute nothing was recorded for is "missing", not failed and not ok.
		if m := hour13.Minutes[5]; m.Feeds.Missing != 4 || m.Vehicles != 0 || m.Feeds.OK+m.Feeds.Failed != 0 {
			t.Errorf("13:05 = %+v, want missing=4", m)
		}
		// Minutes after the newest one are not yet published.
		if m := hour13.Minutes[41]; m.Feeds.Missing != 4 || m.Vehicles != 0 {
			t.Errorf("13:41 = %+v, want missing=4", m)
		}
	})

	t.Run("T4 a late minute changes only its own hour", func(t *testing.T) {
		before := map[string]string{}
		bytesBefore := map[string][]byte{}
		for _, hr := range []string{"2026-10-12T11", "2026-10-12T12", "2026-10-12T13"} {
			_, body, rec := fetchHour(t, h, hr)
			before[hr], bytesBefore[hr] = rec.Header().Get("ETag"), body
		}
		hour12Before := fileSample(mustHour(t, bytesBefore["2026-10-12T12"]))
		if len(hour12Before) != 0 {
			t.Fatalf("hour 12 has %d samples before the backfill; the test needs it empty", len(hour12Before))
		}
		idxBefore := get(t, h, "/v1/history").Header().Get("ETag")

		// The adapter backfills minute 12:38 (the samples again, 1 h earlier).
		evs, sts := shifted(t, minute0, time.Hour)
		write(t, ctx, pool, evs, sts)
		if _, err := srv.refresh(ctx); err != nil {
			t.Fatal(err)
		}

		for hr, etag := range before {
			rec := get(t, h, hourPath(hr))
			changed := rec.Header().Get("ETag") != etag
			if want := hr == "2026-10-12T12"; changed != want {
				t.Errorf("hour %s: ETag changed = %v, want %v", hr, changed, want)
			}
			if !changed && !bytes.Equal(rec.Body.Bytes(), bytesBefore[hr]) {
				t.Errorf("hour %s: same ETag but different bytes", hr)
			}
		}
		hour12, body12, _ := fetchHour(t, h, "2026-10-12T12")
		checkHour(t, body12)
		if got, want := fileSample(hour12), sqlSample(t, ctx, pool, mustTime(t, "2026-10-12T12:00:00Z")); !reflect.DeepEqual(got, want) || len(want) == 0 {
			t.Errorf("hour 12 after the backfill: %d samples, SQL says %d", len(got), len(want))
		}
		if m := hour12.Minutes[38]; m.Feeds.OK != 4 {
			t.Errorf("12:38 = %+v, want ok=4 after the backfill", m)
		}
		if idxAfter := get(t, h, "/v1/history").Header().Get("ETag"); idxAfter == idxBefore {
			// The index lists the same hours, so its bytes need not change; nothing to assert.
			t.Log("index ETag unchanged by a backfill inside the window (same hours, same bounds)")
		}

		// The incremental path equals a fresh start on the same database, byte for byte.
		fresh := newServer(pool, quiet, options{historyHours: 3, now: stepClock(mustTime(t, minute0))})
		if _, err := fresh.refresh(ctx); err != nil {
			t.Fatal(err)
		}
		for _, hr := range []string{"2026-10-12T11", "2026-10-12T12", "2026-10-12T13"} {
			a := get(t, srv.routes(), hourPath(hr)).Body.Bytes()
			b := get(t, fresh.routes(), hourPath(hr)).Body.Bytes()
			if !bytes.Equal(a, b) {
				t.Errorf("hour %s: the incremental history differs from a fresh load (%d vs %d bytes)", hr, len(a), len(b))
			}
		}
	})

	t.Run("an unchanged feed_status rebuilds nothing", func(t *testing.T) {
		etag := get(t, h, hourPath("2026-10-12T13")).Header().Get("ETag")
		before := counting.history.Load()
		for range 3 {
			if _, err := srv.refresh(ctx); err != nil {
				t.Fatal(err)
			}
		}
		if got := counting.history.Load() - before; got != 3 {
			t.Errorf("3 idle refreshes ran %d history queries, want 3 (the status read only)", got)
		}
		if got := get(t, h, hourPath("2026-10-12T13")).Header().Get("ETag"); got != etag {
			t.Errorf("an idle refresh changed the ETag: %s -> %s", etag, got)
		}
	})

	t.Run("http: gzip, 304, 404 and bad hours", func(t *testing.T) {
		rec := get(t, h, hourPath("2026-10-12T13"), "Accept-Encoding", "gzip")
		if rec.Header().Get("Content-Encoding") != "gzip" {
			t.Fatal("not gzipped")
		}
		zr, err := gzip.NewReader(rec.Body)
		if err != nil {
			t.Fatal(err)
		}
		plain, _ := io.ReadAll(zr)
		if !bytes.Equal(plain, get(t, h, hourPath("2026-10-12T13")).Body.Bytes()) {
			t.Error("the gzip body does not decode to the plain body")
		}
		etag := rec.Header().Get("ETag")
		if got := get(t, h, hourPath("2026-10-12T13"), "If-None-Match", etag); got.Code != http.StatusNotModified {
			t.Errorf("If-None-Match = %d, want 304", got.Code)
		}
		if got := rec.Header().Get("Cache-Control"); got != "public, max-age=30, s-maxage=30" {
			t.Errorf("an open hour's Cache-Control = %q", got)
		}
		for path, want := range map[string]int{
			hourPath("2026-10-12T10"): http.StatusNotFound,   // before the window
			hourPath("2026-10-12T14"): http.StatusNotFound,   // after it
			hourPath("nonsense"):      http.StatusBadRequest, // not an hour
		} {
			got := get(t, h, path)
			if got.Code != want {
				t.Errorf("GET %s = %d, want %d", path, got.Code, want)
			}
			var body map[string]string
			if err := json.Unmarshal(got.Body.Bytes(), &body); err != nil || body["error"] == "" {
				t.Errorf("GET %s: no JSON error body: %q", path, got.Body)
			}
		}
	})

	t.Run("T5 a thousand history requests run no query", func(t *testing.T) {
		before := counting.history.Load() + counting.snapshots.Load() + counting.probes.Load()
		ts := httptest.NewServer(h)
		defer ts.Close()
		for _, p := range []string{"/v1/history", hourPath("2026-10-12T13")} {
			hammer(t, ts.URL+p, 500, 8)
		}
		if after := counting.history.Load() + counting.snapshots.Load() + counting.probes.Load(); after != before {
			t.Errorf("1,000 history requests ran %d queries, want 0", after-before)
		}
	})

	_ = rec13
	_ = body13
}

func mustHour(t *testing.T, body []byte) hourJSON {
	t.Helper()
	var f hourJSON
	if err := json.Unmarshal(body, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

// An hour closed for longer than the backfill window is immutable, and says so.
func TestHistoryCompleteHourIsImmutable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	ctr := testharness.StartTimescaleContainer(t)
	conn := testharness.ConnString(t, ctr)
	pool := newPool(t, conn)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	testharness.SettleRetention(t, conn)
	load(t, pool, minute0)
	evs, sts := shifted(t, minute0, 30*time.Hour)
	write(t, ctx, pool, evs, sts)

	srv := newServer(pool, quiet, options{historyHours: 40, now: stepClock(mustTime(t, minute0))})
	if _, err := srv.refresh(ctx); err != nil {
		t.Fatal(err)
	}
	h := srv.routes()
	old := get(t, h, hourPath("2026-10-11T07"))
	if old.Code != http.StatusOK {
		t.Fatalf("GET old hour = %d: %s", old.Code, old.Body)
	}
	if got := old.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("a complete hour's Cache-Control = %q", got)
	}
	if !mustHour(t, old.Body.Bytes()).Complete {
		t.Error("the hour is not marked complete")
	}
	cur := get(t, h, hourPath("2026-10-12T13"))
	if got := cur.Header().Get("Cache-Control"); got != "public, max-age=30, s-maxage=30" {
		t.Errorf("the current hour's Cache-Control = %q", got)
	}
	// Hours with no data at all are still listed, empty and honest: every feed is missing.
	empty := mustHour(t, get(t, h, hourPath("2026-10-11T20")).Body.Bytes())
	for i, m := range empty.Minutes {
		if m.Vehicles != 0 || m.Feeds.Missing != 4 {
			t.Fatalf("empty hour, minute %d = %+v, want missing=4", i, m)
		}
	}
}
