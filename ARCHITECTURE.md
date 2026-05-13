# Architecture

## Overview

I split the project into three binaries in `cmd/`: **gateway** (the reverse
proxy that handles all MCP traffic), **dashboard** (admin web UI for managing
keys and viewing logs), and **keygen** (CLI for creating API keys). Gateway
and dashboard are independent processes that share a PostgreSQL database.

## Request lifecycle

What happens on `POST /mcp`:

1. **Middleware stack** -- request ID, structured logging, panic recovery,
   Prometheus instrumentation.
2. **Auth** -- SHA-256 hashes the Bearer token, looks it up in `api_keys`.
   Missing, expired, or revoked keys get a JSON-RPC error immediately.
3. **Rate limiting** -- per-key token bucket in memory. Returns a
   `Retry-After` header when exhausted.
4. **RBAC** -- reads the JSON-RPC body to extract the tool name, checks the
   key's roles and permissions for a matching resource/action pattern (`*`
   wildcards supported).
5. **Audit** -- captures request/response bodies, drops an entry into a
   buffered channel. A background goroutine writes to PostgreSQL async.
6. **Proxy** -- `httputil.ReverseProxy` forwards to upstream. Response flows
   back through the same chain.

Both RBAC and audit read the request body, so each uses a read-and-restore
pattern on `r.Body`.

## Package layout

Everything under `internal/`:

| Package | What it does |
|---|---|
| `auth` | API key generation, hashing, validation, auth middleware |
| `rbac` | Permission checking with in-memory cache per key ID |
| `ratelimit` | Token bucket per key; lazy init, 10-min idle cleanup |
| `audit` | Buffered channel (4096) + single writer goroutine; drops on overflow |
| `proxy` | Thin `httputil.ReverseProxy` wrapper with JSON-RPC error handling |
| `config` | Env var loader. Durations use `time.ParseDuration` format |
| `database` | PostgreSQL connection + file-based migrations from `migrations/` |
| `health` | `/healthz` (always 200) and `/readyz` (checks DB, 503 during shutdown) |
| `metrics` | Prometheus counters/histograms on a separate HTTP server |
| `middleware` | Request ID (`X-Request-ID`), JSON logging, panic recovery |
| `store` | sqlc-generated from `internal/store/queries.sql` -- don't hand-edit |
| `admin` | `/admin/health` endpoint for database checks |
| `dashboard` | Web UI: templ templates, chi routing, JWT session cookies |
| `models` | Domain types decoupled from sqlc-generated models |
| `policy` | Policy engine for request filtering rules |

## Database schema

Six tables, all UUID primary keys:

- **api_keys** -- SHA-256 hash (unique index), display prefix, optional expiry,
  active flag.
- **roles** -- named groups with descriptions.
- **permissions** -- resource/action pairs on a role. Wildcards supported.
- **api_key_roles** -- join table, many-to-many.
- **rate_limits** -- per-key overrides; gateway defaults apply when absent.
- **audit_logs** -- full request/response capture. Indexed by key ID, timestamp,
  tool name, status code.

## Key decisions

**Async audit logging** -- I used a buffered channel so audit writes don't add
latency to proxied requests. Entries can drop under extreme load; the count is
tracked via a logged metric.

**In-memory rate limiting** -- fast, but state isn't shared across instances.
For multi-instance deployments, I'd swap in a Redis-backed implementation.

**Separate metrics port** -- Prometheus scraping stays independent of proxy auth
and doesn't get accidentally exposed through the main proxy.

**Separate dashboard binary** -- I kept the dashboard separate so it can run
behind a VPN without affecting gateway availability or attack surface.

**sqlc over ORM** -- I write SQL, sqlc generates type-safe Go functions. No
runtime reflection, no query builder magic.

## Graceful shutdown

On `SIGTERM`/`SIGINT`: readiness probe returns 503, metrics server stops, main
server drains in-flight requests (up to `SHUTDOWN_TIMEOUT`), audit channel
flushes, DB pool closes.
