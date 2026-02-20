-- name: CreateUser :one
INSERT INTO users (clerk_user_id, email)
VALUES (@clerk_user_id, @email)
RETURNING id, clerk_user_id, email, created_at;

-- name: GetUserByClerkID :one
SELECT id, clerk_user_id, email, created_at
FROM users
WHERE clerk_user_id = @clerk_user_id;

-- name: GetUserByID :one
SELECT id, clerk_user_id, email, created_at
FROM users
WHERE id = @id;
