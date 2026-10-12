// Package testharness starts real Redpanda and TimescaleDB containers for integration tests.
// There are no mocks: a mocked broker or database proves nothing about ingestion (CLAUDE.md "Tests").
package testharness

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	"github.com/testcontainers/testcontainers-go/wait"
)

// The image tags below are the same as in compose.yaml: change both together.
const (
	RedpandaImage  = "docker.redpanda.com/redpandadata/redpanda:v26.2.4"
	TimescaleImage = "timescale/timescaledb-ha:pg18.6-ts2.30.2"
)

const (
	// DBName is also the user and password. Never "postgres": the postgres module's snapshot
	// feature drops the connected database.
	DBName = "transit_pulse"

	startTimeout = 3 * time.Minute
)

// StartRedpanda starts a single-node Redpanda with topic auto-creation and returns its seed
// broker. The container is terminated when t finishes. It fails the test if Docker is missing;
// it never skips, because a skipped integration test reads as green.
func StartRedpanda(t testing.TB) (seedBroker string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()

	ctr, err := redpanda.Run(ctx, RedpandaImage, redpanda.WithAutoCreateTopics())
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatalf("start redpanda: %v", err)
	}
	seedBroker, err = ctr.KafkaSeedBroker(ctx)
	if err != nil {
		t.Fatalf("redpanda seed broker: %v", err)
	}
	return seedBroker
}

// StartTimescale starts TimescaleDB with PostGIS, runs infra/postgres/initdb/001-extensions.sql
// (the same file compose.yaml mounts), and returns a connection string. The container is
// terminated when t finishes. It fails the test if Docker is missing; it never skips.
func StartTimescale(t testing.TB) (connString string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()

	ctr, err := postgres.Run(ctx, TimescaleImage,
		postgres.WithDatabase(DBName),
		postgres.WithUsername(DBName),
		postgres.WithPassword(DBName),
		postgres.WithInitScripts(initScript()),
		// The server restarts once after init, so the ready line appears twice. Without the
		// port wait the module is flaky on macOS.
		testcontainers.WithAdditionalWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
			wait.ForListeningPort("5432/tcp"),
		),
	)
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatalf("start timescaledb: %v", err)
	}
	connString, err = ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("timescaledb connection string: %v", err)
	}
	return connString
}

// initScript returns the path of infra/postgres/initdb/001-extensions.sql, found relative to
// this file (packages/testharness) so it works from any test's working directory.
func initScript() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "infra", "postgres", "initdb", "001-extensions.sql")
}
