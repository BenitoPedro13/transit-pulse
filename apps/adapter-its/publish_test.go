package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/BenitoPedro13/transit-pulse/packages/contracts"
	"github.com/BenitoPedro13/transit-pulse/packages/event"
	"github.com/BenitoPedro13/transit-pulse/packages/samples"
	"github.com/BenitoPedro13/transit-pulse/packages/testharness"
)

// readTopic returns every record in a topic that was created fresh for the test.
func readTopic(t *testing.T, broker, topic string) []*kgo.Record {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cl, err := kgo.NewClient(kgo.SeedBrokers(broker), kgo.ConsumeTopics(topic), kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	ends, err := kadm.NewClient(cl).ListEndOffsets(ctx, topic)
	if err != nil {
		t.Fatal(err)
	}
	var want int64
	ends.Each(func(o kadm.ListedOffset) { want += o.Offset })

	var out []*kgo.Record
	for int64(len(out)) < want {
		fetches := cl.PollFetches(ctx)
		if ctx.Err() != nil {
			t.Fatalf("read %s: got %d of %d records before the timeout", topic, len(out), want)
		}
		fetches.EachRecord(func(r *kgo.Record) { out = append(out, r) })
	}
	return out
}

func resetTopics(t *testing.T, broker string) {
	t.Helper()
	cl, err := kgo.NewClient(kgo.SeedBrokers(broker))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	// Errors for a topic that does not exist yet are fine.
	_, _ = kadm.NewClient(cl).DeleteTopics(context.Background(), topicPositions, topicFeedStatus)
}

func newTestPublisher(t *testing.T, a *adapter) *publisher {
	t.Helper()
	p, err := newPublisher(a)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.close(context.Background()) })
	if err := p.initCheckpoint(); err != nil {
		t.Fatal(err)
	}
	return p
}

func (f *fakeITS) adapterWithBroker(t *testing.T, dir, broker string) *adapter {
	t.Helper()
	cfg := testConfig(t, f, dir)
	cfg.Brokers = []string{broker}
	a := newAdapter(cfg, quietLogger())
	if err := a.load(); err != nil {
		t.Fatal(err)
	}
	return a
}

func distinctIdentities(t *testing.T, recs []*kgo.Record) int {
	t.Helper()
	ids := map[string]bool{}
	for _, r := range recs {
		var p event.Position
		if err := json.Unmarshal(r.Value, &p); err != nil {
			t.Fatal(err)
		}
		if string(r.Key) != p.Key() {
			t.Fatalf("message key %q != Key() %q", r.Key, p.Key())
		}
		contracts.MustValidate(t, "position.v1.schema.json", r.Value)
		ids[p.Identity()] = true
	}
	return len(ids)
}

func statusesOf(t *testing.T, recs []*kgo.Record) []event.FeedStatus {
	t.Helper()
	var out []event.FeedStatus
	for _, r := range recs {
		var s event.FeedStatus
		if err := json.Unmarshal(r.Value, &s); err != nil {
			t.Fatal(err)
		}
		if string(r.Key) != s.Key() {
			t.Fatalf("status key %q != Key() %q", r.Key, s.Key())
		}
		contracts.MustValidate(t, "feed-status.v1.schema.json", r.Value) // E2
		out = append(out, s)
	}
	return out
}

// One Redpanda container for all subtests; each starts from fresh topics.
func TestPublishAgainstRealRedpanda(t *testing.T) {
	broker := testharness.StartRedpanda(t)
	ctx := context.Background()
	const wantEvents = 14 // 5 + 5 + 4 records in the captured pages, all distinct

	t.Run("publish, then restart with and without the checkpoint (E5)", func(t *testing.T) {
		resetTopics(t, broker)
		f := newFakeITS(t)
		dir := t.TempDir()
		a := f.adapterWithBroker(t, dir, broker)
		if err := a.drain(ctx); err != nil {
			t.Fatal(err)
		}
		p := newTestPublisher(t, a)
		if err := p.cycle(ctx); err != nil {
			t.Fatal(err)
		}

		pos := readTopic(t, broker, topicPositions)
		if len(pos) != wantEvents || distinctIdentities(t, pos) != wantEvents {
			t.Fatalf("positions: %d records, %d distinct identities, want %d of each", len(pos), distinctIdentities(t, pos), wantEvents)
		}
		sts := statusesOf(t, readTopic(t, broker, topicFeedStatus))
		if len(sts) != 4 {
			t.Fatalf("feed-status messages = %d, want 4 (one per mapped feed)", len(sts))
		}
		for _, s := range sts {
			switch s.Feed {
			case "its:maxtrack":
				// The captured pages were requested with id_veiculo, so only maxtrack reports.
				if s.Status != event.StatusOK || s.Records != wantEvents || s.Events != wantEvents || s.Operator != "rio-sistema-rio" ||
					s.ProviderStatus == nil || *s.ProviderStatus != "ok" || len(s.RawKeys) != 1 {
					t.Errorf("maxtrack status = %+v", s)
				}
			default:
				// providers_status in the captured pages says "ok" for all four suppliers, and the
				// id_veiculo filter leaves these three with no records: "no vehicles reported" is
				// ok with zero, which is not the same as failed.
				if s.Status != event.StatusOK || s.ProviderStatus == nil || *s.ProviderStatus != "ok" || s.Records != 0 || s.Events != 0 {
					t.Errorf("%s status = %+v", s.Feed, s)
				}
			}
		}
		b, err := os.ReadFile(p.ckptPath)
		if err != nil || strings.TrimSpace(string(b)) != pagedMinute {
			t.Fatalf("checkpoint = %q (%v), want %s", b, err, pagedMinute)
		}

		// Restart with the checkpoint intact: nothing is published again.
		a2 := f.adapterWithBroker(t, dir, broker)
		p2 := newTestPublisher(t, a2)
		if err := p2.cycle(ctx); err != nil {
			t.Fatal(err)
		}
		if n := len(readTopic(t, broker, topicPositions)); n != wantEvents {
			t.Errorf("restart with checkpoint: %d records on the topic, want %d (nothing re-published)", n, wantEvents)
		}

		// Restart with the checkpoint deleted: everything is re-published from the raw log, and
		// the number of distinct identities does not change.
		if err := os.Remove(p.ckptPath); err != nil {
			t.Fatal(err)
		}
		before := f.requests()
		a3 := f.adapterWithBroker(t, dir, broker)
		p3 := newTestPublisher(t, a3)
		if err := a3.drain(ctx); err != nil {
			t.Fatal(err)
		}
		if err := p3.cycle(ctx); err != nil {
			t.Fatal(err)
		}
		if got := f.requests() - before; got != 0 {
			t.Errorf("re-publishing made %d requests to the provider, want 0", got)
		}
		pos = readTopic(t, broker, topicPositions)
		if len(pos) != 2*wantEvents || distinctIdentities(t, pos) != wantEvents {
			t.Errorf("after re-publish: %d records, %d distinct identities; want %d records and %d distinct", len(pos), distinctIdentities(t, pos), 2*wantEvents, wantEvents)
		}
		if b, _ := os.ReadFile(p3.ckptPath); strings.TrimSpace(string(b)) != pagedMinute {
			t.Errorf("checkpoint after re-publish = %q", b)
		}
	})

	t.Run("a 5xx then success publishes ok (E6)", func(t *testing.T) {
		resetTopics(t, broker)
		f := newFakeITS(t)
		f.wrap = func(n int, w http.ResponseWriter, r *http.Request) bool {
			if n == 0 {
				http.Error(w, "down", http.StatusBadGateway)
				return true
			}
			return false
		}
		a := f.adapterWithBroker(t, t.TempDir(), broker)
		if err := a.drain(ctx); err != nil {
			t.Fatal(err)
		}
		p := newTestPublisher(t, a)
		if err := p.cycle(ctx); err != nil {
			t.Fatal(err)
		}
		for _, s := range statusesOf(t, readTopic(t, broker, topicFeedStatus)) {
			if s.Feed == "its:maxtrack" && s.Status != event.StatusOK {
				t.Errorf("maxtrack status = %q, want ok", s.Status)
			}
		}
		if n := len(readTopic(t, broker, topicPositions)); n != wantEvents {
			t.Errorf("positions = %d, want %d", n, wantEvents)
		}
	})

	t.Run("a 4xx publishes failed for every mapped feed and no positions (E6)", func(t *testing.T) {
		resetTopics(t, broker)
		f := newFakeITS(t)
		body := samples.MustRead(t, "smtr-its-20261012-error-422.json")
		f.wrap = func(n int, w http.ResponseWriter, r *http.Request) bool {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write(body)
			return true
		}
		a := f.adapterWithBroker(t, t.TempDir(), broker)
		if err := a.drain(ctx); err != nil {
			t.Fatal(err)
		}
		p := newTestPublisher(t, a)
		if err := p.cycle(ctx); err != nil {
			t.Fatal(err)
		}
		sts := statusesOf(t, readTopic(t, broker, topicFeedStatus))
		if len(sts) != 4 {
			t.Fatalf("statuses = %d, want 4", len(sts))
		}
		feeds := map[string]bool{}
		for _, s := range sts {
			feeds[s.Feed] = true
			if s.Status != event.StatusFailed || s.MinuteUTC != pagedMinute || s.Events != 0 || s.ProviderStatus != nil {
				t.Errorf("status = %+v", s)
			}
		}
		if len(feeds) != 4 {
			t.Errorf("feeds = %v, want all four mapped feeds", feeds)
		}
		if n := len(readTopic(t, broker, topicPositions)); n != 0 {
			t.Errorf("positions = %d, want 0", n)
		}
		// The checkpoint did not move past the missing minute.
		if _, err := os.Stat(p.ckptPath); err == nil {
			t.Error("checkpoint written although the minute is missing")
		}
		// The failed messages are not sent twice.
		if err := p.cycle(ctx); err != nil {
			t.Fatal(err)
		}
		if n := len(readTopic(t, broker, topicFeedStatus)); n != 4 {
			t.Errorf("after a second cycle: %d statuses, want 4", n)
		}
	})

	t.Run("minutes older than the window are reported once as gaps", func(t *testing.T) {
		resetTopics(t, broker)
		f := newFakeITS(t)
		dir := t.TempDir()
		// The adapter was down: its checkpoint is 14:33, the window starts at 14:36, so 14:34 and
		// 14:35 can never be fetched.
		ckpt := dir + "/its/state/published-through"
		if err := os.MkdirAll(dir+"/its/state", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(ckpt, []byte("2026-10-12T14:33:00Z\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		a := f.adapterWithBroker(t, dir, broker)
		if err := a.drain(ctx); err != nil {
			t.Fatal(err)
		}
		p := newTestPublisher(t, a)
		for range 2 { // the second cycle must not repeat the gaps
			if err := p.cycle(ctx); err != nil {
				t.Fatal(err)
			}
		}
		gaps := map[string]int{}
		for _, s := range statusesOf(t, readTopic(t, broker, topicFeedStatus)) {
			if s.Status == event.StatusGap {
				gaps[s.MinuteUTC]++
			}
		}
		if len(gaps) != 2 || gaps["2026-10-12T14:34:00Z"] != 4 || gaps["2026-10-12T14:35:00Z"] != 4 {
			t.Errorf("gap statuses = %v, want 4 feeds for each of 14:34 and 14:35", gaps)
		}
		if b, _ := os.ReadFile(ckpt); strings.TrimSpace(string(b)) != pagedMinute {
			t.Errorf("checkpoint = %q, want %s", b, pagedMinute)
		}
	})
}
