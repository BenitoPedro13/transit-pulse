// Command rebuild rebuilds the positions and feed_status tables from the raw log, one minute at a
// time and oldest first, with the same mapping and the same store writes the live consumer uses.
// It never fetches from a provider and never touches the raw log. See
// docs/tasks/TASK-positions-consumer.md §2.5.
//
//	rebuild -from 2026-10-12T14:27:00Z -to 2026-10-12T16:24:00Z
//
// Both minutes are inclusive. A minute in the range with no complete page chain in the raw log is
// final: it gets a "gap" feed_status row for every mapped feed, which is what the adapter reports
// once a minute leaves its backfill window. Reading RAW_DIR and writing DATABASE_URL, it is also
// the recovery when the consumer falls more than 24 h behind (the topic only keeps a day).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/BenitoPedro13/transit-pulse/packages/event"
	"github.com/BenitoPedro13/transit-pulse/packages/its"
	"github.com/BenitoPedro13/transit-pulse/packages/raw"
	"github.com/BenitoPedro13/transit-pulse/packages/store"
)

const (
	rawSource    = "its"
	minuteLayout = "2006-01-02T15:04:05Z"
)

func main() {
	lg := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				a.Value = slog.TimeValue(a.Value.Time().UTC())
			}
			return a
		},
	}))
	if err := run(lg); err != nil {
		lg.Error("rebuild stopped with an error", "error", err)
		os.Exit(1)
	}
}

func run(lg *slog.Logger) error {
	fromS := flag.String("from", "", "first minute to rebuild, RFC 3339 UTC on a minute boundary (inclusive)")
	toS := flag.String("to", "", "last minute to rebuild (inclusive)")
	flag.Parse()
	from, to, err := parseRange(*fromS, *toS)
	if err != nil {
		return err
	}
	rawDir := os.Getenv("RAW_DIR")
	if rawDir == "" {
		rawDir = "./var/raw"
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is empty (see .env.example)")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()
	if err := store.Migrate(ctx, pool); err != nil {
		return err
	}
	sum, err := Rebuild(ctx, raw.Open(rawDir, rawSource), pool, from, to, lg)
	if err != nil {
		return err
	}
	lg.Info("rebuild finished", "from", from.Format(minuteLayout), "to", to.Format(minuteLayout),
		"minutes", sum.Minutes, "complete", sum.Complete, "gaps", sum.Gaps, "rows_written", sum.RowsWritten,
		"statuses_written", sum.StatusesWritten, "elapsed_ms", sum.Elapsed.Milliseconds(),
		"rows_per_s", int64(float64(sum.RowsWritten)/max(sum.Elapsed.Seconds(), 0.001)))
	return nil
}

func parseRange(fromS, toS string) (from, to time.Time, err error) {
	parse := func(name, s string) (time.Time, error) {
		if s == "" {
			return time.Time{}, fmt.Errorf("-%s is required", name)
		}
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return time.Time{}, fmt.Errorf("-%s %q: %w", name, s, err)
		}
		t = t.UTC()
		if !t.Equal(t.Truncate(time.Minute)) {
			return time.Time{}, fmt.Errorf("-%s %q is not on a minute boundary", name, s)
		}
		return t, nil
	}
	if from, err = parse("from", fromS); err != nil {
		return
	}
	if to, err = parse("to", toS); err != nil {
		return
	}
	if to.Before(from) {
		err = fmt.Errorf("-to %s is before -from %s", toS, fromS)
	}
	return
}

// Summary is what a rebuild did.
type Summary struct {
	Minutes         int // minutes in the range
	Complete        int // with a complete page chain
	Gaps            int // without one
	RowsWritten     int64
	StatusesWritten int64
	Elapsed         time.Duration
}

// Rebuild writes every minute of [from, to] into the database, oldest first, one transaction per
// minute. A minute with a complete chain gives its positions and its MinuteStatuses; any other
// minute gives gap statuses only (the range is a closed past range, so an incomplete minute is
// final). Running it twice over the same raw log changes nothing.
func Rebuild(ctx context.Context, log *raw.Log, pool *pgxpool.Pool, from, to time.Time, lg *slog.Logger) (Summary, error) {
	start := time.Now()
	var sum Summary
	// A minute's attempts all live in the file of its own hour, so each hour is indexed on its
	// own and only one hour of headers is in memory at a time.
	for hour := from.Truncate(time.Hour); !hour.After(to); hour = hour.Add(time.Hour) {
		idx, err := indexHour(log, hour, lg)
		if err != nil {
			return sum, err
		}
		first := hour
		if from.After(first) {
			first = from
		}
		for m := first; m.Before(hour.Add(time.Hour)) && !m.After(to); m = m.Add(time.Minute) {
			if err := ctx.Err(); err != nil {
				return sum, err
			}
			rows, statuses, complete, err := rebuildMinute(ctx, log, pool, idx, m)
			if err != nil {
				return sum, fmt.Errorf("minute %s: %w", m.Format(minuteLayout), err)
			}
			sum.Minutes++
			if complete {
				sum.Complete++
			} else {
				sum.Gaps++
			}
			sum.RowsWritten += rows
			sum.StatusesWritten += statuses
			lg.Debug("minute rebuilt", "minute", m.Format(minuteLayout), "complete", complete, "rows_written", rows)
		}
	}
	sum.Elapsed = time.Since(start)
	return sum, nil
}

// indexHour reads the headers of one hour file. A file cut short by a crash is read up to the cut
// (the adapter repairs it on its next start; a rebuild never modifies the raw log).
func indexHour(log *raw.Log, hour time.Time, lg *slog.Logger) (*raw.Index, error) {
	res, err := log.Scan(hour, hour, false)
	switch {
	case err == nil:
	case errors.Is(err, raw.ErrTruncated):
		lg.Warn("raw file ends in an incomplete member; using the complete ones", "hour", hour.Format(minuteLayout))
	default:
		return nil, err
	}
	return res.Index, nil
}

func rebuildMinute(ctx context.Context, log *raw.Log, pool *pgxpool.Pool, idx *raw.Index, m time.Time) (rows, statuses int64, complete bool, err error) {
	minute := m.Format(minuteLayout)
	pages, complete := idx.Chain(minute)

	var (
		evs []event.Position
		sts []event.FeedStatus
	)
	if complete {
		envs := make([]raw.Envelope, 0, len(pages))
		var keys []string
		for _, e := range pages {
			env, err := log.Read(e.Loc)
			if err != nil {
				return 0, 0, false, err
			}
			envs = append(envs, env)
			if !slices.Contains(keys, e.Loc.Key) {
				keys = append(keys, e.Loc.Key)
			}
		}
		var st *its.Stats
		if evs, st, err = its.MinuteEvents(envs); err != nil {
			return 0, 0, false, err
		}
		sts = its.MinuteStatuses(minute, st, keys)
	} else {
		keys := []string{}
		for _, e := range idx.Attempts(minute) {
			if !slices.Contains(keys, e.Loc.Key) {
				keys = append(keys, e.Loc.Key)
			}
		}
		sts = its.EmptyStatuses(minute, event.StatusGap, keys)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, 0, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if rows, err = store.WritePositions(ctx, tx, evs); err != nil {
		return 0, 0, false, err
	}
	if statuses, err = store.WriteFeedStatus(ctx, tx, sts); err != nil {
		return 0, 0, false, err
	}
	return rows, statuses, complete, tx.Commit(ctx)
}
