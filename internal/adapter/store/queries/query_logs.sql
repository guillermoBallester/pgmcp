-- name: InsertQueryLog :exec
INSERT INTO query_logs (workspace_id, database_id, key_id, tool_name, tool_input, duration_ms, is_error)
VALUES (@workspace_id, @database_id, @key_id, @tool_name, @tool_input, @duration_ms, @is_error);

-- name: ListQueryLogs :many
SELECT id, workspace_id, database_id, key_id, tool_name, tool_input, duration_ms, is_error, created_at
FROM query_logs
WHERE workspace_id = @workspace_id
  AND (@database_id::UUID IS NULL OR database_id = @database_id)
ORDER BY created_at DESC
LIMIT @result_limit;
