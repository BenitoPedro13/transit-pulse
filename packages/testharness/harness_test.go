package testharness_test

import (
	"bytes"
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/BenitoPedro13/transit-pulse/packages/testharness"
)

const smokeTopic = "scaffold.smoke.v1"

// TestSmoke proves both containers start from the pinned images and do what later tasks rely on:
// Redpanda carries a record from producer to consumer, and TimescaleDB has TimescaleDB and PostGIS.
func TestSmoke(t *testing.T) {
	t.Run("redpanda", func(t *testing.T) {
		t.Parallel()
		start := time.Now()
		seed := testharness.StartRedpanda(t)
		t.Logf("redpanda started in %s", time.Since(start).Round(time.Millisecond))

		cl, err := kgo.NewClient(
			kgo.SeedBrokers(seed),
			kgo.AllowAutoTopicCreation(),
			kgo.ConsumeTopics(smokeTopic),
			kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		)
		if err != nil {
			t.Fatalf("kgo client: %v", err)
		}
		t.Cleanup(cl.Close)

		wantKey, wantValue := []byte("smoke"), []byte("hello transit-pulse")

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		roundTrip := time.Now()
		rec := &kgo.Record{Topic: smokeTopic, Key: wantKey, Value: wantValue}
		if err := cl.ProduceSync(ctx, rec).FirstErr(); err != nil {
			t.Fatalf("produce: %v", err)
		}

		var got *kgo.Record
		for got == nil {
			fetches := cl.PollFetches(ctx)
			if errs := fetches.Errors(); len(errs) > 0 {
				t.Fatalf("consume: %v", errs)
			}
			fetches.EachRecord(func(r *kgo.Record) {
				if got == nil {
					got = r
				}
			})
		}
		t.Logf("produce -> consume in %s", time.Since(roundTrip).Round(time.Millisecond))

		if !bytes.Equal(got.Key, wantKey) || !bytes.Equal(got.Value, wantValue) {
			t.Fatalf("got key %q value %q, want key %q value %q", got.Key, got.Value, wantKey, wantValue)
		}
	})

	t.Run("timescaledb", func(t *testing.T) {
		t.Parallel()
		start := time.Now()
		connString := testharness.StartTimescale(t)
		t.Logf("timescaledb started in %s", time.Since(start).Round(time.Millisecond))

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		conn, err := pgx.Connect(ctx, connString)
		if err != nil {
			t.Fatalf("connect: %v", err)
		}
		t.Cleanup(func() { _ = conn.Close(context.Background()) })

		rows, err := conn.Query(ctx, "SELECT postgis_version(), extversion FROM pg_extension WHERE extname = 'timescaledb'")
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		defer rows.Close()

		var n int
		var postgis, timescale string
		for rows.Next() {
			n++
			if err := rows.Scan(&postgis, &timescale); err != nil {
				t.Fatalf("scan: %v", err)
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("rows: %v", err)
		}
		if n != 1 {
			t.Fatalf("got %d rows, want 1", n)
		}
		if postgis == "" {
			t.Error("postgis_version() is empty")
		}
		if want := timescaleVersion(testharness.TimescaleImage); timescale != want {
			t.Errorf("timescaledb extversion = %q, want %q (from the image tag)", timescale, want)
		}
	})
}

// timescaleVersion extracts "2.30.2" from "timescale/timescaledb-ha:pg18.6-ts2.30.2".
func timescaleVersion(image string) string {
	i := strings.LastIndex(image, "-ts")
	if i < 0 {
		return ""
	}
	return image[i+len("-ts"):]
}

// failRecorder stands in for *testing.T to see whether a helper fails the test: Fatal records the
// message and ends the goroutine it was called from, as the real one does.
type failRecorder struct {
	testing.TB
	msg string
}

func (r *failRecorder) Fatal(args ...any) {
	r.msg = fmt.Sprint(args...)
	runtime.Goexit()
}

func (r *failRecorder) Fatalf(format string, args ...any) {
	r.msg = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

// TestSettleRetention: the helper waits for the retention job's first run, then pauses the job,
// and it fails when there is no job to settle.
func TestSettleRetention(t *testing.T) {
	container := testharness.StartTimescale(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	t.Run("waits for the first run, then pauses the job", func(t *testing.T) {
		connString := testharness.NewDatabase(t, container, "settle_ok")
		conn, err := pgx.Connect(ctx, connString)
		if err != nil {
			t.Fatalf("connect: %v", err)
		}
		t.Cleanup(func() { _ = conn.Close(context.Background()) })
		// The same shape as 0001_positions.sql, in plain SQL: testharness doesn't import the store.
		for _, q := range []string{
			`CREATE TABLE positions (event_time timestamptz NOT NULL)`,
			`SELECT create_hypertable('positions', 'event_time', chunk_time_interval => INTERVAL '1 day')`,
			`SELECT add_retention_policy('positions', drop_after => INTERVAL '3 days')`,
		} {
			if _, err := conn.Exec(ctx, q); err != nil {
				t.Fatalf("%s: %v", q, err)
			}
		}

		start := time.Now()
		testharness.SettleRetention(t, connString)
		t.Logf("first retention run settled %s after the policy was created", time.Since(start).Round(time.Millisecond))

		var scheduled bool
		var totalRuns *int64
		var status string
		if err := conn.QueryRow(ctx, `SELECT j.scheduled, js.total_runs, js.job_status
			FROM timescaledb_information.jobs j JOIN timescaledb_information.job_stats js USING (job_id)
			WHERE j.proc_name = 'policy_retention' AND j.hypertable_name = 'positions'`).Scan(&scheduled, &totalRuns, &status); err != nil {
			t.Fatalf("read job: %v", err)
		}
		if scheduled || status != "Paused" {
			t.Errorf("job scheduled = %v, status = %q; want paused", scheduled, status)
		}
		if totalRuns == nil || *totalRuns < 1 {
			t.Errorf("total_runs = %v, want at least 1: the helper must wait for the first run", totalRuns)
		}
	})

	t.Run("fails when the database has no retention job", func(t *testing.T) {
		connString := testharness.NewDatabase(t, container, "settle_none")
		rec := &failRecorder{TB: t}
		done := make(chan struct{})
		go func() {
			defer close(done)
			testharness.SettleRetention(rec, connString)
		}()
		<-done
		if !strings.Contains(rec.msg, "no retention job") {
			t.Errorf("SettleRetention on a database without the job failed with %q, want \"no retention job\"", rec.msg)
		}
	})
}
