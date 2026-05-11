# Architecture

## Overview

MCP Gateway sits between API consumers and upstream MCP servers. Every request is authenticated via API key, checked against role-based permissions, rate-limited, proxied to the target MCP server, and logged for audit.

```
Client -> [Gateway] -> Upstream MCP Server
             |
          PostgreSQL
```

## Directory Layout

```
cmd/gateway/         Entry point, server setup, graceful shutdown
internal/
  admin/             Admin API (key management, health checks)
  auth/              API key hashing, validation, permission lookups
  config/            Environment-based configuration
  database/          PostgreSQL connection pool, file-based migrations
  middleware/         HTTP middleware chain (request ID, logging, panic recovery)
  models/            Domain types (APIKey, Role, Permission, RateLimit, AuditLog)
  proxy/             MCP protocol proxy to upstream servers
  ratelimit/         In-memory sliding-window rate limiter
migrations/          SQL migration files, applied in lexicographic order
```

## Database Schema

- **api_keys** - Client credentials (hashed), expiration, active flag
- **roles** - Named permission groups
- **permissions** - Resource + action pairs attached to roles
- **api_key_roles** - Many-to-many join between keys and roles
- **rate_limits** - Per-key rate limit configuration (per-min, per-hour, per-day)
- **audit_logs** - Request log with action, status, latency, client IP

## Request Flow

1. Request arrives, gets a request ID and is logged
2. API key extracted from `Authorization: Bearer <key>` header
3. Key is SHA-256 hashed and looked up in `api_keys`
4. Permissions loaded via `api_key_roles` + `permissions` join
5. Rate limit checked against in-memory counters (backed by `rate_limits` config)
6. Request proxied to the target MCP server
7. Response returned to client
8. Audit log entry written

## Design Decisions

- **File-based migrations** instead of a migration library — fewer dependencies, full control
- **SHA-256 key hashing** — API keys are high-entropy random strings, not passwords; SHA-256 is appropriate and fast
- **In-memory rate limiting** — simple and fast for single-instance deployment; can be replaced with Redis for multi-instance
- **slog for logging** — stdlib structured logging, no external dependency
