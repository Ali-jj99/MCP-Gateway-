# MCP Gateway

[![CI](https://github.com/Ali-jj99/mcp-gateway/actions/workflows/ci.yml/badge.svg)](https://github.com/Ali-jj99/mcp-gateway/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

I built this because MCP servers have no built-in access control. Any agent
with the URL can call any tool with no limits. This gateway sits in front of
MCP servers and adds API key auth, role-based permissions, per-key rate
limiting, and full audit logging, all without touching the MCP servers
themselves.

![Dashboard](docs/screenshots/dashboard.png)

## Quick start

```bash
# start postgres
docker compose up postgres -d

# run the gateway
export DATABASE_URL="postgres://mcp:mcp_secret@localhost:5432/mcp_gateway?sslmode=disable"
export UPSTREAM_URL="http://localhost:9090/mcp"
make run

# generate an API key
make keygen

# send a request through the gateway
curl -X POST http://localhost:8080/mcp \
  -H "Authorization: Bearer mcpgw_YOUR_KEY_HERE" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}'

# start the admin dashboard (optional)
export ADMIN_PASSWORD="change-me"
go run ./cmd/dashboard
# open http://localhost:8081/dashboard
```

## Architecture

```
Agent --> Gateway (:8080/mcp) --> Upstream MCP Server
               |
          PostgreSQL
     (keys, roles, audit, rate limits)
```

I split the project into three binaries. The `gateway` is the reverse proxy
that handles MCP traffic. The `dashboard` is an admin web UI for managing keys,
roles, and viewing audit logs. The `keygen` is a small CLI for creating API
keys. Gateway and dashboard share a database but run as independent processes.
See [ARCHITECTURE.md](ARCHITECTURE.md) for internals.

## Screenshots

| Login | API Keys | Audit Logs |
|-------|----------|------------|
| ![Login](docs/screenshots/login.png) | ![API Keys](docs/screenshots/api-keys.png) | ![Audit Logs](docs/screenshots/audit-logs.png) |

## Configuration

All config is via environment variables.

### Gateway

| Variable | Default | Description |
|---|---|---|
| `UPSTREAM_URL` | *(required)* | Upstream MCP server URL |
| `DATABASE_URL` | *(empty)* | PostgreSQL connection string. Auth disabled when unset |
| `PORT` | `8080` | Listen port |
| `LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |
| `LOG_FORMAT` | `json` | `json` or `text` |
| `READ_TIMEOUT` | `15s` | HTTP read timeout |
| `WRITE_TIMEOUT` | `30s` | HTTP write timeout |
| `SHUTDOWN_TIMEOUT` | `30s` | Graceful shutdown grace period |
| `METRICS_ENABLED` | `true` | Prometheus `/metrics` on separate port |
| `METRICS_PORT` | `9090` | Metrics listen port |
| `RATE_LIMIT_ENABLED` | `true` | Per-key rate limiting |
| `AUDIT_ENABLED` | `true` | Audit logging to PostgreSQL |

### Dashboard

| Variable | Default | Description |
|---|---|---|
| `DATABASE_URL` | *(required)* | PostgreSQL connection string |
| `DASHBOARD_PORT` | `8081` | Listen port |
| `ADMIN_USER` | `admin` | Login username |
| `ADMIN_PASSWORD` | *(required)* | Login password |
| `JWT_SECRET` | *(auto-generated)* | Session token signing secret |

### Keygen

| Flag | Default | Description |
|---|---|---|
| `-name` | *(required)* | Key label |
| `-expires` | *(no expiry)* | Duration, e.g. `24h`, `720h` |

Requires `DATABASE_URL` to be set.

## Development

```bash
make build        # compile binaries
make test         # tests with race detector
make lint         # golangci-lint
make docker-up    # start postgres
make docker-down  # stop postgres
make sqlc         # regenerate store from SQL
```

See [docs/USAGE.md](docs/USAGE.md) for API endpoints, metrics, and audit log queries.

## License

MIT. See [LICENSE](LICENSE).
