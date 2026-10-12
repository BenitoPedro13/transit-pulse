package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/BenitoPedro13/transit-pulse/packages/raw"
)

const (
	vehiclesPath = "/v1/geolocalizacao/veiculos"
	pageLimit    = "5000"
	// maxBody guards memory: a page is about 2.2 MB, so 64 MiB is a runaway response.
	maxBody = 64 << 20
	// shutdownGrace is how long an in-flight request may run after a stop signal.
	shutdownGrace = 6 * time.Second
	rawSource     = "its"
)

// adapter holds everything the fetcher and the publisher share.
type adapter struct {
	cfg     Config
	lg      *slog.Logger
	log     *raw.Log
	http    *http.Client
	limiter *rate.Limiter
	wake    chan struct{} // tells the publisher a minute completed

	mu      sync.Mutex // guards the fields below
	idx     *raw.Index
	retryAt map[string]time.Time // minute -> do not try before
	pending []pendingStatus      // failed statuses waiting to be published
	known   map[string]bool      // minutes whose gap was already reported or that were published
}

func newAdapter(cfg Config, lg *slog.Logger) *adapter {
	cfg.applyDefaults()
	return &adapter{
		cfg:     cfg,
		lg:      lg,
		log:     raw.Open(cfg.RawDir, rawSource),
		http:    &http.Client{},
		limiter: rate.NewLimiter(rate.Limit(cfg.RatePerSec), 1),
		wake:    make(chan struct{}, 1),
		idx:     raw.NewIndex(),
		retryAt: map[string]time.Time{},
		known:   map[string]bool{},
	}
}

func (a *adapter) notify() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// outcome is what one page fetch (all its attempts) came to.
type outcome int

const (
	pageOK        outcome = iota
	pageRetryable         // out of attempts on a transport error, 5xx, 429 or bad body
	pageRejected          // a 4xx other than 429: the request is wrong, so it is not retried
)

// attempt is the result of one HTTP request, before it is stored.
type attempt struct {
	status      int
	err         string
	body        []byte
	contentType string
	took        time.Duration
	finished    time.Time
	retryAfter  time.Duration
}

// retryable says whether a failed attempt is worth repeating.
func (t attempt) retryable() bool {
	switch {
	case t.status == 0: // transport error
		return true
	case t.status == http.StatusTooManyRequests, t.status >= 500:
		return true
	case t.status == http.StatusOK:
		return true // 200 with a truncated or unusable body
	case t.status >= 400:
		return false
	default:
		return true // 1xx/3xx that survived redirects: unexpected, try again
	}
}

// fetchPage fetches one page of a minute, with retries, and stores every attempt in the raw log
// before doing anything else with it. It returns pageOK when a good page is in the log.
func (a *adapter) fetchPage(ctx context.Context, minute string, page int, cursor *string) (outcome, error) {
	a.mu.Lock()
	prior := 0
	for _, e := range a.idx.Attempts(minute) {
		if e.Header.Page == page {
			prior++
		}
	}
	a.mu.Unlock()

	var last attempt
	for n := 0; n < a.cfg.MaxAttempts; n++ {
		if n > 0 {
			if err := sleep(ctx, a.backoff(n, last.retryAfter)); err != nil {
				return pageRetryable, err
			}
		}
		if err := a.limiter.Wait(ctx); err != nil {
			return pageRetryable, err
		}
		reqURL := a.requestURL(minute, cursor)
		last = a.doRequest(ctx, reqURL)

		h := raw.Header{
			URL: reqURL, MinuteUTC: minute, Page: page, Attempt: prior + n, Cursor: cursor,
			FetchedAt: last.finished, Status: last.status, Error: last.err,
			DurationMS: last.took.Milliseconds(), ContentType: last.contentType,
		}
		if last.status == http.StatusOK && last.err == "" {
			next, bad := checkBody(last.body, minute)
			if bad != "" {
				last.err = bad
				h.Error = bad
			}
			h.NextCursor = next
		}
		loc, err := a.log.Append(h, last.body)
		if err != nil {
			return pageRetryable, fmt.Errorf("raw log write failed, cannot capture: %w", err)
		}
		a.mu.Lock()
		a.idx.Add(raw.Entry{Header: withBodyLen(h, len(last.body)), Loc: loc})
		a.mu.Unlock()

		if last.status == http.StatusOK && last.err == "" {
			return pageOK, nil
		}
		if !last.retryable() {
			a.lg.Warn("request rejected, not retrying", "minute", minute, "page", page, "status", last.status, "body", truncate(string(last.body), 500))
			return pageRejected, nil
		}
		a.lg.Warn("attempt failed", "minute", minute, "page", page, "attempt", n+1, "of", a.cfg.MaxAttempts, "status", last.status, "error", last.err)
	}
	return pageRetryable, nil
}

func withBodyLen(h raw.Header, n int) raw.Header {
	h.V, h.Source, h.BodyLen = 1, rawSource, n
	return h
}

// requestURL builds ?minuto_utc=M&limit=5000[&cursor=...]. The cursor is opaque: it is passed back
// unchanged (percent-encoded for the query string, which the server decodes to the same bytes).
func (a *adapter) requestURL(minute string, cursor *string) string {
	q := url.Values{"minuto_utc": {minute}, "limit": {pageLimit}}
	if cursor != nil {
		q.Set("cursor", *cursor)
	}
	return a.cfg.BaseURL + vehiclesPath + "?" + q.Encode()
}

// doRequest performs one GET and reads the whole body. A body that ends early is a failed
// attempt (status 0) and what was received is kept.
func (a *adapter) doRequest(ctx context.Context, reqURL string) attempt {
	start := time.Now()
	t := attempt{}
	done := func() attempt {
		t.took = time.Since(start)
		t.finished = a.cfg.Now().UTC()
		return t
	}

	// An in-flight request survives a stop signal for shutdownGrace, so a page being read is
	// finished and stored rather than abandoned.
	reqCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.cfg.RequestTimeout)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { time.AfterFunc(shutdownGrace, cancel) })
	defer stop()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, reqURL, nil)
	if err != nil {
		t.err = err.Error()
		return done()
	}
	req.Header.Set("User-Agent", a.cfg.userAgent())
	req.Header.Set("Accept", "application/json")

	resp, err := a.http.Do(req)
	if err != nil {
		t.err = err.Error()
		return done()
	}
	defer func() { _ = resp.Body.Close() }()
	t.status = resp.StatusCode
	t.contentType = resp.Header.Get("Content-Type")
	t.retryAfter = parseRetryAfter(resp.Header.Get("Retry-After"), a.cfg.Now())

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	t.body = body
	switch {
	case err != nil:
		// Truncated or reset mid-body: retried like a 5xx. The status we saw is kept, and what
		// was received is stored as is.
		t.err = "reading body: " + err.Error()
	case len(body) > maxBody:
		t.err = fmt.Sprintf("body larger than %d bytes", maxBody)
	}
	return done()
}

// checkBody validates a 200 body and returns its next_cursor. The empty string means it is fine.
func checkBody(body []byte, minute string) (next *string, bad string) {
	var pg struct {
		MinutoUTC  string          `json:"minuto_utc"`
		Data       json.RawMessage `json:"data"`
		NextCursor *string         `json:"next_cursor"`
	}
	if err := json.Unmarshal(body, &pg); err != nil {
		return nil, "invalid JSON: " + err.Error()
	}
	if len(pg.Data) == 0 || pg.Data[0] != '[' {
		return nil, `body has no "data" array`
	}
	if pg.MinutoUTC != minute {
		return nil, fmt.Sprintf("body is for minuto_utc %q, asked for %q", pg.MinutoUTC, minute)
	}
	return pg.NextCursor, ""
}

// backoff is BackoffBase doubling to BackoffMax, ±20% jitter; a Retry-After longer than that wins.
func (a *adapter) backoff(n int, retryAfter time.Duration) time.Duration {
	d := a.cfg.BackoffBase << (n - 1)
	if d <= 0 || d > a.cfg.BackoffMax {
		d = a.cfg.BackoffMax
	}
	d = time.Duration(float64(d) * (0.8 + 0.4*rand.Float64()))
	return max(d, min(retryAfter, 2*time.Minute))
}

func parseRetryAfter(v string, now time.Time) time.Duration {
	if v == "" {
		return 0
	}
	if s, err := strconv.Atoi(v); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(0, t.Sub(now))
	}
	return 0
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
