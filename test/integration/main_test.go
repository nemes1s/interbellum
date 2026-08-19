// Package integration holds tests that run against a real PostgreSQL
// instance. They are skipped unless TEST_DATABASE_URL is set, so
// `go test ./...` stays fast and dependency-free for anyone who just cloned
// the repository, while CI and `make test-integration` run them for real.
//
// A real database is used rather than mocks on purpose: most of what these
// tests protect — composite foreign keys, unique constraints, row locking,
// transaction atomicity — has no behaviour at all in a mock. See the README's
// testing section.
package integration

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/indurex/interbellum/internal/repository/postgres"
)

// testPool is shared by every test in the package; each test resets the data
// it needs via resetDatabase.
var testPool *pgxpool.Pool

// databaseURL is empty when integration tests are disabled.
var databaseURL string

func TestMain(m *testing.M) {
	databaseURL = os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		// Not a failure: the suite is opt-in. Every test calls requireDB,
		// which skips with an explanatory message.
		os.Exit(m.Run())
	}

	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Migrating here rather than in each test means the suite exercises the
	// same migration path production uses.
	if err := postgres.Migrate(databaseURL, log); err != nil {
		panic("migrate test database: " + err.Error())
	}

	pool, err := postgres.NewPool(ctx, postgres.PoolConfig{DatabaseURL: databaseURL, MaxConns: 8})
	if err != nil {
		panic("connect to test database: " + err.Error())
	}
	testPool = pool

	code := m.Run()
	pool.Close()
	os.Exit(code)
}

// requireDB skips a test when integration testing is not configured.
func requireDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testPool == nil {
		t.Skip("TEST_DATABASE_URL is not set; skipping integration test " +
			"(run `make test-integration`, or see README > Testing)")
	}
	resetDatabase(t)
	return testPool
}

// resetDatabase truncates every table so tests do not observe each other's
// rows. TRUNCATE ... CASCADE is used rather than per-test schemas because the
// suite is small and this keeps the harness to three lines of SQL.
func resetDatabase(t *testing.T) {
	t.Helper()
	_, err := testPool.Exec(context.Background(), `
		TRUNCATE investigation_steps, investigations, alerts,
			playbook_edges, playbook_nodes, playbook_versions, playbooks
		RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("reset database: %v", err)
	}
}

// testLogger keeps test output readable: only warnings and errors surface.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}
