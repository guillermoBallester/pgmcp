-- name: CreateWorkspace :one
INSERT INTO workspaces (clerk_org_id, owner_user_id, name, is_personal)
VALUES (@clerk_org_id, @owner_user_id, @name, @is_personal)
RETURNING id, clerk_org_id, owner_user_id, name, is_personal, created_at;

-- name: GetWorkspaceByClerkOrgID :one
SELECT id, clerk_org_id, owner_user_id, name, is_personal, created_at
FROM workspaces
WHERE clerk_org_id = @clerk_org_id;

-- name: GetPersonalWorkspace :one
SELECT id, clerk_org_id, owner_user_id, name, is_personal, created_at
FROM workspaces
WHERE owner_user_id = @owner_user_id AND is_personal = TRUE;

-- name: GetWorkspaceByID :one
SELECT id, clerk_org_id, owner_user_id, name, is_personal, created_at
FROM workspaces
WHERE id = @id;

-- name: AddWorkspaceMember :exec
INSERT INTO workspace_members (workspace_id, user_id, role)
VALUES (@workspace_id, @user_id, @role)
ON CONFLICT (workspace_id, user_id) DO UPDATE SET role = EXCLUDED.role;

-- name: GetWorkspaceMember :one
SELECT workspace_id, user_id, role, created_at
FROM workspace_members
WHERE workspace_id = @workspace_id AND user_id = @user_id;

-- name: ListWorkspacesForUser :many
SELECT w.id, w.clerk_org_id, w.owner_user_id, w.name, w.is_personal, w.created_at, wm.role
FROM workspaces w
JOIN workspace_members wm ON wm.workspace_id = w.id
WHERE wm.user_id = @user_id
ORDER BY w.created_at;
