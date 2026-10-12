package testharness_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/BenitoPedro13/transit-pulse/internal/testharness"
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
