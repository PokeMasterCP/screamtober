-- User records only; authentication and provisioning belong to the application.
-- name: CreateUser :one
INSERT INTO users (display_name, role)
VALUES (?, ?)
RETURNING *;

-- name: GetUser :one
SELECT * FROM users WHERE id = ?;

-- name: ListUsers :many
SELECT * FROM users ORDER BY id;
