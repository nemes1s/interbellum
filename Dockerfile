# syntax=docker/dockerfile:1

# ---------------------------------------------------------------------------
# Build stage
# ---------------------------------------------------------------------------
FROM golang:1.25-alpine AS build

WORKDIR /src

# Dependencies are copied and downloaded first so that editing application
# code does not invalidate the (slow) module download layer.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO is disabled so the binary is fully static and runs on a bare Alpine
# image. Symbol and DWARF tables are stripped: they are only useful with a
# debugger attached, which is not how this binary is operated.
RUN CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w" \
        -o /out/server ./cmd/server

# ---------------------------------------------------------------------------
# Runtime stage
# ---------------------------------------------------------------------------
FROM alpine:3.20

# ca-certificates is needed for TLS connections to a managed PostgreSQL;
# wget backs the compose health check. Both are a few hundred KB.
RUN apk add --no-cache ca-certificates wget

# Run as an unprivileged user: the process needs no filesystem writes and no
# privileged ports, so there is nothing to gain from running as root.
RUN adduser -D -u 10001 -h /app indurex
WORKDIR /app

COPY --from=build /out/server /app/server

USER 10001:10001

# Documentation only; the actual bind address comes from HTTP_ADDR.
EXPOSE 8080

ENV HTTP_ADDR=:8080 \
    LOG_LEVEL=info \
    LOG_FORMAT=json

ENTRYPOINT ["/app/server"]
