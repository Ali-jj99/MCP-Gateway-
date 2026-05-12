-- name: GetAPIKeyByHash :one
SELECT id, name, key_hash, key_prefix, expires_at, active, created_at, updated_at
FROM api_keys
WHERE key_hash = $1;

-- name: CreateAPIKey :one
INSERT INTO api_keys (name, key_hash, key_prefix, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING id, name, key_hash, key_prefix, expires_at, active, created_at, updated_at;

-- name: ListAPIKeys :many
SELECT id, name, key_prefix, expires_at, active, created_at, updated_at
FROM api_keys
ORDER BY created_at DESC;

-- name: RevokeAPIKey :exec
UPDATE api_keys SET active = false, updated_at = NOW()
WHERE id = $1;

-- name: DeleteAPIKey :exec
DELETE FROM api_keys WHERE id = $1;

-- name: InsertAuditLog :exec
INSERT INTO audit_logs (api_key_id, action, resource, status_code, latency_ms, ip, request_body, response_body, tool_name)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: GetRateLimitByKeyID :one
SELECT id, api_key_id, requests_per_min, burst_size
FROM rate_limits
WHERE api_key_id = $1;

-- name: UpsertRateLimit :one
INSERT INTO rate_limits (api_key_id, requests_per_min, burst_size)
VALUES ($1, $2, $3)
ON CONFLICT (api_key_id)
DO UPDATE SET requests_per_min = EXCLUDED.requests_per_min, burst_size = EXCLUDED.burst_size
RETURNING id, api_key_id, requests_per_min, burst_size;

-- name: CreateRole :one
INSERT INTO roles (name, description) VALUES ($1, $2)
RETURNING id, name, description, created_at;

-- name: GetRoleByName :one
SELECT id, name, description, created_at
FROM roles
WHERE name = $1;

-- name: ListRoles :many
SELECT id, name, description, created_at
FROM roles
ORDER BY created_at;

-- name: DeleteRole :exec
DELETE FROM roles WHERE id = $1;

-- name: AddPermission :one
INSERT INTO permissions (role_id, resource, action) VALUES ($1, $2, $3)
RETURNING id, role_id, resource, action;

-- name: ListPermissionsByRole :many
SELECT id, role_id, resource, action
FROM permissions
WHERE role_id = $1;

-- name: DeletePermission :exec
DELETE FROM permissions WHERE id = $1;

-- name: AssignRoleToKey :exec
INSERT INTO api_key_roles (api_key_id, role_id) VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: RemoveRoleFromKey :exec
DELETE FROM api_key_roles WHERE api_key_id = $1 AND role_id = $2;

-- name: ListRolesForKey :many
SELECT r.id, r.name, r.description, r.created_at
FROM roles r
JOIN api_key_roles akr ON akr.role_id = r.id
WHERE akr.api_key_id = $1;

-- name: GetPermissionsByKeyID :many
SELECT p.resource, p.action
FROM permissions p
JOIN api_key_roles akr ON akr.role_id = p.role_id
WHERE akr.api_key_id = $1;

-- name: CountActiveKeys :one
SELECT COUNT(*) FROM api_keys WHERE active = true;

-- name: CountRequestsToday :one
SELECT COUNT(*) FROM audit_logs WHERE created_at >= CURRENT_DATE;

-- name: CountErrorsToday :one
SELECT COUNT(*) FROM audit_logs WHERE created_at >= CURRENT_DATE AND status_code >= 400;

-- name: ListAuditLogs :many
SELECT id, api_key_id, action, resource, status_code, latency_ms, ip, request_body, response_body, tool_name, created_at
FROM audit_logs
WHERE
    (sqlc.narg('api_key_id')::UUID IS NULL OR api_key_id = sqlc.narg('api_key_id')::UUID) AND
    (sqlc.narg('tool_name')::TEXT IS NULL OR tool_name = sqlc.narg('tool_name')::TEXT) AND
    (sqlc.narg('status_code')::INT IS NULL OR status_code = sqlc.narg('status_code')::INT) AND
    (sqlc.narg('start_time')::TIMESTAMPTZ IS NULL OR created_at >= sqlc.narg('start_time')::TIMESTAMPTZ) AND
    (sqlc.narg('end_time')::TIMESTAMPTZ IS NULL OR created_at <= sqlc.narg('end_time')::TIMESTAMPTZ)
ORDER BY created_at DESC
LIMIT sqlc.arg('page_limit');
