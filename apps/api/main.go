// Command api serves the latest vehicle positions to the web app, read-only and from memory. A
// refresher goroutine looks at feed_status every 10 s; only when a newer minute (or a changed
// status) shows up does it run the one snapshot query, encode the answer and swap it in. A request
// never touches the database (invariant 5). See docs/tasks/TASK-live-map.md §2.3.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	// UTC timestamps, so log lines compare directly with the consumer's.
	lg := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				a.Value = slog.TimeValue(a.Value.Time().UTC())
			}
			return a
		},
	}))
	if err := run(lg); err != nil {
		lg.Error("api stopped with an error", "error", err)
		os.Exit(1)
	}
}

func run(lg *slog.Logger) error {
	cfg, err := LoadConfig(os.Getenv)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// pgxpool connects lazily, so a database that is down at start is a refresh error, not a
	// reason to refuse to start: the API keeps retrying and answers 503 until the first snapshot.
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()

	srv := newServer(pool, lg, options{})
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	hs := &http.Server{Handler: srv.routes(), ReadHeaderTimeout: 5 * time.Second}
	lg.Info("api started", "addr", ln.Addr().String(), "window_seconds", windowSeconds)

	refresherDone := make(chan struct{})
	go func() { defer close(refresherDone); srv.Run(ctx) }()

	serveErr := make(chan error, 1)
	go func() { serveErr <- hs.Serve(ln) }()
	select {
	case err := <-serveErr:
		stop()
		<-refresherDone
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := hs.Shutdown(sctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("shutdown: %w", err)
	}
	<-refresherDone
	lg.Info("stopped cleanly")
	return nil
}
