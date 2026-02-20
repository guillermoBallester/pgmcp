-- name: CreateAPIKey :one
INSERT INTO api_keys (workspace_id, key_hash, key_prefix, name, created_by, expires_at)
VALUES (@workspace_id, @key_hash, @key_prefix, @name, @created_by, @expires_at)
RETURNING id, workspace_id, key_prefix, name, created_at;

-- name: ValidateAPIKey :one
SELECT id, workspace_id, name
FROM api_keys
WHERE key_hash = @key_hash
  AND (expires_at IS NULL OR expires_at > now());

-- name: TouchAPIKeyLastUsed :exec
UPDATE api_keys SET last_used_at = now() WHERE id = @id;

-- name: ListAPIKeysByWorkspace :many
SELECT id, key_prefix, name, created_by, expires_at, last_used_at, created_at
FROM api_keys
WHERE workspace_id = @workspace_id
ORDER BY created_at DESC;

-- name: DeleteAPIKey :exec
DELETE FROM api_keys WHERE id = @id AND workspace_id = @workspace_id;

-- name: GetAPIKeyDatabases :many
SELECT d.id, d.name, d.connection_type, d.status
FROM api_key_databases akd
JOIN databases d ON d.id = akd.database_id
WHERE akd.api_key_id = @api_key_id;

-- name: GrantAPIKeyDatabase :exec
INSERT INTO api_key_databases (api_key_id, database_id)
VALUES (@api_key_id, @database_id)
ON CONFLICT DO NOTHING;

-- name: RevokeAPIKeyDatabase :exec
DELETE FROM api_key_databases WHERE api_key_id = @api_key_id AND database_id = @database_id;
