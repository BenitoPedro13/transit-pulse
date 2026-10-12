package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/BenitoPedro13/transit-pulse/packages/event"
	"github.com/BenitoPedro13/transit-pulse/packages/store"
)

const (
	defaultGroup    = "transit-pulse-consumer"
	topicPositions  = "positions.v1"
	topicFeedStatus = "feed-status.v1"

	defaultBatch = 10000
	// Database retries: 1 s doubling to a 30 s cap, logged at most once per warnEvery.
	defaultBackoffMin = time.Second
	defaultBackoffMax = 30 * time.Second
	warnEvery         = 30 * time.Second
	// attemptTimeout bounds one transaction; shutdownGrace is how long one in flight may keep
	// running after SIGINT/SIGTERM, so a clean stop stays under 10 s.
	attemptTimeout = 60 * time.Second
	shutdownGrace  = 8 * time.Second
	snippetLen     = 200
)

// options are the settings tests change. Production uses the zero value.
type options struct {
	group          string
	positionsTopic string
	statusTopic    string
	batchSize      int
	backoffMin     time.Duration
	backoffMax     time.Duration
}

func (o *options) applyDefaults() {
	if o.group == "" {
		o.group = defaultGroup
	}
	if o.positionsTopic == "" {
		o.positionsTopic = topicPositions
	}
	if o.statusTopic == "" {
		o.statusTopic = topicFeedStatus
	}
	if o.batchSize == 0 {
		o.batchSize = defaultBatch
	}
	if o.backoffMin == 0 {
		o.backoffMin = defaultBackoffMin
	}
	if o.backoffMax == 0 {
		o.backoffMax = defaultBackoffMax
	}
}

type consumer struct {
	opts options
	cl   *kgo.Client
	pool *pgxpool.Pool
	lg   *slog.Logger
	now  func() time.Time

	// afterDBCommit is called after a batch's transaction has committed and before its offsets
	// are. It is nil in production; a test uses it to stand in for a crash in between.
	afterDBCommit func()

	skippedTotal atomic.Int64
	lastWarn     time.Time
}

func newConsumer(brokers []string, pool *pgxpool.Pool, lg *slog.Logger, opts options) (*consumer, error) {
	opts.applyDefaults()
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID("transit-pulse-consumer"),
		kgo.ConsumerGroup(opts.group),
		kgo.ConsumeTopics(opts.positionsTopic, opts.statusTopic),
		// A group with no committed offsets starts at the beginning: the topic holds at most 24 h.
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		// Offsets are committed by hand, only after the batch is in the database. Rebalances are
		// blocked while a batch is in flight, so only partitions this member owns are committed.
		kgo.DisableAutoCommit(),
		kgo.BlockRebalanceOnPoll(),
	)
	if err != nil {
		return nil, fmt.Errorf("kafka client: %w", err)
	}
	return &consumer{opts: opts, cl: cl, pool: pool, lg: lg, now: time.Now}, nil
}

func (c *consumer) close() { c.cl.Close() }

// run consumes until ctx is cancelled. It returns nil on a clean stop. Every error it can hit
// while the broker or the database is down is retried, so a non-nil result means something the
// consumer cannot continue past.
func (c *consumer) run(ctx context.Context) error {
	for {
		stop, err := c.step(ctx)
		if err != nil || stop {
			return err
		}
	}
}

// step polls once and handles what came back. Any non-empty poll result, even one that only
// carries an error, blocks group rebalances until AllowRebalance (BlockRebalanceOnPoll), so it is
// called on every path out. Without it, leaving the group on shutdown would wait forever.
func (c *consumer) step(ctx context.Context) (stop bool, err error) {
	fetches := c.cl.PollRecords(ctx, c.opts.batchSize)
	defer c.cl.AllowRebalance()
	if ctx.Err() != nil || fetches.IsClientClosed() {
		return true, nil
	}
	fetches.EachError(func(topic string, partition int32, err error) {
		c.warn("fetch error", "topic", topic, "partition", partition, "error", err)
	})
	recs := fetches.Records()
	if len(recs) == 0 {
		return false, nil
	}
	switch err := c.handle(ctx, recs); {
	case err == nil:
		return false, nil
	case ctx.Err() != nil:
		return true, nil // stopped while retrying: nothing was committed, the batch comes back
	default:
		return true, err
	}
}

// batch is a poll decoded.
type batch struct {
	positions []event.Position
	statuses  []event.FeedStatus
	skipped   int
}

// handle writes one polled batch and then commits its offsets.
//
// The order is the point: transaction commit first, offsets second. A crash between the two
// re-delivers the batch, and the writes in packages/store make that harmless. The database failing
// never skips a batch: it is retried until it works or ctx is cancelled.
func (c *consumer) handle(ctx context.Context, recs []*kgo.Record) error {
	b := c.decode(recs)
	written, statusesWritten, err := c.writeWithRetry(ctx, b)
	if err != nil {
		return err
	}
	if c.afterDBCommit != nil {
		c.afterDBCommit()
	}
	if err := c.cl.CommitRecords(ctx, recs...); err != nil {
		// The rows are safe. The offsets are committed with the next batch, or the batch is
		// re-delivered after a restart or a rebalance, which is harmless.
		if ctx.Err() == nil {
			c.warn("offset commit failed; the batch is already in the database", "error", err)
		}
		return nil
	}

	oldest := recs[0].Timestamp
	for _, r := range recs {
		if r.Timestamp.Before(oldest) {
			oldest = r.Timestamp
		}
	}
	c.lg.Info("batch committed",
		"records", len(recs), "positions", len(b.positions), "written", written,
		"statuses", len(b.statuses), "statuses_written", statusesWritten,
		"skipped_undecodable", b.skipped, "oldest_record_age_ms", c.now().Sub(oldest).Milliseconds())
	return nil
}

func (c *consumer) writeWithRetry(ctx context.Context, b batch) (written, statusesWritten int64, err error) {
	delay := c.opts.backoffMin
	for attempt := 1; ; attempt++ {
		written, statusesWritten, err = c.write(ctx, b)
		if err == nil {
			return written, statusesWritten, nil
		}
		if ctx.Err() != nil {
			return 0, 0, ctx.Err()
		}
		c.warn("database error; retrying the same batch", "error", err, "attempt", attempt, "retry_in", delay.String())
		select {
		case <-ctx.Done():
			return 0, 0, ctx.Err()
		case <-time.After(delay):
		}
		delay = min(delay*2, c.opts.backoffMax)
	}
}

// write runs both writes in one transaction. A stop signal lets a transaction already running
// finish for shutdownGrace instead of cutting it off.
func (c *consumer) write(ctx context.Context, b batch) (written, statusesWritten int64, err error) {
	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), attemptTimeout)
	defer cancel()
	stopGrace := context.AfterFunc(ctx, func() { time.AfterFunc(shutdownGrace, cancel) })
	defer stopGrace()

	tx, err := c.pool.Begin(actx)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(actx)) }()
	if written, err = store.WritePositions(actx, tx, b.positions); err != nil {
		return 0, 0, err
	}
	if statusesWritten, err = store.WriteFeedStatus(actx, tx, b.statuses); err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(actx); err != nil {
		return 0, 0, err
	}
	return written, statusesWritten, nil
}

// decode turns records into events and statuses, strictly. An undecodable record is logged with
// its position and the start of its bytes, counted, and skipped: it is the only thing the
// consumer skips. The raw log is the source of truth, so `rebuild` recovers whatever the event
// would have been.
func (c *consumer) decode(recs []*kgo.Record) batch {
	var b batch
	for _, r := range recs {
		var err error
		switch r.Topic {
		case c.opts.positionsTopic:
			var p event.Position
			if p, err = decodePosition(r.Value); err == nil {
				b.positions = append(b.positions, p)
			}
		case c.opts.statusTopic:
			var s event.FeedStatus
			if s, err = decodeStatus(r.Value); err == nil {
				b.statuses = append(b.statuses, s)
			}
		default:
			err = fmt.Errorf("record from unexpected topic %q", r.Topic)
		}
		if err != nil {
			b.skipped++
			c.skippedTotal.Add(1)
			c.lg.Warn("undecodable record skipped", "topic", r.Topic, "partition", r.Partition,
				"offset", r.Offset, "error", err, "value", snippet(r.Value))
		}
	}
	return b
}

func snippet(v []byte) string {
	if len(v) > snippetLen {
		v = v[:snippetLen]
	}
	return strings.ToValidUTF8(string(v), "�")
}

// warn logs a repeating problem at most once per warnEvery.
func (c *consumer) warn(msg string, args ...any) {
	if now := time.Now(); now.Sub(c.lastWarn) >= warnEvery {
		c.lastWarn = now
		c.lg.Warn(msg, args...)
	}
}

// strictJSON decodes exactly one JSON value into v and rejects unknown fields and trailing data.
func strictJSON(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing data after the JSON value")
	}
	return nil
}

// decodePosition parses one positions.v1 message and checks what the database cannot take: a
// record that is valid JSON but would make every attempt to write its batch fail would otherwise
// block the consumer forever.
func decodePosition(data []byte) (event.Position, error) {
	var p event.Position
	if err := strictJSON(data, &p); err != nil {
		return p, err
	}
	for name, v := range map[string]string{
		"operator": p.Operator, "feed": p.Feed, "vehicle": p.Vehicle, "city": p.City, "mode": p.Mode,
		"authority": p.Authority, "source_kind": p.SourceKind, "raw_ref.key": p.RawRef.Key,
	} {
		if v == "" {
			return p, fmt.Errorf("%s is empty", name)
		}
	}
	if err := noNUL(map[string]string{"operator": p.Operator, "feed": p.Feed, "vehicle": p.Vehicle,
		"city": p.City, "mode": p.Mode, "authority": p.Authority, "source_kind": p.SourceKind,
		"line": deref(p.Line), "route_id": deref(p.RouteID), "raw_ref.key": p.RawRef.Key}); err != nil {
		return p, err
	}
	if p.EventTime.IsZero() || p.ReceivedTime.IsZero() {
		return p, errors.New("event_time or received_time is missing")
	}
	if _, err := time.Parse(time.RFC3339, p.RawRef.MinuteUTC); err != nil {
		return p, fmt.Errorf("raw_ref.minute_utc: %w", err)
	}
	if math.IsNaN(p.Lat) || math.IsNaN(p.Lon) || p.Lat < -90 || p.Lat > 90 || p.Lon < -180 || p.Lon > 180 {
		return p, errors.New("lat/lon out of range")
	}
	return p, nil
}

func decodeStatus(data []byte) (event.FeedStatus, error) {
	var s event.FeedStatus
	if err := strictJSON(data, &s); err != nil {
		return s, err
	}
	if s.Feed == "" || s.Operator == "" {
		return s, errors.New("feed or operator is empty")
	}
	switch s.Status {
	case event.StatusOK, event.StatusFailed, event.StatusProviderNotOK, event.StatusGap:
	default:
		return s, fmt.Errorf("status %q is not one of the four contract values", s.Status)
	}
	if _, err := time.Parse(time.RFC3339, s.MinuteUTC); err != nil {
		return s, fmt.Errorf("minute_utc: %w", err)
	}
	fields := map[string]string{"feed": s.Feed, "operator": s.Operator, "provider_status": deref(s.ProviderStatus)}
	for i, k := range s.RawKeys {
		fields[fmt.Sprintf("raw_keys[%d]", i)] = k
	}
	return s, noNUL(fields)
}

func noNUL(fields map[string]string) error {
	for name, v := range fields {
		if strings.ContainsRune(v, 0) {
			return fmt.Errorf("%s contains a NUL byte, which PostgreSQL cannot store", name)
		}
	}
	return nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
