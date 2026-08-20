// Command server runs the Indurex Agentic Alert Investigation Engine API.
//
// This file is wiring only: load configuration, open the database, construct
// repositories/services/router, serve until a signal arrives. Every decision
// worth reviewing lives in internal/.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/nemes1s/interbellum/internal/config"
	httpapi "github.com/nemes1s/interbellum/internal/http"
	"github.com/nemes1s/interbellum/internal/logging"
	"github.com/nemes1s/interbellum/internal/repository/postgres"
	"github.com/nemes1s/interbellum/internal/service/alertservice"
	"github.com/nemes1s/interbellum/internal/service/investigationservice"
	"github.com/nemes1s/interbellum/internal/service/playbookservice"
)

func main() {
	if err := run(); err != nil {
		// The logger may not exist yet if configuration failed, so this last
		// resort writes plainly to stderr.
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	log := logging.New(cfg.LogLevel, cfg.LogFormat)
	log.Info("starting investigation engine",
		slog.String("http_addr", cfg.HTTPAddr),
		slog.String("log_level", cfg.LogLevel),
		slog.Bool("run_migrations", cfg.RunMigrations))

	// Cancelled on SIGINT/SIGTERM, which is what triggers graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if cfg.RunMigrations {
		if err := postgres.Migrate(cfg.DatabaseURL, log); err != nil {
			return fmt.Errorf("run migrations: %w", err)
		}
	}

	pool, err := postgres.NewPool(ctx, postgres.PoolConfig{
		DatabaseURL:     cfg.DatabaseURL,
		MaxConns:        cfg.DBMaxConns,
		MinConns:        cfg.DBMinConns,
		MaxConnLifetime: cfg.DBMaxConnLifetime,
		MaxConnIdleTime: cfg.DBMaxConnIdleTime,
		ConnectTimeout:  cfg.DBConnectTimeout,
	})
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()
	log.Info("database connection pool ready",
		slog.Int("max_conns", int(cfg.DBMaxConns)))

	playbookRepo := postgres.NewPlaybookRepository(pool)
	alertRepo := postgres.NewAlertRepository(pool)
	investigationRepo := postgres.NewInvestigationRepository(pool)

	router := httpapi.NewRouter(httpapi.Dependencies{
		Playbooks: playbookservice.New(playbookRepo, log),
		Alerts:    alertservice.New(alertRepo, log),
		Investigations: investigationservice.New(
			investigationRepo, alertRepo, playbookRepo, log),
		Logger:          log,
		Ping:            pool.Ping,
		MaxRequestBytes: cfg.MaxRequestBytes,
	})

	server := httpapi.NewServer(httpapi.ServerConfig{
		Addr:              cfg.HTTPAddr,
		ReadTimeout:       cfg.ReadTimeout,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		ShutdownTimeout:   cfg.ShutdownTimeout,
	}, router, log)

	if err := server.Run(ctx); err != nil {
		return fmt.Errorf("http server: %w", err)
	}
	log.Info("shutdown complete")
	return nil
}
