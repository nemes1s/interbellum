// Package config loads runtime configuration from environment variables.
//
// Every setting has a default that is sensible for docker-compose, so a
// reviewer can start the system without writing a .env file, while nothing
// secret is ever compiled in: DATABASE_URL is the one value an operator must
// supply in a real deployment.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config is the fully-resolved runtime configuration.
type Config struct {
	// DatabaseURL is the PostgreSQL connection string.
	DatabaseURL string
	// HTTPAddr is the listen address, e.g. ":8080".
	HTTPAddr string
	// LogLevel is one of debug, info, warn, error.
	LogLevel string
	// LogFormat is "json" (production) or "text" (readable locally).
	LogFormat string

	// RunMigrations applies pending migrations at startup. On by default so
	// that `docker compose up` yields a working system in one step; see
	// docs/architecture.md for why production usually separates this.
	RunMigrations bool

	// MaxRequestBytes caps request bodies. Evidence payloads are JSON and
	// small; anything larger is either a mistake or an attempt to exhaust
	// memory, and large artifacts belong in object storage (domain-model §6).
	MaxRequestBytes int64

	// HTTP server timeouts. Kept explicit rather than relying on Go's
	// zero-value "no timeout", which is how servers accumulate stuck
	// connections.
	ReadTimeout       time.Duration
	ReadHeaderTimeout time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration

	// Connection pool sizing.
	DBMaxConns        int32
	DBMinConns        int32
	DBMaxConnLifetime time.Duration
	DBMaxConnIdleTime time.Duration
	DBConnectTimeout  time.Duration
}

// Load reads configuration from the environment, applying defaults.
func Load() (Config, error) {
	cfg := Config{
		DatabaseURL: env("DATABASE_URL",
			"postgres://interbellum:interbellum@localhost:5432/interbellum?sslmode=disable"),
		HTTPAddr:  env("HTTP_ADDR", ":8080"),
		LogLevel:  env("LOG_LEVEL", "info"),
		LogFormat: env("LOG_FORMAT", "json"),
	}

	var err error
	if cfg.RunMigrations, err = envBool("RUN_MIGRATIONS", true); err != nil {
		return Config{}, err
	}
	if cfg.MaxRequestBytes, err = envInt64("MAX_REQUEST_BYTES", 1<<20); err != nil { // 1 MiB
		return Config{}, err
	}
	if cfg.ReadTimeout, err = envDuration("HTTP_READ_TIMEOUT", 15*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.ReadHeaderTimeout, err = envDuration("HTTP_READ_HEADER_TIMEOUT", 5*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.WriteTimeout, err = envDuration("HTTP_WRITE_TIMEOUT", 30*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.IdleTimeout, err = envDuration("HTTP_IDLE_TIMEOUT", 60*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.ShutdownTimeout, err = envDuration("SHUTDOWN_TIMEOUT", 20*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.DBMaxConns, err = envInt32("DB_MAX_CONNS", 10); err != nil {
		return Config{}, err
	}
	if cfg.DBMinConns, err = envInt32("DB_MIN_CONNS", 2); err != nil {
		return Config{}, err
	}
	if cfg.DBMaxConnLifetime, err = envDuration("DB_MAX_CONN_LIFETIME", time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.DBMaxConnIdleTime, err = envDuration("DB_MAX_CONN_IDLE_TIME", 30*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.DBConnectTimeout, err = envDuration("DB_CONNECT_TIMEOUT", 10*time.Second); err != nil {
		return Config{}, err
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL must not be empty")
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) (bool, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return fallback, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s: %q is not a boolean", key, raw)
	}
	return v, nil
}

func envInt64(key string, fallback int64) (int64, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return fallback, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not an integer", key, raw)
	}
	return v, nil
}

func envInt32(key string, fallback int32) (int32, error) {
	v, err := envInt64(key, int64(fallback))
	if err != nil {
		return 0, err
	}
	return int32(v), nil
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return fallback, nil
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not a duration (e.g. 30s, 5m)", key, raw)
	}
	return v, nil
}
