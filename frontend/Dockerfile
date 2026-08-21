# syntax=docker/dockerfile:1

# ---------------------------------------------------------------------------
# Dependencies
# ---------------------------------------------------------------------------
# Split from the build so that editing application code does not re-run the
# (slow) npm install layer.
FROM node:22-alpine AS deps

WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------
FROM node:22-alpine AS build

WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .

# `output: "standalone"` (next.config.ts) traces the exact server files needed
# and emits them under .next/standalone, so the runtime image carries no full
# node_modules tree and no build toolchain.
ENV NEXT_TELEMETRY_DISABLED=1
RUN npm run build

# ---------------------------------------------------------------------------
# Runtime
# ---------------------------------------------------------------------------
FROM node:22-alpine AS runtime

# wget backs the compose health check; it is already present in node:alpine's
# busybox, so nothing is installed here.
WORKDIR /app

ENV NODE_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    PORT=3000 \
    HOSTNAME=0.0.0.0

# The standalone bundle expects static assets alongside it; they are not traced
# into it because they are served, not imported.
COPY --from=build /app/.next/standalone ./
COPY --from=build /app/.next/static ./.next/static

# node:alpine ships an unprivileged `node` user. The server writes nothing to
# disk and binds an unprivileged port, so there is nothing to gain from root.
USER node

EXPOSE 3000

# BACKEND_URL is read at request time by the proxy route handler, never baked
# into the client bundle — which is why the same image works in compose
# (http://api:8080) and against a local API.
CMD ["node", "server.js"]
