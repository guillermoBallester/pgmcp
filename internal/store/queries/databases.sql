-- name: CreateDatabase :one
INSERT INTO databases (workspace_id, name, connection_type, status)
VALUES (@workspace_id, @name, @connection_type, @status)
RETURNING id, workspace_id, name, connection_type, status, created_at, updated_at;

-- name: GetDatabaseByID :one
SELECT id, workspace_id, name, connection_type, encrypted_connection_url, status, created_at, updated_at
FROM databases
WHERE id = @id;

-- name: ListDatabasesByWorkspace :many
SELECT id, workspace_id, name, connection_type, status, created_at, updated_at
FROM databases
WHERE workspace_id = @workspace_id
ORDER BY created_at DESC;

-- name: DeleteDatabase :exec
DELETE FROM databases WHERE id = @id AND workspace_id = @workspace_id;

-- name: UpdateDatabaseStatus :exec
UPDATE databases SET status = @status, updated_at = now()
WHERE id = @id;

-- name: CreateDatabaseWithURL :one
INSERT INTO databases (workspace_id, name, connection_type, encrypted_connection_url, status)
VALUES (@workspace_id, @name, @connection_type, @encrypted_connection_url, @status)
RETURNING id, workspace_id, name, connection_type, status, created_at, updated_at;
