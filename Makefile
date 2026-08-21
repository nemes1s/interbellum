# Developer commands. Every target here works as written — nothing is
# aspirational.

# Database used by `make test-integration` and `make migrate`. Matches the
# docker-compose service, so `make docker-up && make test-integration` works
# with no further configuration.
TEST_DATABASE_URL ?= postgres://interbellum:interbellum@localhost:5432/interbellum?sslmode=disable
DATABASE_URL      ?= $(TEST_DATABASE_URL)

# Container tooling. Podman is CLI-compatible here, so `make COMPOSE="podman
# compose" docker-up` works, as does a docker->podman alias.
COMPOSE ?= docker compose

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help.
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

.PHONY: run
run: ## Run the API locally (expects PostgreSQL on localhost:5432; try `make docker-up-db`).
	DATABASE_URL="$(DATABASE_URL)" LOG_FORMAT=text go run ./cmd/server

.PHONY: build
build: ## Compile the server binary to ./bin/server.
	go build -trimpath -o bin/server ./cmd/server

.PHONY: test
test: ## Run unit tests (no database required).
	go test ./...

.PHONY: test-integration
test-integration: ## Run the full suite including database integration tests.
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test -race -count=1 ./...

# gofmt is a filesystem walk: unlike `go vet ./...` it does not respect module
# boundaries, so `gofmt -l .` descends into frontend/node_modules once anyone
# has run `npm ci` there (some npm packages ship vendored .go sources). Feeding
# it the tracked Go files instead keeps it to this repository's own code — and
# stops `make fmt` rewriting a dependency's vendored file.
GO_FILES = $(shell git ls-files '*.go' 2>/dev/null)

# Guard against an empty list: `gofmt -l` with no file arguments reads stdin and
# would hang rather than fail. Empty means this is not a git checkout.
.PHONY: check-go-files
check-go-files:
	@if [ -z "$(GO_FILES)" ]; then \
		echo "no tracked Go files found — these targets need a git checkout"; exit 1; \
	fi

.PHONY: lint
lint: check-go-files ## Run formatting and static checks.
	@echo "==> gofmt"
	@unformatted=$$(gofmt -l $(GO_FILES) 2>/dev/null); \
		if [ -n "$$unformatted" ]; then \
			echo "The following files are not gofmt-formatted:"; echo "$$unformatted"; exit 1; \
		fi
	@echo "==> go vet"
	@go vet ./...

.PHONY: fmt
fmt: check-go-files ## Format this repository's Go code.
	gofmt -w $(GO_FILES)

.PHONY: migrate
migrate: ## Apply database migrations without starting the API.
	DATABASE_URL="$(DATABASE_URL)" go run ./cmd/migrate

.PHONY: openapi-lint
openapi-lint: ## Validate the OpenAPI document (requires npx).
	npx --yes @redocly/cli lint api/openapi.yaml

# ---------------------------------------------------------------------------
# Web console (frontend/) — an optional client of the API above.
# ---------------------------------------------------------------------------

.PHONY: web-install
web-install: ## Install the web console's dependencies.
	cd frontend && npm ci

.PHONY: web-dev
web-dev: ## Run the console against a local API on :8080, serving http://localhost:3000.
	cd frontend && BACKEND_URL=$(BACKEND_URL) npm run dev

.PHONY: web-check
web-check: ## Lint, typecheck and test the web console.
	cd frontend && npm run lint && npm run typecheck && npm run test

# The console never calls the API from the browser; Next.js proxies to this
# address server-side. In compose it is http://api:8080.
BACKEND_URL ?= http://localhost:8080

# `docker compose up --wait` would be tidier, but it is Compose-v2 only and
# podman-compose rejects it. Polling /readyz and pg_isready keeps these targets
# working under both, and waits on the thing that actually matters — the
# service answering — rather than on the container being started.
.PHONY: docker-up
docker-up: ## Start PostgreSQL, the API and the web console, and wait until the API is ready.
	$(COMPOSE) up --build -d
	@printf 'waiting for the API to become ready'
	@for i in $$(seq 1 60); do \
		if curl -fsS http://localhost:8080/readyz >/dev/null 2>&1; then \
			printf '\nAPI ready at http://localhost:8080\nConsole at http://localhost:3000\n'; exit 0; \
		fi; \
		printf '.'; sleep 1; \
	done; \
	printf '\nAPI did not become ready in 60s; check `$(COMPOSE) logs api`\n'; exit 1

.PHONY: docker-up-db
docker-up-db: ## Start only PostgreSQL and wait for it to accept connections (useful with `make run`).
	$(COMPOSE) up -d db
	@printf 'waiting for PostgreSQL'
	@for i in $$(seq 1 60); do \
		if $(COMPOSE) exec -T db pg_isready -U interbellum -d interbellum >/dev/null 2>&1; then \
			printf '\nPostgreSQL ready on localhost:5432\n'; exit 0; \
		fi; \
		printf '.'; sleep 1; \
	done; \
	printf '\nPostgreSQL did not become ready in 60s\n'; exit 1

.PHONY: docker-down
docker-down: ## Stop the compose stack and remove its volumes.
	$(COMPOSE) down -v

.PHONY: docker-logs
docker-logs: ## Tail the API logs.
	$(COMPOSE) logs -f api

.PHONY: walkthrough
walkthrough: ## Run the README's end-to-end example against a running API.
	./scripts/walkthrough.sh
