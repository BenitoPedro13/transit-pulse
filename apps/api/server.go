package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// defaultInterval is how often the refresher looks at feed_status. It is a tiny query and runs
	// whether or not anyone reads (invariant 5).
	defaultInterval = 10 * time.Second
	// probeTimeout bounds the feed_status probe; computeTimeout bounds the snapshot query. A
	// database that is down must show up as a refresh error, not as a stuck goroutine.
	probeTimeout   = 5 * time.Second
	computeTimeout = 30 * time.Second
)

type options struct {
	interval     time.Duration
	now          func() time.Time
	historyHours int
}

// Server holds the current snapshot. Handlers read it and never touch the database.
type Server struct {
	db   querier
	lg   *slog.Logger
	opts options

	snap    atomic.Pointer[Snapshot]
	lastErr atomic.Pointer[string]

	mu    sync.Mutex // serialises refresh; guards state and hist
	state []feedRow  // what the current snapshot was computed from
	hist  *histState // the time machine's samples; nil until the first load

	view atomic.Pointer[historyView] // what the history handlers serve
}

func newServer(db querier, lg *slog.Logger, opts options) *Server {
	if opts.interval == 0 {
		opts.interval = defaultInterval
	}
	if opts.now == nil {
		opts.now = time.Now
	}
	if opts.historyHours == 0 {
		opts.historyHours = defaultHistoryHours
	}
	return &Server{db: db, lg: lg, opts: opts}
}

// refresh probes feed_status and, only when it moved, computes and swaps in a new snapshot. It
// then brings the history up to date (which also looks at older minutes, because a backfill changes
// those without moving the probe). It reports whether the snapshot was swapped. On any error the
// previous snapshot and history stay in place; a failing history never blocks the snapshot.
func (s *Server) refresh(ctx context.Context) (swapped bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	state, err := readState(pctx, s.db)
	cancel()
	if err != nil {
		return false, err
	}
	var errs []error
	if s.state == nil || !sameState(state, s.state) {
		cctx, cancel := context.WithTimeout(ctx, computeTimeout)
		snap, err := compute(cctx, s.db, state, s.opts.now())
		cancel()
		if err != nil {
			errs = append(errs, err)
		} else {
			s.snap.Store(snap)
			s.state = state
			swapped = true
		}
	}
	if err := s.refreshHistory(ctx, state); err != nil {
		errs = append(errs, err)
	}
	return swapped, errors.Join(errs...)
}

// Run refreshes until ctx ends. Errors are logged when they start or change, and again when they
// clear; they never stop the loop.
func (s *Server) Run(ctx context.Context) {
	tick := func() {
		swapped, err := s.refresh(ctx)
		if ctx.Err() != nil {
			return
		}
		prev := s.lastErr.Load()
		switch {
		case err != nil:
			msg := err.Error()
			s.lastErr.Store(&msg)
			if prev == nil || *prev != msg {
				s.lg.Error("refresh failed; serving the last snapshot", "error", msg, "have_snapshot", s.snap.Load() != nil)
			}
		case prev != nil:
			s.lastErr.Store(nil)
			s.lg.Info("refresh recovered")
		}
		if swapped {
			snap := s.snap.Load()
			s.lg.Info("snapshot swapped", "as_of_minute", snap.AsOfMinute, "vehicles", snap.Vehicles,
				"json_bytes", len(snap.JSON), "gzip_bytes", len(snap.Gzip))
		}
	}
	tick()
	t := time.NewTicker(s.opts.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick()
		}
	}
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/vehicles/latest", s.handleLatest)
	mux.HandleFunc("GET /v1/history", s.handleHistory)
	mux.HandleFunc("GET /v1/history/{hour}", s.handleHistoryHour)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	return mux
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if status == http.StatusServiceUnavailable {
		w.Header().Set("Retry-After", "10")
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func (s *Server) handleLatest(w http.ResponseWriter, r *http.Request) {
	snap := s.snap.Load()
	if snap == nil {
		writeError(w, http.StatusServiceUnavailable, "no snapshot yet: the database has not answered with a minute of positions")
		return
	}
	h := w.Header()
	h.Set("ETag", snap.ETag)
	h.Set("Cache-Control", "public, max-age=10")
	h.Set("Vary", "Accept-Encoding")
	if etagMatches(r.Header.Get("If-None-Match"), snap.ETag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	body := snap.JSON
	if acceptsGzip(r.Header.Get("Accept-Encoding")) {
		h.Set("Content-Encoding", "gzip")
		body = snap.Gzip
	}
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	_, _ = w.Write(body)
}

type healthJSON struct {
	Status             string    `json:"status"`
	AsOfMinute         time.Time `json:"as_of_minute"`
	GeneratedAt        time.Time `json:"generated_at"`
	SnapshotAgeSeconds int64     `json:"snapshot_age_seconds"`
	Vehicles           int       `json:"vehicles"`
	// RefreshOK is false while the refresher is failing (the database is down, say). The reason
	// is in the log, not here: this endpoint is readable by anyone who can reach the site.
	RefreshOK bool `json:"refresh_ok"`
}

// handleHealth answers 200 while there is a snapshot to serve, even an old one: the API is doing
// its job, and refresh_ok says whether the snapshot is still being renewed. Without a snapshot it answers 503.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	snap := s.snap.Load()
	if snap == nil {
		writeError(w, http.StatusServiceUnavailable, "no snapshot yet")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(healthJSON{
		Status: "ok", AsOfMinute: snap.AsOfMinute, GeneratedAt: snap.GeneratedAt,
		SnapshotAgeSeconds: int64(s.opts.now().Sub(snap.GeneratedAt).Seconds()),
		Vehicles:           snap.Vehicles, RefreshOK: s.lastErr.Load() == nil,
	})
}
