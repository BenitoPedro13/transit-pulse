package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BenitoPedro13/transit-pulse/packages/raw"
	"github.com/BenitoPedro13/transit-pulse/packages/samples"
)

// pagedMinute is the minute of the captured paged samples (smtr-its-20261012-paged-p0..p2.json):
// one maxtrack vehicle, limit=5, so 5 + 5 + 4 records over 3 real pages.
const pagedMinute = "2026-10-12T14:36:00Z"

var (
	// now makes exactly one minute due: latestDue = 14:36, windowStart = 14:36.
	testNow    = time.Date(2026, 10, 12, 14, 37, 30, 0, time.UTC)
	testWindow = 90 * time.Second
	testLag    = 30 * time.Second
)

// fakeITS serves the captured pages keyed by cursor, the way the real API chains them, and
// records every request. wrap lets a test replace the response for a request.
type fakeITS struct {
	t      *testing.T
	srv    *httptest.Server
	pages  map[string][]byte // cursor ("" for the first page) -> captured body
	mu     sync.Mutex
	urls   []*url.URL
	agents []string
	times  []time.Time
	wrap   func(n int, w http.ResponseWriter, r *http.Request) (handled bool)
}

func newFakeITS(t *testing.T) *fakeITS {
	t.Helper()
	f := &fakeITS{t: t, pages: map[string][]byte{}}
	cursor := ""
	for i := range 3 {
		body := samples.MustRead(t, "smtr-its-20261012-paged-p"+string(rune('0'+i))+".json")
		f.pages[cursor] = body
		var pg struct {
			NextCursor *string `json:"next_cursor"`
		}
		if err := json.Unmarshal(body, &pg); err != nil {
			t.Fatal(err)
		}
		if pg.NextCursor != nil {
			cursor = *pg.NextCursor
		}
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeITS) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.urls = append(f.urls, r.URL)
	f.agents = append(f.agents, r.UserAgent())
	f.times = append(f.times, time.Now())
	n := len(f.urls) - 1
	wrap := f.wrap
	f.mu.Unlock()
	if wrap != nil && wrap(n, w, r) {
		return
	}
	body, ok := f.pages[r.URL.Query().Get("cursor")]
	if !ok || r.URL.Query().Get("minuto_utc") != pagedMinute {
		http.Error(w, "unexpected request "+r.URL.String(), http.StatusTeapot)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

func (f *fakeITS) requests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.urls)
}

func testConfig(t *testing.T, f *fakeITS, dir string) Config {
	return Config{
		RawDir: dir, BaseURL: f.srv.URL, MinuteLag: testLag, BackfillWindow: testWindow,
		RatePerSec:     1000, // the limiter has its own test
		RequestTimeout: 5 * time.Second, BackoffBase: time.Millisecond, BackoffMax: 5 * time.Millisecond,
		MaxAttempts: 5, FailedCooldown: time.Minute, BadRequestWait: 5 * time.Minute,
		Now: func() time.Time { return testNow },
	}
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newTestAdapter(t *testing.T, f *fakeITS, dir string) *adapter {
	t.Helper()
	a := newAdapter(testConfig(t, f, dir), quietLogger())
	if err := a.load(); err != nil {
		t.Fatal(err)
	}
	return a
}

func (a *adapter) isComplete(minute string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.idx.Chain(minute)
	return ok
}

// attemptsOf returns the stored attempts for a page of a minute.
func (a *adapter) attemptsOf(minute string, page int) []raw.Header {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []raw.Header
	for _, e := range a.idx.Attempts(minute) {
		if e.Header.Page == page {
			out = append(out, e.Header)
		}
	}
	return out
}

// E4. The adapter walks the captured pages, marks the minute complete, and after a restart over
// the same RAW_DIR makes no request for it.
func TestPagingCompletenessAndRestart(t *testing.T) {
	f := newFakeITS(t)
	dir := t.TempDir()
	a := newTestAdapter(t, f, dir)

	if err := a.drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.requests(); got != 3 {
		t.Fatalf("requests = %d, want 3 (the number of captured pages)", got)
	}
	if !a.isComplete(pagedMinute) {
		t.Fatal("minute not complete after walking all pages")
	}

	// The cursor of each request is exactly the previous page's next_cursor, and the query is the
	// documented one: minuto_utc, limit=5000, nothing else.
	var wantCursor string
	for i, u := range f.urls {
		q := u.Query()
		if q.Get("cursor") != wantCursor || q.Get("limit") != "5000" || q.Get("minuto_utc") != pagedMinute || len(q) != 2+btoi(i > 0) {
			t.Errorf("request %d: %s (want cursor %q)", i, u.RawQuery, wantCursor)
		}
		var pg struct {
			NextCursor *string `json:"next_cursor"`
		}
		_ = json.Unmarshal(f.pages[wantCursor], &pg)
		if pg.NextCursor != nil {
			wantCursor = *pg.NextCursor
		}
	}

	// Restart: a new process over the same directory.
	before := f.requests()
	b := newTestAdapter(t, f, dir)
	if !b.isComplete(pagedMinute) {
		t.Fatal("restarted adapter does not see the minute as complete")
	}
	if err := b.drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.requests() - before; got != 0 {
		t.Errorf("restart made %d requests for a complete minute, want 0", got)
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestUserAgent(t *testing.T) {
	f := newFakeITS(t)
	cfg := testConfig(t, f, t.TempDir())
	a := newAdapter(cfg, quietLogger())
	_ = a.load()
	_ = a.drain(context.Background())
	if got := f.agents[0]; got != "transit-pulse/"+version {
		t.Errorf("User-Agent without contact = %q", got)
	}
	if got := (Config{Contact: "ops@example.invalid"}).userAgent(); got != "transit-pulse/"+version+" (+ops@example.invalid)" {
		t.Errorf("User-Agent with contact = %q", got)
	}
}

// E6: a 5xx followed by success keeps both attempts in the raw log and completes the minute.
func TestRetryAfterServerError(t *testing.T) {
	f := newFakeITS(t)
	f.wrap = func(n int, w http.ResponseWriter, r *http.Request) bool {
		if n == 0 {
			http.Error(w, "upstream down", http.StatusServiceUnavailable)
			return true
		}
		return false
	}
	a := newTestAdapter(t, f, t.TempDir())
	if err := a.drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.requests(); got != 4 {
		t.Errorf("requests = %d, want 4 (503, then 3 pages)", got)
	}
	at := a.attemptsOf(pagedMinute, 0)
	if len(at) != 2 || at[0].Status != 503 || at[1].Status != 200 || at[0].Attempt != 0 || at[1].Attempt != 1 {
		t.Errorf("page 0 attempts = %+v, want a 503 then a 200", at)
	}
	if !a.isComplete(pagedMinute) {
		t.Error("minute not complete after the retry")
	}
	if len(a.pending) != 0 {
		t.Errorf("a recovered fetch queued %d failed statuses", len(a.pending))
	}
}

// E6: a 4xx (the captured 422 body) is not retried; the minute stays missing and a failed status
// is queued for every mapped feed.
func TestClientErrorIsNotRetried(t *testing.T) {
	f := newFakeITS(t)
	body := samples.MustRead(t, "smtr-its-20261012-error-422.json")
	f.wrap = func(n int, w http.ResponseWriter, r *http.Request) bool {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write(body)
		return true
	}
	a := newTestAdapter(t, f, t.TempDir())
	if err := a.drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.requests(); got != 1 {
		t.Errorf("requests = %d, want 1 (a 422 is not retried)", got)
	}
	if a.isComplete(pagedMinute) {
		t.Error("minute complete after a 422")
	}
	at := a.attemptsOf(pagedMinute, 0)
	if len(at) != 1 || at[0].Status != 422 {
		t.Fatalf("attempts = %+v", at)
	}
	env, err := a.log.Read(a.idx.Attempts(pagedMinute)[0].Loc)
	if err != nil || string(env.Body) != string(body) {
		t.Errorf("the 422 body was not stored as received (err %v)", err)
	}
	if len(a.pending) != 4 {
		t.Fatalf("failed statuses queued = %d, want 4 (one per mapped feed)", len(a.pending))
	}
	for _, s := range a.pending {
		if s.Status != "failed" || s.MinuteUTC != pagedMinute || len(s.RawKeys) != 1 {
			t.Errorf("status = %+v", s)
		}
	}
	// It is not hammered: the minute waits out BadRequestWait before it is tried again.
	if got, want := a.retryAt[pagedMinute], testNow.Add(5*time.Minute); !got.Equal(want) {
		t.Errorf("retry at %v, want %v", got, want)
	}
}

// E6: a connection that dies mid-body is treated like a 5xx: stored, then retried.
func TestConnectionResetMidBody(t *testing.T) {
	f := newFakeITS(t)
	f.wrap = func(n int, w http.ResponseWriter, r *http.Request) bool {
		if n != 0 {
			return false
		}
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return true
		}
		defer func() { _ = conn.Close() }()
		// Promise 100000 bytes, send a few, close. Go's client reports an unexpected EOF.
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 100000\r\n\r\n{\"minuto_utc\":\"")
		_ = buf.Flush()
		if tc, ok := conn.(*net.TCPConn); ok {
			_ = tc.SetLinger(0)
		}
		return true
	}
	a := newTestAdapter(t, f, t.TempDir())
	if err := a.drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	at := a.attemptsOf(pagedMinute, 0)
	if len(at) != 2 {
		t.Fatalf("page 0 attempts = %d, want 2", len(at))
	}
	if !strings.Contains(at[0].Error, "reading body") || at[0].BodyLen == 0 || at[0].Status != 200 {
		t.Errorf("first attempt = %+v, want the partial body stored with an error", at[0])
	}
	if at[1].Error != "" || !a.isComplete(pagedMinute) {
		t.Error("the retry did not complete the minute")
	}
}

// A 200 whose body is not the expected JSON counts as a failed attempt.
func TestUnusableBodyIsRetried(t *testing.T) {
	f := newFakeITS(t)
	f.wrap = func(n int, w http.ResponseWriter, r *http.Request) bool {
		if n == 0 {
			_, _ = w.Write([]byte(`<html>maintenance</html>`))
			return true
		}
		return false
	}
	a := newTestAdapter(t, f, t.TempDir())
	_ = a.drain(context.Background())
	at := a.attemptsOf(pagedMinute, 0)
	if len(at) != 2 || at[0].Error == "" || !a.isComplete(pagedMinute) {
		t.Errorf("attempts = %+v, complete = %v", at, a.isComplete(pagedMinute))
	}
}

func TestGivesUpAfterMaxAttempts(t *testing.T) {
	f := newFakeITS(t)
	f.wrap = func(n int, w http.ResponseWriter, r *http.Request) bool {
		w.Header().Set("Retry-After", "0")
		http.Error(w, "busy", http.StatusTooManyRequests)
		return true
	}
	a := newTestAdapter(t, f, t.TempDir())
	_ = a.drain(context.Background())
	if got := f.requests(); got != 5 {
		t.Errorf("requests = %d, want 5 attempts", got)
	}
	if a.isComplete(pagedMinute) || len(a.pending) != 4 {
		t.Errorf("complete = %v, failed statuses = %d", a.isComplete(pagedMinute), len(a.pending))
	}
	// Tried again on the next cycle, not before.
	if got, want := a.retryAt[pagedMinute], testNow.Add(time.Minute); !got.Equal(want) {
		t.Errorf("retry at %v, want %v", got, want)
	}
}

// The limiter applies to every request, retries included.
func TestRateLimit(t *testing.T) {
	f := newFakeITS(t)
	f.wrap = func(n int, w http.ResponseWriter, r *http.Request) bool {
		if n < 2 {
			http.Error(w, "x", http.StatusBadGateway)
			return true
		}
		return false
	}
	cfg := testConfig(t, f, t.TempDir())
	cfg.RatePerSec = 20 // one request per 50 ms
	a := newAdapter(cfg, quietLogger())
	_ = a.load()
	_ = a.drain(context.Background())
	if len(f.times) != 5 {
		t.Fatalf("requests = %d, want 5", len(f.times))
	}
	for i := 1; i < len(f.times); i++ {
		if gap := f.times[i].Sub(f.times[i-1]); gap < 45*time.Millisecond {
			t.Errorf("requests %d and %d are %v apart, want >= 50ms", i-1, i, gap)
		}
	}
}

// Live data first: the newest due minute is worked before older missing ones, then the oldest.
func TestNextWorkPriority(t *testing.T) {
	f := newFakeITS(t)
	cfg := testConfig(t, f, t.TempDir())
	cfg.BackfillWindow = 10 * time.Minute
	a := newAdapter(cfg, quietLogger())
	_ = a.load()

	now := testNow // latestDue = 14:36, windowStart = 14:28 (ceil of 14:27:30)
	if m, _ := a.nextWork(now); m != "2026-10-12T14:36:00Z" {
		t.Errorf("first pick = %s, want the newest due minute", m)
	}
	// Once the newest is cooling down, backfill goes oldest first.
	a.retryAt["2026-10-12T14:36:00Z"] = now.Add(time.Hour)
	if m, _ := a.nextWork(now); m != "2026-10-12T14:28:00Z" {
		t.Errorf("backfill pick = %s, want the oldest minute in the window", m)
	}
	// Nothing outside [windowStart, latestDue] is ever picked: with every minute inside the window
	// cooling down there is no work, although 14:37 (not yet due) and 14:27 (too old) are missing.
	for tm := time.Date(2026, 10, 12, 14, 28, 0, 0, time.UTC); !tm.After(time.Date(2026, 10, 12, 14, 36, 0, 0, time.UTC)); tm = tm.Add(time.Minute) {
		a.retryAt[fmtMinute(tm)] = now.Add(time.Hour)
	}
	if m, ok := a.nextWork(now); ok {
		t.Errorf("picked %s with the whole window cooling down", m)
	}
}

func TestWindowAndDueMath(t *testing.T) {
	now := time.Date(2026, 10, 12, 14, 37, 30, 0, time.UTC)
	if got := fmtMinute(latestDue(now, 3*time.Minute)); got != "2026-10-12T14:33:00Z" {
		t.Errorf("latestDue = %s", got)
	}
	if got := fmtMinute(windowStart(now, 23*time.Hour)); got != "2026-10-11T15:38:00Z" {
		t.Errorf("windowStart = %s", got)
	}
	// A minute is due exactly at M + 1 min + lag.
	if got := fmtMinute(latestDue(time.Date(2026, 10, 12, 14, 41, 0, 0, time.UTC), 3*time.Minute)); got != "2026-10-12T14:37:00Z" {
		t.Errorf("due boundary = %s", got)
	}
	if got := fmtMinute(latestDue(time.Date(2026, 10, 12, 14, 40, 59, 0, time.UTC), 3*time.Minute)); got != "2026-10-12T14:36:00Z" {
		t.Errorf("just before the boundary = %s", got)
	}
}

func TestStartupRepairsACrashedTail(t *testing.T) {
	f := newFakeITS(t)
	dir := t.TempDir()
	a := newTestAdapter(t, f, dir)
	if err := a.drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash in the middle of writing the last page: chop its tail off.
	path := a.log.Path(raw.HourKey("its", testNow))
	data, _ := os.ReadFile(path)
	if err := os.WriteFile(path, data[:len(data)-40], 0o644); err != nil {
		t.Fatal(err)
	}
	b := newTestAdapter(t, f, dir)
	if b.isComplete(pagedMinute) {
		t.Fatal("a minute with a half-written last page counted as complete")
	}
	before := f.requests()
	if err := b.drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.requests() - before; got != 1 {
		t.Errorf("requests after repair = %d, want 1 (only the lost page)", got)
	}
	// The re-fetched page is readable by a third start.
	c := newTestAdapter(t, f, dir)
	if !c.isComplete(pagedMinute) {
		t.Error("minute not complete after the repaired page was fetched again")
	}
}

func TestLoadConfig(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

	c, err := LoadConfig(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.BackfillWindow != 23*time.Hour || c.RatePerSec != 1 || c.RawDir != "./var/raw" || c.BaseURL != "https://its.mobilidade.rio" ||
		len(c.Brokers) != 1 || c.Brokers[0] != "127.0.0.1:29092" || c.Contact != "" {
		t.Errorf("defaults = %+v", c)
	}

	for name, m := range map[string]map[string]string{
		"rate above the ceiling": {"ITS_RATE_PER_SEC": "2"},
		"rate not a number":      {"ITS_RATE_PER_SEC": "fast"},
		"bad lag":                {"ITS_MINUTE_LAG": "soon"},
		"window under an hour":   {"ITS_BACKFILL_WINDOW": "30m"},
	} {
		if _, err := LoadConfig(env(m)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	c, err = LoadConfig(env(map[string]string{"ITS_RATE_PER_SEC": "0.5", "KAFKA_BROKERS": "a:1, b:2", "ITS_BASE_URL": "http://x/"}))
	if err != nil || c.RatePerSec != 0.5 || len(c.Brokers) != 2 || c.BaseURL != "http://x" {
		t.Errorf("overrides = %+v, %v", c, err)
	}
}

func TestEnvExampleListsEveryVariable(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".env.example"))
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"KAFKA_BROKERS", "RAW_DIR", "ITS_BASE_URL", "ITS_MINUTE_LAG", "ITS_BACKFILL_WINDOW", "ITS_RATE_PER_SEC", "ITS_CONTACT"} {
		if !strings.Contains(string(b), k+"=") {
			t.Errorf(".env.example does not list %s", k)
		}
	}
}
