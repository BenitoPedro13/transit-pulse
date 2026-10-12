// Publishing to Redpanda/Kafka.
//
// Delivery is at-least-once. Events are built from the raw log and produced minute by minute. A
// checkpoint file records the newest minute such that it and every earlier minute are fully
// acked (or reported as a permanent gap). After a crash, everything after the checkpoint is
// produced again from the raw log, so the topic can hold the same event twice. That is harmless:
// the consumer is idempotent on Position.Identity() (architecture §10 #7).
//
// The broker is a buffer, not the record: the raw log is (invariant 2). A broker outage never
// stops raw capture; publishing catches up from the checkpoint when the broker returns.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/BenitoPedro13/transit-pulse/packages/event"
	"github.com/BenitoPedro13/transit-pulse/packages/raw"
)

const (
	topicPositions  = "positions.v1"
	topicFeedStatus = "feed-status.v1"

	topicPartitions  = 6
	topicReplication = 1
	// positionsRetention: the broker is a buffer, so a day is plenty.
	positionsRetentionMS = "86400000"

	produceChunk    = 2000
	deliveryTimeout = 30 * time.Second
	publishTick     = 2 * time.Second
	// maxGapReport bounds how many minutes of a long outage are reported as permanent gaps.
	maxGapReport = 7 * 24 * time.Hour
	// warnEvery rate-limits a repeating publish warning.
	warnEvery = 30 * time.Second
)

type publisher struct {
	a        *adapter
	cl       *kgo.Client
	adm      *kadm.Client
	ckptPath string

	checkpoint time.Time // everything up to and including this minute is done
	published  map[string]bool
	topicsOK   bool
	lastWarn   time.Time
}

func newPublisher(a *adapter) (*publisher, error) {
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(a.cfg.Brokers...),
		kgo.ClientID("transit-pulse-adapter-its"),
		kgo.ProducerBatchCompression(kgo.ZstdCompression()), // franz-go's default is snappy
		kgo.RequiredAcks(kgo.AllISRAcks()),                  // the default; idempotent writes are on by default too
		kgo.RecordDeliveryTimeout(deliveryTimeout),
	)
	if err != nil {
		return nil, fmt.Errorf("kafka client: %w", err)
	}
	return &publisher{
		a: a, cl: cl, adm: kadm.NewClient(cl),
		ckptPath:  filepath.Join(a.cfg.RawDir, rawSource, "state", "published-through"),
		published: map[string]bool{},
	}, nil
}

func (p *publisher) close(ctx context.Context) {
	if err := p.cl.Flush(ctx); err != nil {
		p.a.lg.Warn("flush on shutdown", "error", err)
	}
	p.cl.Close()
}

// pendingStatus aliases the message type for the adapter's queue of failed statuses.
type pendingStatus = event.FeedStatus

// initCheckpoint reads the checkpoint, or works out where to start without one: from the oldest
// minute in the raw log, or from the start of the window on a first run.
func (p *publisher) initCheckpoint() error {
	a := p.a
	now := a.cfg.Now()
	start := windowStart(now, a.cfg.BackfillWindow)

	b, err := os.ReadFile(p.ckptPath)
	switch {
	case err == nil:
		t, perr := parseMinute(strings.TrimSpace(string(b)))
		if perr != nil {
			return fmt.Errorf("checkpoint %s: %w", p.ckptPath, perr)
		}
		p.checkpoint = t
		if floor := start.Add(-maxGapReport); t.Before(floor) {
			a.lg.Warn("checkpoint is older than the gap-report limit; older minutes are not reported as gaps",
				"checkpoint", fmtMinute(t), "limit", maxGapReport.String())
			p.checkpoint = floor
		}
	case errors.Is(err, os.ErrNotExist):
		a.mu.Lock()
		minutes := a.idx.Minutes()
		a.mu.Unlock()
		p.checkpoint = start.Add(-time.Minute)
		if len(minutes) > 0 {
			if first, perr := parseMinute(minutes[0]); perr == nil && first.Add(-time.Minute).Before(p.checkpoint) {
				p.checkpoint = first.Add(-time.Minute)
			}
		}
		a.lg.Info("no checkpoint: publishing from the raw log", "from", fmtMinute(p.checkpoint.Add(time.Minute)))
	default:
		return fmt.Errorf("read checkpoint: %w", err)
	}
	return nil
}

func (p *publisher) writeCheckpoint() error {
	if err := os.MkdirAll(filepath.Dir(p.ckptPath), 0o755); err != nil {
		return err
	}
	tmp := p.ckptPath + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(fmtMinute(p.checkpoint) + "\n"); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, p.ckptPath)
}

// run publishes until ctx is cancelled. Errors are logged and retried on the next tick: the
// broker being down must never stop anything else.
func (p *publisher) run(ctx context.Context) error {
	if err := p.initCheckpoint(); err != nil {
		return err
	}
	tick := time.NewTicker(publishTick)
	defer tick.Stop()
	for {
		if err := p.cycle(ctx); err != nil && ctx.Err() == nil {
			p.warn("publish cycle failed, will retry", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-p.a.wake:
		case <-tick.C:
		}
	}
}

func (p *publisher) warn(msg string, err error) {
	if now := time.Now(); now.Sub(p.lastWarn) >= warnEvery {
		p.lastWarn = now
		p.a.lg.Warn(msg, "error", err)
	}
}

// cycle does one pass: topics, queued failed statuses, every complete unpublished minute in
// order, then the checkpoint.
func (p *publisher) cycle(ctx context.Context) error {
	if err := p.ensureTopics(ctx); err != nil {
		return err
	}
	if err := p.drainPending(ctx); err != nil {
		return err
	}
	for {
		m, ok := p.nextToPublish()
		if !ok {
			break
		}
		if err := p.publishMinute(ctx, m); err != nil {
			return fmt.Errorf("minute %s: %w", m, err)
		}
	}
	return p.advance(ctx)
}

func (p *publisher) ensureTopics(ctx context.Context) error {
	if p.topicsOK {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	topics := []struct {
		name    string
		configs map[string]*string
	}{
		{topicPositions, map[string]*string{"retention.ms": ptr(positionsRetentionMS)}},
		{topicFeedStatus, map[string]*string{"cleanup.policy": ptr("compact")}},
	}
	for _, t := range topics {
		_, err := p.adm.CreateTopic(ctx, topicPartitions, topicReplication, t.configs, t.name)
		if err != nil && !errors.Is(err, kerr.TopicAlreadyExists) {
			return fmt.Errorf("create topic %s: %w", t.name, err)
		}
	}
	p.topicsOK = true
	return nil
}

func ptr[T any](v T) *T { return &v }

// nextToPublish is the oldest complete minute that is not published yet.
func (p *publisher) nextToPublish() (string, bool) {
	a := p.a
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, m := range a.idx.Minutes() {
		if p.published[m] {
			continue
		}
		if t, err := parseMinute(m); err != nil || !t.After(p.checkpoint) {
			continue
		}
		if _, ok := a.idx.Chain(m); ok {
			return m, true
		}
	}
	return "", false
}

func (p *publisher) drainPending(ctx context.Context) error {
	a := p.a
	a.mu.Lock()
	batch := slices.Clone(a.pending)
	a.mu.Unlock()
	if len(batch) == 0 {
		return nil
	}
	recs, err := statusRecords(batch)
	if err != nil {
		return err
	}
	if err := p.produce(ctx, recs); err != nil {
		return err
	}
	a.mu.Lock()
	a.pending = a.pending[len(batch):]
	a.mu.Unlock()
	return nil
}

// produce sends records in chunks and returns on the first failure.
func (p *publisher) produce(ctx context.Context, recs []*kgo.Record) error {
	for chunk := range slices.Chunk(recs, produceChunk) {
		if err := p.cl.ProduceSync(ctx, chunk...).FirstErr(); err != nil {
			return err
		}
	}
	return nil
}

func statusRecords(sts []event.FeedStatus) ([]*kgo.Record, error) {
	out := make([]*kgo.Record, 0, len(sts))
	for _, s := range sts {
		if s.RawKeys == nil {
			s.RawKeys = []string{}
		}
		v, err := json.Marshal(s)
		if err != nil {
			return nil, err
		}
		out = append(out, &kgo.Record{Topic: topicFeedStatus, Key: []byte(s.Key()), Value: v})
	}
	return out, nil
}

// minuteStatuses builds the feed-status message of every mapped feed for a completed minute.
func minuteStatuses(minute string, st *Stats, rawKeys []string) []event.FeedStatus {
	var out []event.FeedStatus
	for _, f := range mappedFeeds() {
		fs := st.Feeds[f]
		if fs == nil {
			fs = &FeedStats{}
		}
		s := event.FeedStatus{
			Feed: f, Operator: feedOperators[f], MinuteUTC: minute, Status: event.StatusOK,
			Records: fs.Records, Events: fs.Events, Invalid: fs.Invalid, Unmapped: fs.Unmapped,
			ConflictingDuplicates: fs.ConflictingDuplicates, RawKeys: rawKeys,
		}
		if ps, ok := st.ProviderStatus[f]; ok {
			s.ProviderStatus = &ps
		}
		if s.ProviderStatus == nil || *s.ProviderStatus != "ok" {
			s.Status = event.StatusProviderNotOK
		}
		out = append(out, s)
	}
	return out
}

// publishMinute reads a complete minute back from the raw log, maps it with the same function a
// replay uses, and produces its events and then its feed-status messages. Only when every message
// is acked is the minute marked published.
func (p *publisher) publishMinute(ctx context.Context, minute string) error {
	a := p.a
	a.mu.Lock()
	pages, ok := a.idx.Chain(minute)
	pages = slices.Clone(pages)
	attempts := slices.Clone(a.idx.Attempts(minute))
	a.mu.Unlock()
	if !ok {
		return nil
	}

	envs := make([]raw.Envelope, 0, len(pages))
	var rawKeys []string
	for _, e := range pages {
		env, err := a.log.Read(e.Loc)
		if err != nil {
			return err
		}
		envs = append(envs, env)
		if !slices.Contains(rawKeys, e.Loc.Key) {
			rawKeys = append(rawKeys, e.Loc.Key)
		}
	}
	evs, st, err := MinuteEvents(envs)
	if err != nil {
		return err
	}

	recs := make([]*kgo.Record, 0, len(evs)+4)
	for _, e := range evs {
		v, err := json.Marshal(e)
		if err != nil {
			return err
		}
		recs = append(recs, &kgo.Record{Topic: topicPositions, Key: []byte(e.Key()), Value: v})
	}
	srecs, err := statusRecords(minuteStatuses(minute, st, rawKeys))
	if err != nil {
		return err
	}
	recs = append(recs, srecs...)
	if err := p.produce(ctx, recs); err != nil {
		return err
	}
	acked := a.cfg.Now()
	p.published[minute] = true

	t := st.Total()
	end, _ := parseMinute(minute)
	var fetchMS, gz int64
	for _, e := range attempts {
		fetchMS += e.Header.DurationMS
		gz += e.Loc.Len
	}
	a.lg.Info("minute published",
		"minute", minute, "requests", len(attempts), "pages", len(pages), "records", t.Records, "events", t.Events,
		"invalid", t.Invalid, "unmapped", t.Unmapped, "dups", t.Dups, "conflicting_duplicates", t.ConflictingDuplicates,
		"null_speed", t.NullSpeed, "null_heading", t.NullHeading,
		"fetch_ms", fetchMS, "raw_bytes_gz", gz, "lag_to_publish_ms", acked.Sub(end.Add(time.Minute)).Milliseconds())
	for _, c := range st.Conflicts {
		a.lg.Warn("conflicting duplicate", "minute", minute, "detail", c)
	}
	for _, u := range st.UnmappedTriples {
		a.warnUnmapped(minute, u)
	}
	return nil
}

// warnUnmapped logs each distinct unmapped triple once per hour.
func (a *adapter) warnUnmapped(minute, triple string) {
	a.mu.Lock()
	key := "unmapped|" + minute[:13] + "|" + triple
	seen := a.known[key]
	a.known[key] = true
	a.mu.Unlock()
	if !seen {
		a.lg.Warn("unmapped records, not emitted (the raw log still has them)", "minute", minute, "triple", triple)
	}
}

// advance moves the checkpoint over every minute that is done: published, or older than the
// window and still incomplete, which is a permanent gap and is reported once.
func (p *publisher) advance(ctx context.Context) error {
	a := p.a
	now := a.cfg.Now()
	start := windowStart(now, a.cfg.BackfillWindow)
	hi := latestDue(now, a.cfg.MinuteLag)
	moved := false

	for t := p.checkpoint.Add(time.Minute); !t.After(hi); t = t.Add(time.Minute) {
		m := fmtMinute(t)
		if !p.published[m] {
			a.mu.Lock()
			_, complete := a.idx.Chain(m)
			var gap []event.FeedStatus
			if !complete && t.Before(start) {
				gap = a.statusesFor(m, event.StatusGap)
			}
			a.mu.Unlock()
			if gap == nil {
				break
			}
			recs, err := statusRecords(gap)
			if err != nil {
				return err
			}
			if err := p.produce(ctx, recs); err != nil {
				return err
			}
			a.lg.Warn("permanent gap: minute is older than the backfill window and was never captured", "minute", m)
			p.published[m] = true
		}
		delete(p.published, m)
		p.checkpoint = t
		moved = true
	}
	if moved {
		if err := p.writeCheckpoint(); err != nil {
			return fmt.Errorf("write checkpoint: %w", err)
		}
	}

	// Forget the headers of minutes that are done and no longer inside the window.
	a.mu.Lock()
	for _, m := range a.idx.Minutes() {
		if t, err := parseMinute(m); err == nil && !t.After(p.checkpoint) && t.Before(start.Add(-time.Hour)) {
			a.idx.Delete(m)
			delete(a.retryAt, m)
		}
	}
	a.mu.Unlock()
	return nil
}
