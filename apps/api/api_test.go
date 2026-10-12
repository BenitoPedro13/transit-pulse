package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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

	// waitLimit is the longest any wait in this file may block. Every wait names what it waits
	// for, so a stuck test says what never happened.
	waitLimit = 30 * time.Second
)

var (
	suppliers = []string{"conecta", "zirix", "maxtrack", "sonda"}
	quiet     = slog.New(slog.NewTextHandler(io.Discard, nil))
)

// waitFor polls cond until it is true. It fails the test after waitLimit, saying what it waited for.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitLimit)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", waitLimit, what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// minuteOf maps the four captured samples as if they were the single page of the given raw
// minute. The bodies are the real provider bytes; only the header's minute and fetched_at say
// when "we got them". (Like the helper in packages/store's test.)
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
		// Each sample is one supplier's response, so only that supplier's feed has a status to
		// report here: the other three would read "provider_not_ok" (absent from providers_status).
		for _, fs := range its.MinuteStatuses(minute, st, []string{raw.HourKey("its", at)}) {
			if fs.Feed == "its:"+s {
				statuses = append(statuses, fs)
			}
		}
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

// load writes a minute of real sample positions and their statuses, the way the consumer does.
func load(t *testing.T, pool *pgxpool.Pool, minute string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), waitLimit)
	defer cancel()
	evs, sts := minuteOf(t, minute)
	if _, err := store.WritePositions(ctx, pool, evs); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteFeedStatus(ctx, pool, sts); err != nil {
		t.Fatal(err)
	}
}

// countingDB counts the two kinds of query the API runs.
type countingDB struct {
	inner     querier
	snapshots atomic.Int64 // vehiclesQuery: the snapshot query
	probes    atomic.Int64 // stateQuery: the feed_status probe
}

func (c *countingDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	switch sql {
	case vehiclesQuery:
		c.snapshots.Add(1)
	case stateQuery:
		c.probes.Add(1)
	}
	return c.inner.Query(ctx, sql, args...)
}

// stepClock returns a clock that moves one second per call, so two snapshots never share a
// generated_at (and so an ETag).
func stepClock(start time.Time) func() time.Time {
	var n atomic.Int64
	return func() time.Time { return start.Add(time.Duration(n.Add(1)) * time.Second) }
}

func get(t *testing.T, h http.Handler, path string, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, body []byte) latestJSON {
	t.Helper()
	var out latestJSON
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return out
}

func TestLoadConfig(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

	c, err := LoadConfig(env(map[string]string{"DATABASE_URL": "postgres://x"}))
	if err != nil || c.DatabaseURL != "postgres://x" || c.Addr != "127.0.0.1:8080" {
		t.Errorf("defaults = %+v, %v", c, err)
	}
	c, err = LoadConfig(env(map[string]string{"DATABASE_URL": "postgres://x", "API_ADDR": " 127.0.0.1:9 "}))
	if err != nil || c.Addr != "127.0.0.1:9" {
		t.Errorf("addr = %+v, %v", c, err)
	}
	if _, err := LoadConfig(env(nil)); err == nil {
		t.Error("an empty DATABASE_URL was accepted")
	}
}

func TestEnvExampleListsEveryVariable(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".env.example"))
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"DATABASE_URL", "API_ADDR"} {
		if !strings.Contains(string(b), k+"=") {
			t.Errorf(".env.example does not list %s", k)
		}
	}
}

// Every operator the mapping can emit has a label and a mode here, and an unknown one is an error,
// not a guess.
func TestOperatorsCoverTheMapping(t *testing.T) {
	for _, st := range its.EmptyStatuses(minute0, event.StatusFailed, nil) {
		if _, err := operatorOf(st.Operator); err != nil {
			t.Errorf("feed %s: %v", st.Feed, err)
		}
	}
	if _, err := operatorOf("rio-nonexistent"); err == nil {
		t.Error("an unknown operator got a label")
	}
}

func TestNoSnapshotYet(t *testing.T) {
	srv := newServer(nil, quiet, options{})
	for _, path := range []string{"/v1/vehicles/latest", "/healthz"} {
		rec := get(t, srv.routes(), path)
		if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
			t.Errorf("%s before the first snapshot = %d (Retry-After %q), want 503 with Retry-After",
				path, rec.Code, rec.Header().Get("Retry-After"))
		}
	}
}

func TestHeaderParsing(t *testing.T) {
	for _, c := range []struct {
		header string
		want   bool
	}{
		{"gzip", true}, {"br, gzip;q=0.8", true}, {"GZIP", true}, {"gzip;q=0", false},
		{"identity", false}, {"", false}, {"*", false},
	} {
		if got := acceptsGzip(c.header); got != c.want {
			t.Errorf("acceptsGzip(%q) = %v, want %v", c.header, got, c.want)
		}
	}
	for _, c := range []struct {
		header string
		want   bool
	}{
		{`W/"a"`, true}, {`"a"`, true}, {`"b", W/"a"`, true}, {"*", true}, {`"b"`, false}, {"", false},
	} {
		if got := etagMatches(c.header, `W/"a"`); got != c.want {
			t.Errorf("etagMatches(%q) = %v, want %v", c.header, got, c.want)
		}
	}
}

// B1 and B2, on real sample positions in a real TimescaleDB.
func TestSnapshotOnRealData(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn := testharness.StartTimescale(t)
	pool := newPool(t, conn)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	testharness.SettleRetention(t, conn)
	load(t, pool, minute0)
	asOf := mustTime(t, minute0)

	srv := newServer(pool, quiet, options{now: stepClock(asOf.Add(91 * time.Second))})
	if swapped, err := srv.refresh(ctx); err != nil || !swapped {
		t.Fatalf("first refresh = %v, %v; want a snapshot", swapped, err)
	}
	h := srv.routes()
	first := get(t, h, "/v1/vehicles/latest")
	if first.Code != http.StatusOK {
		t.Fatalf("GET latest = %d: %s", first.Code, first.Body)
	}
	resp := decode(t, first.Body.Bytes())

	t.Run("B1 the vehicles are the newest fix per vehicle inside the window", func(t *testing.T) {
		if !resp.AsOfMinute.Equal(asOf) || resp.WindowSeconds != 300 {
			t.Errorf("as_of_minute %s, window_seconds %d; want %s and 300", resp.AsOfMinute, resp.WindowSeconds, asOf)
		}
		to := asOf.Add(time.Minute)
		from := to.Add(-300 * time.Second)

		// Computed independently, with GROUP BY and a join instead of DISTINCT ON.
		type exp struct {
			eventTime      time.Time
			line           *string
			speed, heading *float64
			lat, lon       float64
		}
		want := map[string]exp{}
		rows, err := pool.Query(ctx, `
			SELECT p.operator, p.vehicle, p.event_time, p.line, p.speed_kmh, p.heading_deg, p.lat, p.lon, p.mode
			FROM positions p
			JOIN (SELECT operator, vehicle, max(event_time) AS t FROM positions
			       WHERE event_time > $1 AND event_time <= $2 GROUP BY operator, vehicle) m
			  ON p.operator = m.operator AND p.vehicle = m.vehicle AND p.event_time = m.t`, from, to)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var op, veh, mode string
			var e exp
			if err := rows.Scan(&op, &veh, &e.eventTime, &e.line, &e.speed, &e.heading, &e.lat, &e.lon, &mode); err != nil {
				t.Fatal(err)
			}
			if _, dup := want[op+"/"+veh]; dup {
				t.Fatalf("%s/%s has two fixes at the same instant in the samples; the test would be ambiguous", op, veh)
			}
			want[op+"/"+veh] = e
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if len(want) == 0 {
			t.Fatal("no vehicle inside the window: the test has nothing to check")
		}
		if len(resp.Vehicles) != len(want) {
			t.Errorf("snapshot has %d vehicles, SQL says %d distinct (operator, vehicle) in the window", len(resp.Vehicles), len(want))
		}
		seen := map[string]bool{}
		for _, v := range resp.Vehicles {
			key := v.Operator + "/" + v.Vehicle
			if seen[key] {
				t.Errorf("%s appears twice", key)
			}
			seen[key] = true
			e, ok := want[key]
			if !ok {
				t.Errorf("%s is in the snapshot but not in the window", key)
				continue
			}
			if !v.EventTime.Equal(e.eventTime) || v.Lat != e.lat || v.Lon != e.lon ||
				!reflect.DeepEqual(v.Line, e.line) || !reflect.DeepEqual(v.SpeedKmh, e.speed) || !reflect.DeepEqual(v.HeadingDeg, e.heading) {
				t.Errorf("%s differs from its newest stored fix: got %+v, want %+v", key, v, e)
			}
			if info := operators[v.Operator]; v.Mode != info.Mode {
				t.Errorf("%s: row mode %q but the operators table says %q", key, v.Mode, info.Mode)
			}
		}

		// Null stays null and a reported 0 stays 0 (the samples have both).
		var nulls, zeros int
		for _, v := range resp.Vehicles {
			if v.Line == nil || v.SpeedKmh == nil || v.HeadingDeg == nil {
				nulls++
			}
			if v.SpeedKmh != nil && *v.SpeedKmh == 0 {
				zeros++
			}
		}
		if zeros == 0 {
			t.Error("no vehicle with a reported speed of 0 in the window; the 0-is-not-null check has nothing to check")
		}
		t.Logf("%d vehicles in the window (%d with a null line, speed or heading, %d reporting speed 0)", len(resp.Vehicles), nulls, zeros)
	})

	t.Run("B1 a vehicle reported by both SPPO feeds appears once, with its newest fix", func(t *testing.T) {
		// Found by SQL, not named in the test: the vehicles that two feeds of one operator report.
		rows, err := pool.Query(ctx, `SELECT operator, vehicle FROM positions
			WHERE event_time > $1 AND event_time <= $2
			GROUP BY operator, vehicle HAVING count(DISTINCT feed) > 1`, asOf.Add(time.Minute-300*time.Second), asOf.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		var both [][2]string
		for rows.Next() {
			var op, veh string
			if err := rows.Scan(&op, &veh); err != nil {
				t.Fatal(err)
			}
			both = append(both, [2]string{op, veh})
		}
		rows.Close()
		if len(both) == 0 {
			t.Fatal("no vehicle is reported by two feeds in the window; the samples used to have one (A48064)")
		}
		for _, b := range both {
			var n int
			var newest time.Time
			for _, v := range resp.Vehicles {
				if v.Operator == b[0] && v.Vehicle == b[1] {
					n++
					newest = v.EventTime
				}
			}
			var maxStored time.Time
			if err := pool.QueryRow(ctx, `SELECT max(event_time) FROM positions WHERE operator = $1 AND vehicle = $2 AND event_time <= $3`,
				b[0], b[1], asOf.Add(time.Minute)).Scan(&maxStored); err != nil {
				t.Fatal(err)
			}
			if n != 1 || !newest.Equal(maxStored) {
				t.Errorf("%s/%s: %d rows in the snapshot, newest %s; want 1 row at %s", b[0], b[1], n, newest, maxStored)
			}
		}
	})

	t.Run("B1 a fix older than 300 s is absent", func(t *testing.T) {
		from := asOf.Add(time.Minute - 300*time.Second)
		rows, err := pool.Query(ctx, `SELECT operator, vehicle FROM positions
			GROUP BY operator, vehicle HAVING max(event_time) <= $1`, from)
		if err != nil {
			t.Fatal(err)
		}
		var stale []string
		for rows.Next() {
			var op, veh string
			if err := rows.Scan(&op, &veh); err != nil {
				t.Fatal(err)
			}
			stale = append(stale, op+"/"+veh)
		}
		rows.Close()
		if len(stale) == 0 {
			t.Fatal("no vehicle in the samples has only fixes older than the window; the test has nothing to check")
		}
		for _, v := range resp.Vehicles {
			if slices.Contains(stale, v.Operator+"/"+v.Vehicle) {
				t.Errorf("%s/%s has no fix newer than %s but is shown (event_time %s)", v.Operator, v.Vehicle, from, v.EventTime)
			}
		}
		t.Logf("%d vehicles have only fixes older than the window and are absent", len(stale))
	})

	t.Run("coverage has one entry per operator, without the feed name", func(t *testing.T) {
		got := map[string]coverageJSON{}
		for _, c := range resp.Coverage {
			got[c.Operator] = c
		}
		if len(got) != 3 || len(resp.Coverage) != 3 {
			t.Fatalf("coverage = %+v; want rio-sppo, rio-sistema-rio, rio-brt", resp.Coverage)
		}
		sppo := got["rio-sppo"]
		if sppo.Label != "Ônibus municipais (SPPO)" || sppo.Mode != "bus" || sppo.Tier != "live" ||
			sppo.FeedsTotal != 2 || !reflect.DeepEqual(sppo.FeedsByStatus, map[string]int{"ok": 2}) || !sppo.MinuteUTC.Equal(asOf) {
			t.Errorf("rio-sppo coverage = %+v", sppo)
		}
		if strings.Contains(first.Body.String(), `"feed"`) || strings.Contains(first.Body.String(), "its:") {
			t.Error("the response names a feed")
		}
		order := []string{resp.Coverage[0].Operator, resp.Coverage[1].Operator, resp.Coverage[2].Operator}
		if !slices.Equal(order, []string{"rio-sppo", "rio-sistema-rio", "rio-brt"}) {
			t.Errorf("coverage order = %v", order)
		}
	})

	t.Run("headers, 304 and gzip", func(t *testing.T) {
		if cc := first.Header().Get("Cache-Control"); cc != "public, max-age=10" {
			t.Errorf("Cache-Control = %q", cc)
		}
		etag := first.Header().Get("ETag")
		if !strings.Contains(etag, "2026-10-12T13:38:00Z") || first.Header().Get("Vary") != "Accept-Encoding" {
			t.Errorf("ETag %q (want the as_of_minute in it), Vary %q", etag, first.Header().Get("Vary"))
		}
		if first.Header().Get("Content-Encoding") != "" {
			t.Error("gzip sent to a client that did not ask for it")
		}
		not := get(t, h, "/v1/vehicles/latest", "If-None-Match", etag)
		if not.Code != http.StatusNotModified || not.Body.Len() != 0 || not.Header().Get("ETag") != etag {
			t.Errorf("If-None-Match = %d with %d body bytes", not.Code, not.Body.Len())
		}
		zipped := get(t, h, "/v1/vehicles/latest", "Accept-Encoding", "gzip")
		if zipped.Header().Get("Content-Encoding") != "gzip" {
			t.Fatalf("no gzip for Accept-Encoding: gzip (headers %v)", zipped.Header())
		}
		gzipBytes := zipped.Body.Len()
		zr, err := gzip.NewReader(zipped.Body)
		if err != nil {
			t.Fatal(err)
		}
		plain, err := io.ReadAll(zr)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(plain, first.Body.Bytes()) {
			t.Error("the gzip body does not decode to the plain body")
		}
		health := get(t, h, "/healthz")
		var hj healthJSON
		if err := json.Unmarshal(health.Body.Bytes(), &hj); err != nil || health.Code != 200 || hj.Status != "ok" ||
			hj.Vehicles != len(resp.Vehicles) || !hj.RefreshOK {
			t.Errorf("healthz = %d %s (%v)", health.Code, health.Body, err)
		}
		// B4 on the samples; the recorded run's number is measured by hand and recorded in the task document.
		t.Logf("payload: %d vehicles, %d bytes JSON, %d bytes gzip", len(resp.Vehicles), len(plain), gzipBytes)
	})

	t.Run("a refresh with nothing new does not query positions", func(t *testing.T) {
		counting := &countingDB{inner: pool}
		s2 := newServer(counting, quiet, options{now: stepClock(asOf.Add(91 * time.Second))})
		for range 3 {
			if _, err := s2.refresh(ctx); err != nil {
				t.Fatal(err)
			}
		}
		if counting.snapshots.Load() != 1 || counting.probes.Load() != 3 {
			t.Errorf("3 refreshes ran %d snapshot queries and %d probes; want 1 and 3", counting.snapshots.Load(), counting.probes.Load())
		}
	})

	// B2: a newer minute that failed. The vehicles do not move and the coverage says why.
	t.Run("B2 a newer failed minute is failed, not zero vehicles", func(t *testing.T) {
		failedMinute := mustTime(t, minute1)
		if _, err := store.WriteFeedStatus(ctx, pool, its.EmptyStatuses(minute1, event.StatusFailed, nil)); err != nil {
			t.Fatal(err)
		}
		if swapped, err := srv.refresh(ctx); err != nil || !swapped {
			t.Fatalf("refresh after the failed minute = %v, %v; want a new snapshot", swapped, err)
		}
		rec := get(t, h, "/v1/vehicles/latest")
		after := decode(t, rec.Body.Bytes())
		if !after.AsOfMinute.Equal(asOf) {
			t.Errorf("as_of_minute moved to %s on a failed minute; want %s", after.AsOfMinute, asOf)
		}
		if !reflect.DeepEqual(after.Vehicles, resp.Vehicles) {
			t.Errorf("vehicles changed: %d before, %d after", len(resp.Vehicles), len(after.Vehicles))
		}
		if rec.Header().Get("ETag") == first.Header().Get("ETag") {
			t.Error("the ETag did not change with the coverage")
		}
		if len(after.Coverage) != 3 {
			t.Fatalf("coverage = %+v", after.Coverage)
		}
		for _, c := range after.Coverage {
			if !c.MinuteUTC.Equal(failedMinute) || c.FeedsByStatus["failed"] != c.FeedsTotal || c.FeedsByStatus["ok"] != 0 {
				t.Errorf("%s coverage = %+v; want all %d feeds failed at %s", c.Operator, c, c.FeedsTotal, failedMinute)
			}
		}
		if after.Coverage[0].Operator != "rio-sppo" || after.Coverage[0].FeedsTotal != 2 || after.Coverage[0].FeedsByStatus["failed"] != 2 {
			t.Errorf("rio-sppo coverage = %+v; want 2 failed feeds", after.Coverage[0])
		}

		// The minute is then fetched after all: failed → ok at the same minute_utc moves as_of_minute.
		evs, sts := minuteOf(t, minute1)
		if _, err := store.WritePositions(ctx, pool, evs); err != nil {
			t.Fatal(err)
		}
		if _, err := store.WriteFeedStatus(ctx, pool, sts); err != nil {
			t.Fatal(err)
		}
		if swapped, err := srv.refresh(ctx); err != nil || !swapped {
			t.Fatalf("refresh after the minute recovered = %v, %v", swapped, err)
		}
		recovered := decode(t, get(t, h, "/v1/vehicles/latest").Body.Bytes())
		if !recovered.AsOfMinute.Equal(failedMinute) || recovered.Coverage[0].FeedsByStatus["ok"] != 2 {
			t.Errorf("after recovery: as_of_minute %s, rio-sppo %+v", recovered.AsOfMinute, recovered.Coverage[0])
		}
	})
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// hammer sends n GET requests from workers goroutines and returns each request's latency. Every
// response must be a 200 with a body that decodes. It fails the test, not hangs, if it takes longer
// than waitLimit.
func hammer(t *testing.T, url string, n, workers int) []time.Duration {
	t.Helper()
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DisableCompression: true, MaxIdleConnsPerHost: workers}}
	defer client.CloseIdleConnections()
	lat := make([]time.Duration, n)
	var next, bad atomic.Int64
	var first atomic.Pointer[string]
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1)) - 1
				if i >= n {
					return
				}
				req, _ := http.NewRequest(http.MethodGet, url, nil)
				req.Header.Set("Accept-Encoding", "gzip")
				start := time.Now()
				resp, err := client.Do(req)
				if err == nil {
					_, err = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()
					if err == nil && resp.StatusCode != http.StatusOK {
						err = &statusError{resp.StatusCode}
					}
				}
				lat[i] = time.Since(start)
				if err != nil {
					bad.Add(1)
					msg := err.Error()
					first.CompareAndSwap(nil, &msg)
				}
			}
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(waitLimit):
		t.Fatalf("%d requests did not finish within %s", n, waitLimit)
	}
	if bad.Load() > 0 {
		t.Fatalf("%d of %d requests failed; first: %s", bad.Load(), n, *first.Load())
	}
	return lat
}

type statusError struct{ code int }

func (e *statusError) Error() string { return "status " + http.StatusText(e.code) }

func p95(d []time.Duration) time.Duration {
	s := slices.Clone(d)
	slices.Sort(s)
	return s[len(s)*95/100]
}

// B3: readers are isolated from the database.
func TestReadersAreIsolated(t *testing.T) {
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

	counting := &countingDB{inner: pool}
	srv := newServer(counting, quiet, options{interval: 100 * time.Millisecond})
	runCtx, stopRun := context.WithCancel(ctx)
	runDone := make(chan struct{})
	go func() { defer close(runDone); srv.Run(runCtx) }()
	defer func() {
		stopRun()
		select {
		case <-runDone:
		case <-time.After(waitLimit):
			t.Error("the refresher did not stop within 30 s of its context ending")
		}
	}()
	ts := httptest.NewServer(srv.routes())
	defer ts.Close()
	url := ts.URL + "/v1/vehicles/latest"

	asOfNow := func() string {
		snap := srv.snap.Load()
		if snap == nil {
			return ""
		}
		return snap.AsOfMinute.Format(time.RFC3339)
	}
	waitFor(t, "the first snapshot", func() bool { return asOfNow() == minute0 })

	t.Run("1,000 requests run no snapshot query and answer within 20 ms (p95, median of 3)", func(t *testing.T) {
		var p95s []time.Duration
		for run := range 3 {
			before := counting.snapshots.Load()
			lat := hammer(t, url, 1000, 8)
			p95s = append(p95s, p95(lat))
			if got := counting.snapshots.Load() - before; got != 0 {
				t.Errorf("run %d: 1,000 requests ran %d snapshot queries; want 0", run+1, got)
			}
		}
		slices.Sort(p95s)
		t.Logf("p95 of 3 runs of 1,000 requests: %v (median %v)", p95s, p95s[1])
		if p95s[1] > 20*time.Millisecond {
			t.Errorf("median p95 = %v, want <= 20ms", p95s[1])
		}
		// The refresher keeps looking at feed_status on its own clock, readers or not.
		waitFor(t, "three feed_status probes", func() bool { return counting.probes.Load() >= 3 })
		if got := counting.snapshots.Load(); got != 1 {
			t.Errorf("snapshot queries after the probes = %d, want 1: an unchanged feed_status must not trigger one", got)
		}
	})

	t.Run("a new minute costs one snapshot query, however many readers there are", func(t *testing.T) {
		if got := counting.snapshots.Load(); got != 1 {
			t.Fatalf("snapshot queries before the new minute = %d, want 1", got)
		}
		stopLoad := make(chan struct{})
		var loadWG sync.WaitGroup
		var served atomic.Int64
		for range 4 {
			loadWG.Add(1)
			go func() {
				defer loadWG.Done()
				c := &http.Client{Timeout: 10 * time.Second}
				for {
					select {
					case <-stopLoad:
						return
					default:
					}
					if resp, err := c.Get(url); err == nil {
						_, _ = io.Copy(io.Discard, resp.Body)
						_ = resp.Body.Close()
						served.Add(1)
					}
				}
			}()
		}
		load(t, pool, minute1)
		waitFor(t, "the snapshot to move to "+minute1, func() bool { return asOfNow() == minute1 })
		close(stopLoad)
		loadDone := make(chan struct{})
		go func() { loadWG.Wait(); close(loadDone) }()
		select {
		case <-loadDone:
		case <-time.After(waitLimit):
			t.Fatal("the background readers did not stop within 30 s")
		}
		hammer(t, url, 1000, 8)
		if got := counting.snapshots.Load(); got != 2 {
			t.Errorf("snapshot queries after one new minute = %d (with %d requests served meanwhile), want 2 in total", got, served.Load())
		}
	})

	t.Run("with the database stopped, requests still return 200 with the old snapshot", func(t *testing.T) {
		before, err := http.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		beforeBody, _ := io.ReadAll(before.Body)
		_ = before.Body.Close()

		stopCtx, stopCancel := context.WithTimeout(ctx, waitLimit)
		defer stopCancel()
		if err := ctr.Stop(stopCtx, nil); err != nil {
			t.Fatalf("stop the database: %v", err)
		}
		waitFor(t, "the refresher to report that the database is unreachable", func() bool {
			return srv.lastErr.Load() != nil
		})

		lat := hammer(t, url, 200, 8)
		resp, err := http.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		afterBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != 200 || !bytes.Equal(beforeBody, afterBody) {
			t.Errorf("with the database down: status %d, same body = %v", resp.StatusCode, bytes.Equal(beforeBody, afterBody))
		}
		health, err := http.Get(ts.URL + "/healthz")
		if err != nil {
			t.Fatal(err)
		}
		healthBody, _ := io.ReadAll(health.Body)
		_ = health.Body.Close()
		var hj healthJSON
		err = json.Unmarshal(healthBody, &hj)
		if strings.Contains(string(healthBody), "transit_pulse") || strings.Contains(string(healthBody), "SQLSTATE") {
			t.Errorf("healthz leaks the database error: %s", healthBody)
		}
		if err != nil || health.StatusCode != 200 || hj.RefreshOK || !hj.AsOfMinute.Equal(mustTime(t, minute1)) {
			t.Errorf("healthz with the database down = %d %+v (%v); want 200, the old as_of_minute and refresh_ok false", health.StatusCode, hj, err)
		}
		t.Logf("database down: 200 requests all answered, p95 %v; refresh_error: %s", p95(lat), *srv.lastErr.Load())
	})
}
