# MCP Gateway

[![CI](https://github.com/Ali-jj99/mcp-gateway/actions/workflows/ci.yml/badge.svg)](https://github.com/Ali-jj99/mcp-gateway/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

A reverse proxy that sits between AI agents and MCP servers. It handles
authentication, role-based access control, rate limiting, and audit logging so
you don't have to bolt those things onto every MCP server you deploy.

AI agents talk to MCP servers over HTTP using JSON-RPC. That's great for
getting things working, but it means any agent with the URL can call any tool
with no limits. MCP Gateway gives you a single place to control who can call
what, how often, and to keep a record of everything that happened.

## How it works

The gateway is a Go HTTP server that proxies MCP requests. A request hits the
gateway, gets authenticated via API key, checked against RBAC permissions,
logged to PostgreSQL, and rate-limited — all before it reaches your MCP server.
The response flows back through the same path. The MCP protocol itself is
untouched; the gateway is transparent to both the agent and the server.

```
Agent → Gateway (:8080/mcp) → Upstream MCP Server
                ↓
          PostgreSQL (audit logs, keys, roles, rate limits)
```

The gateway also ships with a web dashboard for managing keys, roles, and
viewing audit logs without touching SQL directly.

## Quick start

You need Go 1.26+, Docker (for PostgreSQL), and a few minutes.

**1. Start PostgreSQL:**

```bash
docker compose up postgres -d
```

**2. Set environment variables and run:**

```bash
export DATABASE_URL="postgres://mcp:mcp_secret@localhost:5432/mcp_gateway?sslmode=disable"
export UPSTREAM_URL="http://localhost:9090/mcp"
make run
```

The gateway starts on port 8080. Migrations run automatically on startup.

**3. Create an API key:**

```bash
make keygen
```

This prints a key like `mcpgw_a1b2c3d4...`. Save it — you won't see it again.

**4. Send a request through the gateway:**

```bash
curl -X POST http://localhost:8080/mcp \
  -H "Authorization: Bearer mcpgw_YOUR_KEY_HERE" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}'
```

**5. (Optional) Start the admin dashboard:**

```bash
export JWT_SECRET="your-secret-here"
export ADMIN_PASSWORD="your-password"
go run ./cmd/dashboard
```

Open `http://localhost:8081/dashboard` in your browser.

## Running with Docker

```bash
docker compose up --build -d
```

This starts both PostgreSQL and the gateway. Set `UPSTREAM_URL` in the
docker-compose environment to point to your MCP server.

## Configuration

Everything is configured through environment variables, following 12-factor
principles. No config files to manage.

### Gateway server

| Variable | Default | Description |
|---|---|---|
| `UPSTREAM_URL` | *(required)* | URL of the upstream MCP server |
| `DATABASE_URL` | *(empty)* | PostgreSQL connection string. When unset, auth and admin endpoints are disabled |
| `PORT` | `8080` | Gateway listen port |
| `MIGRATIONS_PATH` | `migrations` | Path to SQL migration files |
| `LOG_LEVEL` | `info` | Log verbosity: `debug`, `info`, `warn`, `error` |
| `LOG_FORMAT` | `json` | Log output format: `json` or `text` |
| `READ_TIMEOUT` | `15s` | HTTP server read timeout (Go duration) |
| `WRITE_TIMEOUT` | `30s` | HTTP server write timeout |
| `IDLE_TIMEOUT` | `60s` | HTTP server idle connection timeout |
| `SHUTDOWN_TIMEOUT` | `30s` | Grace period for in-flight requests on shutdown |
| `METRICS_ENABLED` | `true` | Enable Prometheus metrics endpoint |
| `METRICS_PORT` | `9090` | Port for the `/metrics` endpoint |
| `RATE_LIMIT_ENABLED` | `true` | Enable per-key rate limiting |
| `AUDIT_ENABLED` | `true` | Enable audit logging to PostgreSQL |

### Dashboard server

| Variable | Default | Description |
|---|---|---|
| `DATABASE_URL` | *(required)* | PostgreSQL connection string |
| `DASHBOARD_PORT` | `8081` | Dashboard listen port |
| `ADMIN_USER` | `admin` | Login username |
| `ADMIN_PASSWORD` | *(required)* | Login password |
| `JWT_SECRET` | *(required)* | Secret for signing session tokens |
| `MIGRATIONS_PATH` | `migrations` | Path to SQL migration files |

### Key generator CLI

| Flag / Variable | Default | Description |
|---|---|---|
| `DATABASE_URL` | *(required)* | PostgreSQL connection string |
| `-name` | *(required)* | Name label for the API key |
| `-expires` | *(no expiry)* | Expiration duration, e.g. `24h`, `720h` |

## API reference

### Gateway endpoints

**`POST /mcp`** — Proxy endpoint. Forwards JSON-RPC requests to the upstream
MCP server. Requires `Authorization: Bearer <key>` when a database is
configured.

**`GET /healthz`** — Liveness probe. Always returns `200 OK` with
`{"status":"ok"}`. Use this for Kubernetes liveness checks.

**`GET /readyz`** — Readiness probe. Returns `200` when the server is fully
initialized and the database is reachable. Returns `503` otherwise. Use this
for load balancer health checks.

**`GET /admin/health`** — Database health check. Returns `{"status":"ok"}` or
`{"status":"degraded"}` with a `503` if the database is unreachable. Only
available when `DATABASE_URL` is set.

### Metrics endpoint

**`GET /metrics`** (port 9090 by default) — Prometheus metrics. Exposed on a
separate port so you don't need to worry about auth or accidentally exposing it
to the internet through your main proxy.

Available metrics:

| Metric | Type | Labels | Description |
|---|---|---|---|
| `mcpgw_http_requests_total` | counter | `method`, `path`, `status` | Total HTTP requests processed |
| `mcpgw_http_request_duration_seconds` | histogram | `method`, `path` | Request latency distribution |
| `mcpgw_http_errors_total` | counter | `method`, `path`, `status` | Requests that returned 4xx or 5xx |
| `mcpgw_active_connections` | gauge | — | Number of requests currently being processed |

### Dashboard endpoints

The dashboard runs as a separate process on its own port. All routes are under
`/dashboard` and require login except the login page itself.

| Method | Path | Description |
|---|---|---|
| `GET` | `/dashboard` | Home page with stats |
| `GET` | `/dashboard/keys` | List and manage API keys |
| `POST` | `/dashboard/keys` | Create a new key |
| `POST` | `/dashboard/keys/{id}/revoke` | Revoke a key |
| `DELETE` | `/dashboard/keys/{id}` | Delete a key |
| `GET` | `/dashboard/audit` | Audit log viewer |
| `GET` | `/dashboard/roles` | List and manage roles |
| `POST` | `/dashboard/roles` | Create a role |
| `DELETE` | `/dashboard/roles/{id}` | Delete a role |
| `POST` | `/dashboard/roles/{roleID}/permissions` | Add a permission to a role |
| `DELETE` | `/dashboard/permissions/{id}` | Remove a permission |

### Error format

All error responses follow JSON-RPC 2.0 format:

```json
{
  "jsonrpc": "2.0",
  "error": {
    "code": -32001,
    "message": "rate limit exceeded"
  }
}
```

Rate-limited responses include a `Retry-After` header with the number of
seconds to wait.

## Querying audit logs

The audit log table captures every proxied request. Here are some useful
queries for understanding what's happening in your system.

**Who called what, and when:**

```sql
SELECT
    ak.name AS key_name,
    al.tool_name,
    al.status_code,
    al.latency_ms,
    al.created_at
FROM audit_logs al
JOIN api_keys ak ON ak.id = al.api_key_id
ORDER BY al.created_at DESC
LIMIT 50;
```

**Failed requests only:**

```sql
SELECT
    ak.name AS key_name,
    al.tool_name,
    al.status_code,
    al.ip,
    al.request_body,
    al.created_at
FROM audit_logs al
JOIN api_keys ak ON ak.id = al.api_key_id
WHERE al.status_code >= 400
ORDER BY al.created_at DESC;
```

**Requests per agent (API key):**

```sql
SELECT
    ak.name AS key_name,
    COUNT(*) AS total_requests,
    COUNT(*) FILTER (WHERE al.status_code >= 400) AS errors,
    ROUND(AVG(al.latency_ms)) AS avg_latency_ms
FROM audit_logs al
JOIN api_keys ak ON ak.id = al.api_key_id
GROUP BY ak.name
ORDER BY total_requests DESC;
```

**Requests today, broken down by hour:**

```sql
SELECT
    date_trunc('hour', al.created_at) AS hour,
    COUNT(*) AS requests,
    COUNT(*) FILTER (WHERE al.status_code >= 400) AS errors
FROM audit_logs al
WHERE al.created_at >= CURRENT_DATE
GROUP BY hour
ORDER BY hour;
```

## Development

```bash
make build        # compile gateway and keygen binaries
make test         # run tests with race detector
make lint         # run golangci-lint
make docker-up    # start PostgreSQL
make docker-down  # stop PostgreSQL
make sqlc         # regenerate Go code from SQL queries
make clean        # remove compiled binaries
```

## License

MIT — see [LICENSE](LICENSE) for details.
