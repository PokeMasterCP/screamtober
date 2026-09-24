-- Administration must be authorized by the application before invoking writes.
-- name: CreateUser :one
INSERT INTO users (display_name, role)
VALUES (?, ?)
RETURNING *;

-- name: GetUser :one
SELECT * FROM users WHERE id = ?;

-- name: RenameUser :one
UPDATE users SET display_name = ? WHERE id = ? RETURNING *;

-- name: DisableUser :one
UPDATE users SET disabled_at = CURRENT_TIMESTAMP WHERE id = ? RETURNING *;

-- name: EnableUser :one
UPDATE users SET disabled_at = NULL WHERE id = ? RETURNING *;
