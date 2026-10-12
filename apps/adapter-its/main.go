// Command adapter-its polls the Rio ITS vehicle-positions API one UTC minute at a time, stores
// every response byte for byte in the raw log, and publishes canonical position events and
// per-feed minute statuses to Redpanda/Kafka. See docs/tasks/TASK-its-adapter.md.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// flushTimeout and shutdownGrace together keep a clean stop under 10 s.
const flushTimeout = 3 * time.Second

func main() {
	// UTC timestamps, so log lines compare directly with the raw log's fetched_at.
	lg := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				a.Value = slog.TimeValue(a.Value.Time().UTC())
			}
			return a
		},
	}))
	if err := run(lg); err != nil {
		lg.Error("adapter-its stopped with an error", "error", err)
		os.Exit(1)
	}
}

func run(lg *slog.Logger) error {
	cfg, err := LoadConfig(os.Getenv)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	a := newAdapter(cfg, lg)
	if err := a.load(); err != nil {
		return err
	}
	pub, err := newPublisher(a)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	lg.Info("adapter-its started", "version", version, "brokers", cfg.Brokers, "raw_dir", cfg.RawDir,
		"minute_lag", cfg.MinuteLag.String(), "backfill_window", cfg.BackfillWindow.String(), "rate_per_sec", cfg.RatePerSec)

	// A fatal error in either loop stops the other. A signal is not an error.
	var (
		fatal error
		once  sync.Once
		wg    sync.WaitGroup
	)
	fail := func(err error) {
		once.Do(func() {
			fatal = err
			stop()
		})
	}
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := a.fetchLoop(ctx); err != nil {
			fail(err)
		}
	}()
	go func() {
		defer wg.Done()
		if err := pub.run(ctx); err != nil {
			fail(err)
		}
	}()
	<-ctx.Done()
	lg.Info("stopping")
	wg.Wait()

	flushCtx, flushCancel := context.WithTimeout(context.Background(), flushTimeout)
	defer flushCancel()
	pub.close(flushCtx)
	if fatal != nil {
		return fatal
	}
	lg.Info("stopped cleanly")
	return nil
}
