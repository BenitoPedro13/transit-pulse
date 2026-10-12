package main

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/BenitoPedro13/transit-pulse/packages/event"
	"github.com/BenitoPedro13/transit-pulse/packages/its"
)

const minuteLayout = "2006-01-02T15:04:05Z"

func fmtMinute(t time.Time) string { return t.UTC().Format(minuteLayout) }

func parseMinute(s string) (time.Time, error) {
	t, err := time.Parse(minuteLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("minute %q: %w", s, err)
	}
	return t, nil
}

// latestDue is the newest minute M with now >= M + 1 min + lag: a minute is only fetched once the
// provider has had the lag to finish it.
func latestDue(now time.Time, lag time.Duration) time.Time {
	return now.Add(-time.Minute - lag).UTC().Truncate(time.Minute)
}

// windowStart is the oldest minute still fetched: history is verified for 24 h, and the default
// window is 23 h. A minute older than this is never requested.
func windowStart(now time.Time, window time.Duration) time.Time {
	t := now.Add(-window).UTC()
	if f := t.Truncate(time.Minute); f.Before(t) {
		return f.Add(time.Minute)
	}
	return t
}

// load indexes the raw log inside the backfill window. A complete minute found here is never
// requested again, which is what makes a restart idempotent (invariant 3). A crash can leave a
// half-written last member; it is cut off so later appends stay readable.
func (a *adapter) load() error {
	now := a.cfg.Now()
	res, err := a.log.Scan(windowStart(now, a.cfg.BackfillWindow), now, true)
	if err != nil {
		return fmt.Errorf("scan raw log: %w", err)
	}
	complete := 0
	for _, m := range res.Index.Minutes() {
		if _, ok := res.Index.Chain(m); ok {
			complete++
		}
	}
	a.mu.Lock()
	a.idx = res.Index
	a.mu.Unlock()
	a.lg.Info("raw log loaded", "files", res.Files, "minutes_seen", len(res.Index.Minutes()), "minutes_complete", complete, "repaired", res.Repaired)
	return nil
}

// nextWork picks the minute to work on: the newest due minute if it is incomplete (live data
// first), otherwise the oldest incomplete one in the window. Minutes cooling down after a failure
// are skipped. ok is false when there is nothing to do.
func (a *adapter) nextWork(now time.Time) (minute string, ok bool) {
	lo, hi := windowStart(now, a.cfg.BackfillWindow), latestDue(now, a.cfg.MinuteLag)
	if hi.Before(lo) {
		return "", false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	ready := func(m string) bool {
		if _, done := a.idx.Chain(m); done {
			return false
		}
		return !now.Before(a.retryAt[m])
	}
	if m := fmtMinute(hi); ready(m) {
		return m, true
	}
	for t := lo; !t.After(hi); t = t.Add(time.Minute) {
		if m := fmtMinute(t); ready(m) {
			return m, true
		}
	}
	return "", false
}

// step fetches one page of the minute nextWork picks. It reports whether it did any work. An
// error is either a stop signal (ctx.Err() != nil) or something the adapter cannot continue past,
// such as a raw log it cannot write.
func (a *adapter) step(ctx context.Context) (bool, error) {
	now := a.cfg.Now()
	minute, ok := a.nextWork(now)
	if !ok {
		return false, nil
	}
	a.mu.Lock()
	pages, _ := a.idx.Chain(minute)
	a.mu.Unlock()
	var cursor *string
	if n := len(pages); n > 0 {
		cursor = pages[n-1].Header.NextCursor
	}

	out, err := a.fetchPage(ctx, minute, len(pages), cursor)
	if err != nil {
		return true, err
	}
	switch out {
	case pageOK:
		a.mu.Lock()
		_, done := a.idx.Chain(minute)
		a.mu.Unlock()
		if done {
			a.notify()
		}
	case pageRetryable:
		a.failMinute(minute, a.cfg.FailedCooldown)
	case pageRejected:
		a.failMinute(minute, a.cfg.BadRequestWait)
	}
	return true, nil
}

// failMinute records that a minute could not be fetched this cycle. It is tried again after the
// cooldown, until it falls out of the backfill window. One "failed" feed-status is queued per
// mapped feed: when the fetch fails the supplier list is not known (invariant 6).
func (a *adapter) failMinute(minute string, cooldown time.Duration) {
	now := a.cfg.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.retryAt[minute] = now.Add(cooldown)
	a.pending = append(a.pending, a.statusesFor(minute, event.StatusFailed)...)
	a.lg.Warn("minute failed, will retry", "minute", minute, "retry_after", cooldown.String())
}

// statusesFor builds one message per mapped feed with zero counts and the raw keys of every
// attempt made for the minute. The caller holds a.mu.
func (a *adapter) statusesFor(minute, status string) []event.FeedStatus {
	keys := []string{}
	for _, e := range a.idx.Attempts(minute) {
		if !slices.Contains(keys, e.Loc.Key) {
			keys = append(keys, e.Loc.Key)
		}
	}
	return its.EmptyStatuses(minute, status, keys)
}

// fetchLoop runs step until ctx is cancelled. It returns nil on a clean stop.
func (a *adapter) fetchLoop(ctx context.Context) error {
	for ctx.Err() == nil {
		worked, err := a.step(ctx)
		switch {
		case err != nil && ctx.Err() != nil:
			return nil
		case err != nil:
			return err
		case !worked:
			// Nothing due: check again in a second. A minute becomes due on a minute boundary
			// plus the lag, so one second of slack costs nothing.
			if err := sleep(ctx, time.Second); err != nil {
				return nil
			}
		}
	}
	return nil
}

// drain runs step until there is nothing left to do. Tests use it in place of fetchLoop.
func (a *adapter) drain(ctx context.Context) error {
	for {
		worked, err := a.step(ctx)
		if err != nil {
			return err
		}
		if !worked {
			return nil
		}
	}
}
