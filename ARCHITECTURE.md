# Architecture

This document explains how the MCP Gateway is built, why certain decisions were
made, and where to find things in the code. It's meant for anyone who wants to
contribute or understand what's going on under the hood.

## The big picture

The gateway is a Go HTTP server that acts as a reverse proxy. It sits between
AI agents and MCP servers, intercepting every request to apply authentication,
authorization, rate limiting, and audit logging. The MCP protocol (JSON-RPC 2.0
over HTTP) passes through unchanged — the gateway doesn't interpret or modify
MCP messages beyond reading them for logging and permission checks.

There are three separate binaries in `cmd/`:

- **gateway** — the main proxy server that handles all MCP traffic
- **dashboard** — a web UI for managing keys, roles, and viewing audit logs
- **keygen** — a CLI tool for creating API keys from the command line

The gateway and dashboard run as independent processes. They share the same
PostgreSQL database but have no runtime dependency on each other.

## Request lifecycle

Here's what happens when an agent sends a request to `POST /mcp`:

1. The request hits chi's middleware stack: request ID generation, structured
   logging, panic recovery, and Prometheus metrics instrumentation.

2. The **auth middleware** extracts the Bearer token from the Authorization
   header, SHA-256 hashes it, and looks up the hash in the `api_keys` table. If
   the key is missing, expired, or revoked, the request gets a JSON-RPC error
   response and never reaches the upstream server.

3. The **rate limiter** checks whether this API key has exceeded its request
   quota. Each key gets a token bucket with a configurable refill rate and burst
   size. The bucket state lives in memory (not in the database) because
   checking a rate limit needs to be fast.

4. The **RBAC middleware** reads the request body to figure out which MCP tool
   is being called. It looks up the key's roles and permissions, and checks
   whether the tool and action match any granted permission patterns. Patterns
   support `*` wildcards, so `tool:*` grants access to all tools.

5. The **audit middleware** captures the request body, wraps the response
   writer to capture the response body and status code, and after the request
   completes, drops an entry into a buffered channel. A background goroutine
   picks up entries and writes them to PostgreSQL.

6. The **reverse proxy** forwards the request to the upstream MCP server using
   Go's `httputil.ReverseProxy`. The response flows back through the same
   middleware chain.

Both the RBAC middleware and the audit middleware need to read the request body,
so they each use a read-and-restore pattern: read the body into a buffer, then
replace `r.Body` with a new reader over that buffer so downstream handlers can
read it again.

## Package layout

Everything under `internal/` is private to this module. Here's what each
package does:

**auth** — API key generation and validation. Keys are 32 random bytes,
hex-encoded with a `mcpgw_` prefix. The plaintext key is shown to the user
once; only the SHA-256 hash is stored. The auth middleware lives here too.

**rbac** — Role-based access control. Permissions are stored as
resource/action string pairs attached to roles. API keys can have multiple
roles. The service caches permissions in memory per key ID to avoid hitting
the database on every request.

**ratelimit** — Token bucket rate limiting. Each API key gets its own bucket.
Default limits can be overridden per-key via the `rate_limits` table. Buckets
are lazily created on first request and cleaned up after 10 minutes of
inactivity.

**audit** — Async audit logging. The logger uses a buffered channel (default
4096 entries) and a single background goroutine that writes to PostgreSQL. If
the buffer fills up under extreme load, new entries are dropped rather than
blocking the request. Dropped entries are counted and logged.

**proxy** — A thin wrapper around `httputil.ReverseProxy`. It rewrites the
request URL to point at the upstream server, logs upstream response status, and
returns a JSON-RPC error if the upstream is unreachable.

**config** — Loads all configuration from environment variables. No config
files, no flags (except in the keygen CLI). Every setting has a sensible
default except `UPSTREAM_URL`, which is required. Duration values use Go's
`time.ParseDuration` format.

**database** — PostgreSQL connection setup and a simple file-based migration
system. Migrations are plain SQL files in the `migrations/` directory, applied
in filename order inside transactions. A `schema_migrations` table tracks
which ones have been applied.

**health** — Liveness (`/healthz`) and readiness (`/readyz`) probes. The
liveness probe always returns 200. The readiness probe checks that the server
has finished initialization and that the database is reachable. During graceful
shutdown, readiness is set to false before the server stops accepting
connections, giving load balancers time to drain traffic.

**metrics** — Prometheus instrumentation. Tracks request count, latency
histogram, error rate, and active connection count. The metrics endpoint runs on
a separate HTTP server (port 9090 by default) so it's not exposed through the
main proxy.

**middleware** — Shared HTTP middleware: request ID generation (reads or
generates `X-Request-ID`), structured JSON logging, and panic recovery.

**store** — Generated by sqlc from `internal/store/queries.sql`. This package
is not hand-written; run `make sqlc` to regenerate it after changing queries.

**admin** — A single handler that exposes `/admin/health` for database health
checks.

**dashboard** — The web UI server. Uses templ for HTML templates and chi for
routing. Authentication uses a simple username/password login with JWT session
cookies. The dashboard is deliberately separate from the gateway so you can run
it on an internal network that agents can't reach.

**models** — Core domain types. These are separate from the sqlc-generated
models so the rest of the codebase isn't coupled to the database schema.

## Database schema

Six tables, all using UUID primary keys:

**api_keys** — Stores the SHA-256 hash of each key, a display prefix, an
optional expiry timestamp, and an active flag for revocation. The key hash has a
unique index for fast lookups.

**roles** — Named groups like "readonly" or "admin" with a description. Roles
are the link between keys and permissions.

**permissions** — Each permission is a resource/action pair attached to a role.
Resources use a `tool:name` format. Both resource and action support wildcard
patterns for flexible matching.

**api_key_roles** — Join table mapping keys to roles. A key can have multiple
roles, and roles can be shared across keys.

**rate_limits** — Per-key rate limit overrides. If a key doesn't have an entry
here, the gateway's default rate limits apply.

**audit_logs** — Every proxied request creates a row here: the API key used,
HTTP method, path, status code, latency, client IP, request body, response
body, and the MCP tool name extracted from the JSON-RPC payload. Indexed by
key ID, timestamp, tool name, and status code.

## Key design decisions

**Go** — It's the standard language for proxies and gateways. The standard
library's `net/http` and `httputil` packages do most of the heavy lifting, and
Go's concurrency model makes it straightforward to handle the async audit
logger and per-key rate limiting.

**PostgreSQL** — Enterprise deployments expect it. It handles concurrent
writes well, which matters when audit logging is writing a row for every
proxied request. SQLite would be simpler for development but doesn't scale
for production use.

**sqlc** — Generates type-safe Go code from SQL queries. No ORM, no magic.
You write SQL, you get Go functions. The generated code lives in
`internal/store/` and shouldn't be edited by hand.

**Async audit logging** — Writing to the database on every request would add
latency. The buffered channel approach means the gateway can accept and forward
requests at full speed. The tradeoff is that entries can be dropped if the
buffer fills up, but this only happens under extreme load and the count is
tracked so you know if it's happening.

**Separate metrics port** — Prometheus metrics are served on their own port
rather than as a path on the main server. This avoids accidentally exposing
metrics through the proxy and keeps the auth middleware from interfering with
metric scraping.

**Separate dashboard process** — The admin dashboard is a different binary
from the gateway. This lets you run the dashboard behind a VPN or on an
internal network without affecting the gateway's availability or attack
surface.

**In-memory rate limiting** — Rate limit state is stored in memory, not in
the database. This means rate limits aren't shared across multiple gateway
instances. For single-instance deployments this is fine. For multi-instance
setups, you'd want to swap in a Redis-backed implementation.

## Graceful shutdown

The gateway handles `SIGTERM` and `SIGINT`. When a signal arrives:

1. The readiness probe (`/readyz`) immediately starts returning 503, telling
   load balancers to stop sending new traffic.
2. The metrics server shuts down.
3. The main server stops accepting new connections and waits for in-flight
   requests to complete, up to the configured `SHUTDOWN_TIMEOUT`.
4. The audit logger's channel is closed and the background goroutine finishes
   writing any remaining entries.
5. The database connection pool is closed.

This sequence ensures that no requests are dropped during a rolling deployment,
as long as your load balancer respects the readiness probe.
