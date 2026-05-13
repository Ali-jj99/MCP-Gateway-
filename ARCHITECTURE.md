# Architecture

## Overview

I split the project into three binaries in `cmd/`. The **gateway** is the
reverse proxy that handles all MCP traffic. The **dashboard** is a web UI for
managing keys and viewing logs. The **keygen** is a CLI for creating API keys.
Gateway and dashboard are independent processes that share a PostgreSQL
database.

## Request lifecycle

When a request hits `POST /mcp`, it passes through six stages.

First, the middleware stack assigns a request ID, sets up structured logging,
recovers from panics, and records Prometheus metrics.

Second, the auth layer extracts the Bearer token from the Authorization header,
SHA-256 hashes it, and looks up the hash in the `api_keys` table. If the key is
missing, expired, or revoked, the request gets a JSON-RPC error and never
reaches upstream.

Third, the rate limiter checks whether this API key has exceeded its quota. Each
key gets an in-memory token bucket with a configurable refill rate and burst
size. If the bucket is empty, the response includes a `Retry-After` header.

Fourth, the RBAC layer reads the JSON-RPC body to extract the tool name. It
checks the key's roles and permissions for a matching resource/action pattern.
Patterns support `*` wildcards so `tool:*` grants access to everything.

Fifth, the audit layer captures request and response bodies and drops an entry
into a buffered channel. A background goroutine picks entries off the channel
and writes them to PostgreSQL.

Sixth, `httputil.ReverseProxy` forwards the request to the upstream MCP server.
The response flows back through the same middleware chain.

Both RBAC and audit need the request body, so each reads it into a buffer and
replaces `r.Body` with a new reader over that buffer.

## Package layout

Everything lives under `internal/`:

| Package | What it does |
|---|---|
| `auth` | API key generation, hashing, validation, auth middleware |
| `rbac` | Permission checking with in-memory cache per key ID |
| `ratelimit` | Token bucket per key, lazy init, 10 minute idle cleanup |
| `audit` | Buffered channel (4096) with single writer goroutine. Drops on overflow |
| `proxy` | Thin `httputil.ReverseProxy` wrapper with JSON-RPC error handling |
| `config` | Env var loader. Durations use `time.ParseDuration` format |
| `database` | PostgreSQL connection and file-based migrations from `migrations/` |
| `health` | `/healthz` always returns 200. `/readyz` checks DB, returns 503 during shutdown |
| `metrics` | Prometheus counters and histograms on a separate HTTP server |
| `middleware` | Request ID (`X-Request-ID`), JSON logging, panic recovery |
| `store` | sqlc-generated from `internal/store/queries.sql`. Do not hand-edit |
| `admin` | `/admin/health` endpoint for database checks |
| `dashboard` | Web UI with templ templates, chi routing, JWT session cookies |
| `models` | Domain types decoupled from sqlc-generated models |
| `policy` | Policy engine for request filtering rules |

## Database schema

There are six tables, all using UUID primary keys.

The `api_keys` table stores the SHA-256 hash of each key with a unique index,
a display prefix, an optional expiry timestamp, and an active flag for
revocation. The `roles` table holds named groups like "readonly" or "admin"
with a description. The `permissions` table stores resource/action string pairs
attached to a role, and both fields support wildcard patterns.

The `api_key_roles` table is a join table mapping keys to roles in a
many-to-many relationship. The `rate_limits` table holds per-key overrides for
request quotas. When a key has no entry here, the gateway defaults apply. The
`audit_logs` table captures every proxied request with the API key used, status
code, latency, client IP, request body, response body, and the tool name
extracted from the JSON-RPC payload. It is indexed by key ID, timestamp, tool
name, and status code.

## Key decisions

I used a buffered channel for audit logging so writes don't add latency to
proxied requests. The tradeoff is that entries can be dropped under extreme
load, but the count is tracked and logged so I know when it happens.

I kept rate limit state in memory rather than the database because checking a
rate limit needs to be fast. The downside is that state isn't shared across
multiple gateway instances. For a multi-instance setup I'd swap in Redis.

Prometheus metrics are served on a separate port so scraping stays independent
of proxy auth and doesn't get accidentally exposed through the main proxy.

I kept the dashboard as a separate binary so it can run behind a VPN without
affecting gateway availability or attack surface.

I chose sqlc over an ORM. I write SQL, sqlc generates type-safe Go functions.
No runtime reflection, no query builder.

## Graceful shutdown

When the gateway receives `SIGTERM` or `SIGINT`, the readiness probe
immediately starts returning 503 so load balancers stop sending traffic. The
metrics server shuts down next. Then the main server stops accepting new
connections and waits for in-flight requests to finish, up to the configured
`SHUTDOWN_TIMEOUT`. After that the audit logger's channel is closed and the
background goroutine finishes writing remaining entries. Finally the database
connection pool closes.
