# Developer commands. Every target here works as written — nothing is
# aspirational.

# Database used by `make test-integration` and `make migrate`. Matches the
# docker-compose service, so `make docker-up && make test-integration` works
# with no further configuration.
TEST_DATABASE_URL ?= postgres://indurex:indurex@localhost:5432/indurex?sslmode=disable
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

.PHONY: lint
lint: ## Run formatting and static checks.
	@echo "==> gofmt"
	@unformatted=$$(gofmt -l . 2>/dev/null); \
		if [ -n "$$unformatted" ]; then \
			echo "The following files are not gofmt-formatted:"; echo "$$unformatted"; exit 1; \
		fi
	@echo "==> go vet"
	@go vet ./...

.PHONY: fmt
fmt: ## Format all Go code.
	gofmt -w .

.PHONY: migrate
migrate: ## Apply database migrations without starting the API.
	DATABASE_URL="$(DATABASE_URL)" go run ./cmd/migrate

.PHONY: openapi-lint
openapi-lint: ## Validate the OpenAPI document (requires npx).
	npx --yes @redocly/cli lint api/openapi.yaml

# `docker compose up --wait` would be tidier, but it is Compose-v2 only and
# podman-compose rejects it. Polling /readyz and pg_isready keeps these targets
# working under both, and waits on the thing that actually matters — the
# service answering — rather than on the container being started.
.PHONY: docker-up
docker-up: ## Start PostgreSQL and the API via docker compose, and wait until the API is ready.
	$(COMPOSE) up --build -d
	@printf 'waiting for the API to become ready'
	@for i in $$(seq 1 60); do \
		if curl -fsS http://localhost:8080/readyz >/dev/null 2>&1; then \
			printf '\nAPI ready at http://localhost:8080\n'; exit 0; \
		fi; \
		printf '.'; sleep 1; \
	done; \
	printf '\nAPI did not become ready in 60s; check `$(COMPOSE) logs api`\n'; exit 1

.PHONY: docker-up-db
docker-up-db: ## Start only PostgreSQL and wait for it to accept connections (useful with `make run`).
	$(COMPOSE) up -d db
	@printf 'waiting for PostgreSQL'
	@for i in $$(seq 1 60); do \
		if $(COMPOSE) exec -T db pg_isready -U indurex -d indurex >/dev/null 2>&1; then \
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
