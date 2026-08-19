// Package migrations embeds the SQL migration files so the server binary can
// apply them without needing the repository checked out next to it. This is
// what makes `docker compose up --build` work with no migration sidecar
// container and no shell script: the API applies migrations at startup (see
// internal/repository/postgres/migrate.go).
package migrations

import "embed"

// FS holds the numbered up/down migration pairs in this directory.
//
//go:embed *.sql
var FS embed.FS
