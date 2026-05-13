# Usage Reference

## API endpoints

### Gateway

| Method | Path | Auth | Description |
|---|---|---|---|
| `POST` | `/mcp` | Bearer token | Proxy JSON-RPC to upstream MCP server |
| `GET` | `/healthz` | None | Liveness probe, always returns 200 |
| `GET` | `/readyz` | None | Readiness probe, returns 503 if DB is unreachable |
| `GET` | `/admin/health` | None | Database health check |

### Dashboard

All routes under `/dashboard` require login except the login page itself.

| Method | Path | Description |
|---|---|---|
| `GET/POST` | `/dashboard/login` | Login page |
| `GET` | `/dashboard` | Home with stats |
| `GET/POST` | `/dashboard/keys` | API key management |
| `POST` | `/dashboard/keys/{id}/revoke` | Revoke a key |
| `DELETE` | `/dashboard/keys/{id}` | Delete a key |
| `GET` | `/dashboard/audit` | Audit log viewer |
| `GET/POST` | `/dashboard/roles` | Role management |
| `POST` | `/dashboard/roles/{roleID}/permissions` | Add permission |
| `DELETE` | `/dashboard/permissions/{id}` | Remove permission |

### Error format

All errors follow JSON-RPC 2.0 format:

```json
{"jsonrpc":"2.0","error":{"code":-32001,"message":"rate limit exceeded"}}
```

Rate-limited responses include a `Retry-After` header with the number of
seconds to wait.

## Prometheus metrics

I serve metrics on a separate port (default 9090) so they stay independent
of the proxy auth middleware.

| Metric | Type | Labels |
|---|---|---|
| `mcpgw_http_requests_total` | counter | `method`, `path`, `status` |
| `mcpgw_http_request_duration_seconds` | histogram | `method`, `path` |
| `mcpgw_http_errors_total` | counter | `method`, `path`, `status` |
| `mcpgw_active_connections` | gauge | none |

## Audit log queries

These are the queries I find most useful for checking what's going on.

```sql
-- recent requests
SELECT ak.name, al.tool_name, al.status_code, al.latency_ms, al.created_at
FROM audit_logs al JOIN api_keys ak ON ak.id = al.api_key_id
ORDER BY al.created_at DESC LIMIT 50;

-- errors only
SELECT ak.name, al.tool_name, al.status_code, al.ip, al.created_at
FROM audit_logs al JOIN api_keys ak ON ak.id = al.api_key_id
WHERE al.status_code >= 400
ORDER BY al.created_at DESC;

-- per-key summary
SELECT ak.name,
       COUNT(*) AS total,
       COUNT(*) FILTER (WHERE al.status_code >= 400) AS errors,
       ROUND(AVG(al.latency_ms)) AS avg_ms
FROM audit_logs al JOIN api_keys ak ON ak.id = al.api_key_id
GROUP BY ak.name ORDER BY total DESC;
```
