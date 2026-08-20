package postgres

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	// Registers the "pgx5" database driver used by pgxURL below.
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/nemes1s/interbellum/migrations"
)

// Migrate applies all pending migrations from the embedded migrations
// directory. It is safe to call from every replica on startup: golang-migrate
// takes a PostgreSQL advisory lock, so concurrent instances serialize and the
// losers observe an already-migrated schema rather than corrupting it.
//
// Applying migrations from the API process (rather than a separate job) is a
// deliberate simplification for this assignment — it makes
// `docker compose up --build` a single, transparent step. Production would
// normally run migrations as a distinct deployment stage so that a bad
// migration fails before any new application code starts serving; that
// trade-off is discussed in docs/architecture.md.
func Migrate(databaseURL string, log *slog.Logger) error {
	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("load embedded migrations: %w", err)
	}
	defer func() { _ = source.Close() }()

	m, err := migrate.NewWithSourceInstance("iofs", source, pgxURL(databaseURL))
	if err != nil {
		return fmt.Errorf("initialize migrator: %w", err)
	}
	defer func() {
		// Close returns (sourceErr, dbErr); the source is already closed above
		// and a failure to close the migrator's own connection is not fatal.
		if _, dbErr := m.Close(); dbErr != nil {
			log.Warn("closing migration connection failed", slog.Any("error", dbErr))
		}
	}()

	switch err := m.Up(); {
	case err == nil:
		version, dirty, verr := m.Version()
		if verr != nil {
			return fmt.Errorf("read schema version: %w", verr)
		}
		log.Info("database migrations applied",
			slog.Uint64("schema_version", uint64(version)),
			slog.Bool("dirty", dirty))
	case errors.Is(err, migrate.ErrNoChange):
		log.Info("database schema already up to date")
	default:
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// pgxURL rewrites a postgres:// URL to the scheme golang-migrate's pgx/v5
// driver registers itself under, so operators configure exactly one
// DATABASE_URL rather than one per consumer.
func pgxURL(databaseURL string) string {
	for _, prefix := range []string{"postgres://", "postgresql://"} {
		if rest, ok := strings.CutPrefix(databaseURL, prefix); ok {
			return "pgx5://" + rest
		}
	}
	return databaseURL
}
