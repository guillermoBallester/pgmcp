-- +goose Up
CREATE TABLE query_logs (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    database_id  UUID NOT NULL REFERENCES databases(id) ON DELETE CASCADE,
    key_id       UUID REFERENCES api_keys(id) ON DELETE SET NULL,
    tool_name    VARCHAR(100) NOT NULL,
    tool_input   TEXT,
    duration_ms  INTEGER NOT NULL,
    is_error     BOOLEAN NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_query_logs_workspace ON query_logs(workspace_id, created_at DESC);
CREATE INDEX idx_query_logs_database  ON query_logs(database_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS query_logs;
