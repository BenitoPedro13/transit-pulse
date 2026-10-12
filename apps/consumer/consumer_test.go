package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/BenitoPedro13/transit-pulse/packages/event"
	"github.com/BenitoPedro13/transit-pulse/packages/its"
	"github.com/BenitoPedro13/transit-pulse/packages/raw"
	"github.com/BenitoPedro13/transit-pulse/packages/samples"
	"github.com/BenitoPedro13/transit-pulse/packages/store"
	"github.com/BenitoPedro13/transit-pulse/packages/testharness"
)

const (
	minute0 = "2026-10-12T14:36:00Z"
	minute1 = "2026-10-12T14:37:00Z"
	minute2 = "2026-10-12T14:38:00Z"
	minute3 = "2026-10-12T14:39:00Z"
	minute4 = "2026-10-12T14:40:00Z"
)

// ---- helpers ----

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

// lines returns the JSON log lines written so far.
func (s *syncBuffer) lines() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []map[string]any
	for _, l := range strings.Split(s.b.String(), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(l), &m) == nil {
			out = append(out, m)
		}
	}
	return out
}

func (s *syncBuffer) count(msg string) int {
	n := 0
	for _, l := range s.lines() {
		if l["msg"] == msg {
			n++
		}
	}
	return n
}

// env is one Redpanda, one TimescaleDB container and an admin client, shared by the subtests.
// Every subtest gets its own topics, group and database.
type env struct {
	t       *testing.T
	broker  string
	conn    string // direct connection string of the container
	adm     *kadm.Client
	cl      *kgo.Client
	counter int
}

func newEnv(t *testing.T) *env {
	t.Helper()
	broker := testharness.StartRedpanda(t)
	cl, err := kgo.NewClient(kgo.SeedBrokers(broker), kgo.ProducerBatchCompression(kgo.NoCompression()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cl.Close)
	return &env{t: t, broker: broker, adm: kadm.NewClient(cl), cl: cl}
}

// sandbox is what one subtest runs in.
type sandbox struct {
	*env
	opts options
	pool *pgxpool.Pool
	dsn  string
}

func (e *env) sandbox(t *testing.T, conn string) *sandbox {
	t.Helper()
	e.counter++
	n := e.counter
	opts := options{
		group:          fmt.Sprintf("group-%d", n),
		positionsTopic: fmt.Sprintf("positions-%d", n),
		statusTopic:    fmt.Sprintf("status-%d", n),
	}
	ctx := context.Background()
	if _, err := e.adm.CreateTopics(ctx, 3, 1, nil, opts.positionsTopic, opts.statusTopic); err != nil {
		t.Fatal(err)
	}
	dsn := testharness.NewDatabase(t, conn, fmt.Sprintf("consumer_%d", n))
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	testharness.SettleRetention(t, dsn)
	return &sandbox{env: e, opts: opts, pool: pool, dsn: dsn}
}

// mapMinute maps the chosen supplier samples as the single page of a raw minute: the provider's
// bytes, untouched, with the header saying which minute and when "we got them".
func mapMinute(t *testing.T, minute string, suppliers ...string) ([]event.Position, []event.FeedStatus) {
	t.Helper()
	at, err := time.Parse(time.RFC3339, minute)
	if err != nil {
		t.Fatal(err)
	}
	var envs []raw.Envelope
	for i, s := range suppliers {
		body := samples.MustRead(t, "smtr-its-20261012-veiculos-"+s+".json")
		envs = append(envs, raw.Envelope{
			Header: raw.Header{V: 1, Source: "its", MinuteUTC: minute, Page: i, Status: 200,
				FetchedAt: at.Add(90 * time.Second), BodyLen: len(body)},
			Body: body,
		})
	}
	evs, st, err := its.MinuteEvents(envs)
	if err != nil {
		t.Fatal(err)
	}
	return evs, its.MinuteStatuses(minute, st, []string{raw.HourKey("its", at)})
}

func positionRecords(topic string, evs []event.Position) []*kgo.Record {
	out := make([]*kgo.Record, 0, len(evs))
	for _, e := range evs {
		v, _ := json.Marshal(e)
		out = append(out, &kgo.Record{Topic: topic, Key: []byte(e.Key()), Value: v})
	}
	return out
}

func statusRecords(topic string, sts []event.FeedStatus) []*kgo.Record {
	out := make([]*kgo.Record, 0, len(sts))
	for _, s := range sts {
		if s.RawKeys == nil {
			s.RawKeys = []string{}
		}
		v, _ := json.Marshal(s)
		out = append(out, &kgo.Record{Topic: topic, Key: []byte(s.Key()), Value: v})
	}
	return out
}

func (e *env) produce(recs ...*kgo.Record) []*kgo.Record {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	res := e.cl.ProduceSync(ctx, recs...)
	if err := res.FirstErr(); err != nil {
		e.t.Fatal(err)
	}
	out := make([]*kgo.Record, len(res))
	for i, r := range res {
		out[i] = r.Record
	}
	return out
}

// committed returns the group's committed offset per "topic/partition".
func (e *env) committed(group string) map[string]int64 {
	e.t.Helper()
	resp, err := e.adm.FetchOffsets(context.Background(), group)
	if err != nil {
		e.t.Fatal(err)
	}
	out := map[string]int64{}
	resp.Each(func(o kadm.OffsetResponse) {
		if o.Err == nil {
			out[fmt.Sprintf("%s/%d", o.Topic, o.Partition)] = o.At
		}
	})
	return out
}

func (e *env) ends(topics ...string) map[string]int64 {
	e.t.Helper()
	ends, err := e.adm.ListEndOffsets(context.Background(), topics...)
	if err != nil {
		e.t.Fatal(err)
	}
	out := map[string]int64{}
	ends.Each(func(o kadm.ListedOffset) {
		if o.Err == nil {
			out[fmt.Sprintf("%s/%d", o.Topic, o.Partition)] = o.Offset
		}
	})
	return out
}

// waitDrained waits until the group has committed every record of the sandbox's topics.
func (s *sandbox) waitDrained(timeout time.Duration) {
	s.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		ends, done := s.ends(s.opts.positionsTopic, s.opts.statusTopic), true
		got := s.committed(s.opts.group)
		for k, end := range ends {
			if end > 0 && got[k] != end {
				done = false
			}
		}
		if done {
			return
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("consumer did not drain within %v: committed %v, ends %v", timeout, got, ends)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

type running struct {
	c      *consumer
	cancel context.CancelFunc
	done   chan error
	logs   *syncBuffer
}

// start runs a consumer on pool. mutate, if set, runs before it starts and receives the function
// that cancels it.
func (s *sandbox) start(t *testing.T, pool *pgxpool.Pool, mutate func(c *consumer, cancel context.CancelFunc)) *running {
	t.Helper()
	logs := &syncBuffer{}
	c, err := newConsumer([]string{s.broker}, pool, slog.New(slog.NewJSONHandler(logs, nil)), s.opts)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if mutate != nil {
		mutate(c, cancel)
	}
	r := &running{c: c, cancel: cancel, done: make(chan error, 1), logs: logs}
	go func() { r.done <- c.run(ctx) }()
	t.Cleanup(func() { cancel(); c.close() })
	return r
}

// stop cancels the consumer, waits for run to return, and leaves the group.
func (r *running) stop(t *testing.T) {
	t.Helper()
	r.cancel()
	select {
	case err := <-r.done:
		if err != nil {
			t.Errorf("consumer returned %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("consumer did not stop within 10 s")
	}
	r.c.close()
}

func counts(t *testing.T, pool *pgxpool.Pool) (positions, statuses int) {
	t.Helper()
	ctx := context.Background()
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM positions`).Scan(&positions); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM feed_status`).Scan(&statuses); err != nil {
		t.Fatal(err)
	}
	return
}

func distinct(evs []event.Position) int {
	ids := map[string]bool{}
	for _, e := range evs {
		ids[e.Identity()] = true
	}
	return len(ids)
}

func checksums(t *testing.T, pool *pgxpool.Pool) (string, string) {
	t.Helper()
	_, a, err := store.Checksum(context.Background(), pool, "positions")
	if err != nil {
		t.Fatal(err)
	}
	_, b, err := store.Checksum(context.Background(), pool, "feed_status")
	if err != nil {
		t.Fatal(err)
	}
	return a, b
}

// sameTables fails the test unless both databases hold the same rows on every column.
func sameTables(t *testing.T, a, b *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pa, _ := store.ReadPositions(ctx, a, time.Time{}, time.Time{})
	pb, _ := store.ReadPositions(ctx, b, time.Time{}, time.Time{})
	if len(pa) != len(pb) {
		t.Fatalf("positions: %d rows vs %d", len(pa), len(pb))
	}
	for i := range pa {
		if !reflect.DeepEqual(pa[i], pb[i]) {
			t.Fatalf("positions differ at %s:\n A %+v\n B %+v", pa[i].Identity(), pa[i], pb[i])
		}
	}
	fa, _ := store.ReadFeedStatus(ctx, a)
	fb, _ := store.ReadFeedStatus(ctx, b)
	if !reflect.DeepEqual(fa, fb) {
		t.Fatalf("feed_status differs:\n A %+v\n B %+v", fa, fb)
	}
	pa1, fa1 := checksums(t, a)
	pb1, fb1 := checksums(t, b)
	if pa1 != pb1 || fa1 != fb1 {
		t.Errorf("checksums differ: %s/%s vs %s/%s", pa1, fa1, pb1, fb1)
	}
}

// ---- tests ----

func TestConsumer(t *testing.T) {
	e := newEnv(t)
	ctr := testharness.StartTimescaleContainer(t)
	e.conn = testharness.ConnString(t, ctr)

	// E2: the same events and statuses produced twice change nothing.
	t.Run("idempotency", func(t *testing.T) {
		s := e.sandbox(t, e.conn)
		ev0, st0 := mapMinute(t, minute0, "conecta", "zirix", "maxtrack", "sonda")
		ev1, st1 := mapMinute(t, minute1, "conecta", "zirix", "maxtrack", "sonda") // every identity repeats
		all := append(append([]event.Position{}, ev0...), ev1...)
		produceAll := func() {
			s.produce(positionRecords(s.opts.positionsTopic, all)...)
			s.produce(statusRecords(s.opts.statusTopic, append(append([]event.FeedStatus{}, st0...), st1...))...)
		}
		r := s.start(t, s.pool, nil)

		produceAll()
		s.waitDrained(30 * time.Second)
		rows, statuses := counts(t, s.pool)
		if rows != distinct(all) || rows != 160 || statuses != 8 {
			t.Fatalf("%d positions and %d statuses, want %d distinct identities and 8", rows, statuses, distinct(all))
		}
		p1, f1 := checksums(t, s.pool)

		produceAll() // the second pass: at-least-once delivery, twice over
		s.waitDrained(30 * time.Second)
		p2, f2 := checksums(t, s.pool)
		rows2, statuses2 := counts(t, s.pool)
		if p1 != p2 || f1 != f2 || rows2 != rows || statuses2 != statuses {
			t.Errorf("second pass changed the tables: positions %s -> %s, feed_status %s -> %s", p1, p2, f1, f2)
		}
		r.stop(t)
		if r.logs.count("batch committed") == 0 {
			t.Error("no 'batch committed' log line")
		}
	})

	// E4: replay = live. The same raw envelopes reach the database by two roads.
	t.Run("replay equals live", func(t *testing.T) {
		s := e.sandbox(t, e.conn)
		dsnB := testharness.NewDatabase(t, e.conn, "consumer_replay_b")

		// The raw log is made of captured provider bytes. The header cursors of the four-supplier
		// minutes are ours: the veiculos samples are first pages, so a header ends the chain after
		// each set; the body bytes are untouched.
		log := raw.Open(t.TempDir(), "its")
		put := func(minute string, page int, cursor, next *string, status int, body []byte) {
			at, _ := time.Parse(time.RFC3339, minute)
			if _, err := log.Append(raw.Header{MinuteUTC: minute, Page: page, Attempt: 1, Cursor: cursor,
				NextCursor: next, FetchedAt: at.Add(2*time.Minute + time.Duration(page)*time.Second), Status: status}, body); err != nil {
				t.Fatal(err)
			}
		}
		str := func(v string) *string { return &v }
		// minute0: the real three-page chain.
		var cur *string
		for i := range 3 {
			body := samples.MustRead(t, fmt.Sprintf("smtr-its-20261012-paged-p%d.json", i))
			var pg struct {
				NextCursor *string `json:"next_cursor"`
			}
			_ = json.Unmarshal(body, &pg)
			put(minute0, i, cur, pg.NextCursor, 200, body)
			cur = pg.NextCursor
		}
		// minute1 and minute2: the same four bodies, so every identity repeats across minutes.
		for _, m := range []string{minute1, minute2} {
			var prev *string
			for i, sup := range suppliersAll {
				var next *string
				if i < len(suppliersAll)-1 {
					next = str(fmt.Sprintf("c%d", i+1))
				}
				put(m, i, prev, next, 200, samples.MustRead(t, "smtr-its-20261012-veiculos-"+sup+".json"))
				prev = next
			}
		}
		// minute3: page 0 and then a real 422. Incomplete. minute4: nothing at all.
		put(minute3, 0, nil, str("c1"), 200, samples.MustRead(t, "smtr-its-20261012-veiculos-conecta.json"))
		put(minute3, 1, str("c1"), nil, 422, samples.MustRead(t, "smtr-its-20261012-error-422.json"))

		from, _ := time.Parse(time.RFC3339, minute0)
		to, _ := time.Parse(time.RFC3339, minute4)

		// Path A: what the adapter publishes. Newest minute first (its recovery order), then the
		// backlog; minute3 is `failed` while it can still be retried and `gap` once it cannot;
		// minute4 was never captured and is `gap`.
		published := func(minute string) ([]event.Position, []event.FeedStatus) {
			res, err := log.Scan(from, to, false)
			if err != nil {
				t.Fatal(err)
			}
			pages, ok := res.Index.Chain(minute)
			if !ok {
				t.Fatalf("minute %s has no complete chain", minute)
			}
			var envs []raw.Envelope
			var keys []string
			for _, p := range pages {
				env, err := log.Read(p.Loc)
				if err != nil {
					t.Fatal(err)
				}
				envs = append(envs, env)
				if len(keys) == 0 || keys[len(keys)-1] != p.Loc.Key {
					keys = append(keys, p.Loc.Key)
				}
			}
			evs, st, err := its.MinuteEvents(envs)
			if err != nil {
				t.Fatal(err)
			}
			return evs, its.MinuteStatuses(minute, st, keys)
		}
		r := s.start(t, s.pool, nil)
		for _, m := range []string{minute2, minute0, minute1} {
			evs, sts := published(m)
			s.produce(positionRecords(s.opts.positionsTopic, evs)...)
			s.produce(statusRecords(s.opts.statusTopic, sts)...)
		}
		s.produce(statusRecords(s.opts.statusTopic, its.EmptyStatuses(minute3, event.StatusFailed, []string{raw.HourKey("its", from)}))...)
		s.produce(statusRecords(s.opts.statusTopic, its.EmptyStatuses(minute4, event.StatusGap, []string{}))...)
		s.produce(statusRecords(s.opts.statusTopic, its.EmptyStatuses(minute3, event.StatusGap, []string{raw.HourKey("its", from)}))...)
		s.waitDrained(30 * time.Second)
		r.stop(t)

		// Path B: the real rebuild binary over the same raw log. Its database is migrated and its
		// retention job settled here, before any row exists: the binary's own Migrate then does
		// nothing, and a first retention run can't land in the middle of the rebuild.
		poolB, err := pgxpool.New(context.Background(), dsnB)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(poolB.Close)
		if err := store.Migrate(context.Background(), poolB); err != nil {
			t.Fatal(err)
		}
		testharness.SettleRetention(t, dsnB)
		bin := filepath.Join(t.TempDir(), "rebuild")
		if out, err := exec.Command("go", "build", "-o", bin, "github.com/BenitoPedro13/transit-pulse/apps/rebuild").CombinedOutput(); err != nil {
			t.Fatalf("build rebuild: %v\n%s", err, out)
		}
		cmd := exec.Command(bin, "-from", minute0, "-to", minute4)
		cmd.Env = append(os.Environ(), "RAW_DIR="+log.Dir(), "DATABASE_URL="+dsnB)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("rebuild: %v\n%s", err, out)
		}

		rows, statuses := counts(t, s.pool)
		if rows != 160+14 || statuses != 5*4 {
			t.Errorf("live tables hold %d positions and %d statuses, want 174 and 20", rows, statuses)
		}
		sameTables(t, s.pool, poolB)
	})

	// E5(a): a crash between the transaction commit and the offset commit.
	t.Run("at-least-once: crash after the database commit", func(t *testing.T) {
		s := e.sandbox(t, e.conn)
		s.opts.batchSize = 40
		ev0, st0 := mapMinute(t, minute0, "conecta", "zirix", "maxtrack", "sonda")
		s.produce(positionRecords(s.opts.positionsTopic, ev0)...)
		s.produce(statusRecords(s.opts.statusTopic, st0)...)

		// The hook stands in for a crash between steps 4 and 5: it cancels the consumer after the
		// first batch is in the database and before its offsets are committed.
		var once sync.Once
		first := s.start(t, s.pool, func(c *consumer, cancel context.CancelFunc) {
			c.afterDBCommit = func() { once.Do(cancel) }
		})
		select {
		case <-first.done:
		case <-time.After(30 * time.Second):
			t.Fatal("first consumer did not stop after its simulated crash")
		}
		first.c.close()

		rowsAfterCrash, _ := counts(t, s.pool)
		if rowsAfterCrash == 0 {
			t.Fatal("the crashed batch is not in the database")
		}
		for k, v := range s.committed(s.opts.group) {
			if v > 0 {
				t.Fatalf("offset %s=%d was committed before the crash", k, v)
			}
		}

		second := s.start(t, s.pool, nil)
		s.waitDrained(30 * time.Second)
		second.stop(t)
		rows, statuses := counts(t, s.pool)
		if rows != 160 || statuses != 4 {
			t.Errorf("%d positions and %d statuses after recovery, want 160 and 4 with no row missing", rows, statuses)
		}
		got, _ := store.ReadPositions(context.Background(), s.pool, time.Time{}, time.Time{})
		want := map[string]bool{}
		for _, ev := range ev0 {
			want[ev.Identity()] = true
		}
		for _, g := range got {
			delete(want, g.Identity())
		}
		if len(want) != 0 {
			t.Errorf("%d events are missing from the table", len(want))
		}
		t.Logf("the crashed batch left %d of 160 rows; the second consumer redelivered it", rowsAfterCrash)
	})

	// E5(b): the database goes away for 20 s while the consumer is draining.
	t.Run("at-least-once: database outage", func(t *testing.T) {
		container := testharness.StartTimescaleContainer(t)
		direct := testharness.ConnString(t, container)
		hostPort := func() (string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			host, err := container.Host(ctx)
			if err != nil {
				return "", err
			}
			port, err := container.MappedPort(ctx, "5432/tcp")
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("%s:%s", host, port.Port()), nil
		}
		proxy := testharness.NewProxy(t, hostPort)
		s := e.sandbox(t, direct)
		viaProxy, err := pgxpool.New(context.Background(), proxy.ConnString(t, s.dsn))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(viaProxy.Close)

		evA, stA := mapMinute(t, minute0, "conecta", "zirix")
		evB, stB := mapMinute(t, minute0, "maxtrack", "sonda")
		s.produce(positionRecords(s.opts.positionsTopic, evA)...)
		s.produce(statusRecords(s.opts.statusTopic, onlyFeeds(stA, "its:conecta", "its:zirix"))...)
		r := s.start(t, viaProxy, nil)
		s.waitDrained(30 * time.Second)
		if rows, _ := counts(t, s.pool); rows != len(evA) {
			t.Fatalf("%d rows before the outage, want %d", rows, len(evA))
		}

		stopTimeout := 10 * time.Second
		if err := container.Stop(context.Background(), &stopTimeout); err != nil {
			t.Fatal(err)
		}
		proxy.DropConnections()
		down := time.Now()
		s.produce(positionRecords(s.opts.positionsTopic, evB)...)
		s.produce(statusRecords(s.opts.statusTopic, onlyFeeds(stB, "its:maxtrack", "its:sonda"))...)
		time.Sleep(3 * time.Second)
		before := s.committed(s.opts.group)
		time.Sleep(time.Until(down.Add(20 * time.Second)))
		after := s.committed(s.opts.group)
		if !reflect.DeepEqual(before, after) {
			t.Errorf("committed offsets moved during the outage: %v -> %v", before, after)
		}
		ends := s.ends(s.opts.positionsTopic, s.opts.statusTopic)
		behind := false
		for k, end := range ends {
			if after[k] < end {
				behind = true
			}
		}
		if !behind {
			t.Error("the consumer had committed everything during the outage; the test did not catch it mid-drain")
		}
		if n := r.logs.count("database error; retrying the same batch"); n == 0 {
			t.Error("no retry was logged during the outage")
		}

		if err := container.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		// The proxy keeps the consumer's address; wait for the server to take connections again.
		deadline := time.Now().Add(60 * time.Second)
		for {
			conn, err := pgx.Connect(context.Background(), testharness.ConnString(t, container))
			if err == nil {
				_ = conn.Close(context.Background())
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("database did not come back: %v", err)
			}
			time.Sleep(500 * time.Millisecond)
		}
		t.Logf("host:port before the restart %s, after %s", hostOf(t, direct), hostOf(t, testharness.ConnString(t, container)))
		s.waitDrained(2 * time.Minute)
		r.stop(t)

		// The container's port may have changed: read the tables through its current address.
		checkPool, err := pgxpool.New(context.Background(), withHostOf(t, s.dsn, testharness.ConnString(t, container)))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(checkPool.Close)
		rows, statuses := counts(t, checkPool)
		if rows != len(evA)+len(evB) || statuses != 4 {
			t.Errorf("%d positions and %d statuses after the outage, want %d and 4", rows, statuses, len(evA)+len(evB))
		}
	})

	// E6: one broken message among valid ones.
	t.Run("undecodable record", func(t *testing.T) {
		s := e.sandbox(t, e.conn)
		ev0, st0 := mapMinute(t, minute0, "conecta", "zirix", "maxtrack", "sonda")
		half := len(ev0) / 2
		withUnknownField := positionRecords(s.opts.positionsTopic, ev0[:1])[0]
		withUnknownField.Value = bytes.Replace(withUnknownField.Value, []byte(`{`), []byte(`{"surprise":1,`), 1)

		key := []byte(ev0[0].Key())
		bad := []*kgo.Record{
			{Topic: s.opts.positionsTopic, Key: key, Value: []byte(`this is not json {{{`)},
			{Topic: s.opts.positionsTopic, Key: key, Value: withUnknownField.Value},
			{Topic: s.opts.statusTopic, Key: []byte("x"), Value: []byte(`{"feed":"its:conecta","status":"exploded"}`)},
		}
		// Valid records, then the broken ones, then more valid ones.
		s.produce(positionRecords(s.opts.positionsTopic, ev0[:half])...)
		badRes := s.produce(bad...)
		s.produce(positionRecords(s.opts.positionsTopic, ev0[half:])...)
		s.produce(statusRecords(s.opts.statusTopic, st0)...)

		r := s.start(t, s.pool, nil)
		s.waitDrained(30 * time.Second)
		r.stop(t)

		rows, statuses := counts(t, s.pool)
		if rows != 160 || statuses != 4 {
			t.Errorf("%d positions and %d statuses, want every valid record written (160 and 4)", rows, statuses)
		}
		if got := r.c.skippedTotal.Load(); got != 3 {
			t.Errorf("skipped %d records, want 3", got)
		}
		// Each skip is logged with topic, partition and offset.
		logged := map[string]bool{}
		for _, l := range r.logs.lines() {
			if l["msg"] == "undecodable record skipped" {
				logged[fmt.Sprintf("%v/%v/%v", l["topic"], l["partition"], l["offset"])] = true
				if v, _ := l["value"].(string); len(v) > snippetLen {
					t.Errorf("logged value is %d bytes, want at most %d", len(v), snippetLen)
				}
			}
		}
		for _, b := range badRes {
			k := fmt.Sprintf("%s/%d/%d", b.Topic, b.Partition, b.Offset)
			if !logged[k] {
				t.Errorf("skip of %s was not logged (logged: %v)", k, logged)
			}
		}
		var skippedInBatchLines int
		for _, l := range r.logs.lines() {
			if l["msg"] == "batch committed" {
				skippedInBatchLines += int(l["skipped_undecodable"].(float64))
			}
		}
		if skippedInBatchLines != 3 {
			t.Errorf("batch lines report %d skipped, want 3", skippedInBatchLines)
		}
	})
}

var suppliersAll = []string{"conecta", "zirix", "maxtrack", "sonda"}

// withHostOf returns dsn with the host and port of other.
func withHostOf(t *testing.T, dsn, other string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	o, err := url.Parse(other)
	if err != nil {
		t.Fatal(err)
	}
	u.Host = o.Host
	return u.String()
}

func onlyFeeds(sts []event.FeedStatus, feeds ...string) []event.FeedStatus {
	var out []event.FeedStatus
	for _, s := range sts {
		if slices.Contains(feeds, s.Feed) {
			out = append(out, s)
		}
	}
	return out
}

func hostOf(t *testing.T, connString string) string {
	t.Helper()
	u, err := url.Parse(connString)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host
}
