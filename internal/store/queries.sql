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
