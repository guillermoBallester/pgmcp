-- +goose Up

-- ==========================================
-- 1. USERS & WORKSPACES
-- ==========================================
CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    clerk_user_id VARCHAR(255) NOT NULL UNIQUE,
    email         VARCHAR(255) NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE workspaces (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    clerk_org_id  VARCHAR(255) UNIQUE,
    owner_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    name          VARCHAR(255) NOT NULL,
    is_personal   BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_workspace_type CHECK (
        (is_personal = TRUE AND owner_user_id IS NOT NULL AND clerk_org_id IS NULL) OR
        (is_personal = FALSE AND clerk_org_id IS NOT NULL)
    )
);

CREATE TABLE workspace_members (
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role         VARCHAR(50) NOT NULL DEFAULT 'member',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, user_id)
);

-- ==========================================
-- 2. RESOURCES (Databases)
-- ==========================================
CREATE TABLE databases (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id             UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name                     VARCHAR(255) NOT NULL,
    connection_type          VARCHAR(50) NOT NULL DEFAULT 'tunnel',
    encrypted_connection_url BYTEA,
    status                   VARCHAR(50) NOT NULL DEFAULT 'pending',
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_databases_workspace ON databases(workspace_id);

-- ==========================================
-- 3. IAM (API Keys scoped to workspaces)
-- ==========================================
CREATE TABLE api_keys (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    database_id  UUID REFERENCES databases(id) ON DELETE SET NULL,
    name         VARCHAR(255) NOT NULL DEFAULT '',
    key_prefix   VARCHAR(255) NOT NULL UNIQUE,
    key_hash     VARCHAR(255) NOT NULL UNIQUE,
    expires_at   TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_api_keys_key_hash ON api_keys(key_hash);
CREATE INDEX idx_api_keys_workspace ON api_keys(workspace_id);
CREATE INDEX idx_api_keys_database ON api_keys(database_id);

-- +goose Down
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS databases;
DROP TABLE IF EXISTS workspace_members;
DROP TABLE IF EXISTS workspaces;
DROP TABLE IF EXISTS users;
