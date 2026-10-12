// Command consumer reads positions.v1 and feed-status.v1 from Redpanda/Kafka and writes them to
// TimescaleDB, in one transaction per batch, committing the offsets only after the transaction.
// Delivery is at-least-once and the writes are idempotent, so a crash between the two is
// harmless. See docs/tasks/TASK-positions-consumer.md §2.4.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/BenitoPedro13/transit-pulse/packages/store"
)

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
		lg.Error("consumer stopped with an error", "error", err)
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

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()
	if err := store.Migrate(ctx, pool); err != nil {
		return err
	}
	c, err := newConsumer(cfg.Brokers, pool, lg, options{})
	if err != nil {
		return err
	}
	defer c.close()
	lg.Info("consumer started", "brokers", cfg.Brokers, "group", c.opts.group,
		"topics", []string{c.opts.positionsTopic, c.opts.statusTopic})

	if err := c.run(ctx); err != nil {
		return err
	}
	lg.Info("stopped cleanly", "skipped_undecodable_total", c.skippedTotal.Load())
	return nil
}
