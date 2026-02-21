-- name: CreateAPIKey :one
INSERT INTO api_keys (workspace_id, database_id, key_hash, key_prefix, name)
VALUES (@workspace_id, @database_id, @key_hash, @key_prefix, @name)
RETURNING id, workspace_id, database_id, key_prefix, name, created_at;

-- name: ValidateAPIKey :one
SELECT id, workspace_id, name, database_id
FROM api_keys
WHERE key_hash = @key_hash
  AND (expires_at IS NULL OR expires_at > now());

-- name: TouchAPIKeyLastUsed :exec
UPDATE api_keys SET last_used_at = now() WHERE id = @id;

-- name: ListAPIKeysByWorkspace :many
SELECT id, key_prefix, name, database_id, expires_at, last_used_at, created_at
FROM api_keys
WHERE workspace_id = @workspace_id
ORDER BY created_at DESC;

-- name: DeleteAPIKey :exec
DELETE FROM api_keys WHERE id = @id AND workspace_id = @workspace_id;
