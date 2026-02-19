-- name: ValidateAPIKey :one
SELECT id, name, is_active
FROM api_keys
WHERE key_hash = @key_hash AND is_active = TRUE AND revoked_at IS NULL;

-- name: TouchAPIKeyLastUsed :exec
UPDATE api_keys SET last_used_at = now() WHERE id = @id;

-- name: CreateAPIKey :one
INSERT INTO api_keys (key_hash, key_prefix, name)
VALUES (@key_hash, @key_prefix, @name)
RETURNING id, key_prefix, name, is_active, created_at;

-- name: RevokeAPIKey :exec
UPDATE api_keys SET is_active = FALSE, revoked_at = now() WHERE id = @id;

-- name: ListAPIKeys :many
SELECT id, key_prefix, name, is_active, created_at, last_used_at, revoked_at
FROM api_keys
ORDER BY created_at DESC;
