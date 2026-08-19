// Command migrate applies the embedded database migrations and exits.
//
// The API applies migrations itself on startup, so this is not needed for
// `docker compose up`. It exists for two cases the server binary does not
// cover: running migrations as a separate deployment step (the production
// pattern described in docs/architecture.md), and preparing a database for the
// integration test suite without starting an API.
package main

import (
	"fmt"
	"os"

	"github.com/indurex/interbellum/internal/config"
	"github.com/indurex/interbellum/internal/logging"
	"github.com/indurex/interbellum/internal/repository/postgres"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: load configuration: %v\n", err)
		os.Exit(1)
	}

	log := logging.New(cfg.LogLevel, cfg.LogFormat)
	if err := postgres.Migrate(cfg.DatabaseURL, log); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}
