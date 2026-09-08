-- Never store or return the raw personal login token.
-- name: SetUserToken :exec
INSERT INTO user_tokens (user_id, token_hash)
VALUES (?, ?)
ON CONFLICT (user_id) DO UPDATE SET
    token_hash = excluded.token_hash,
    created_at = CURRENT_TIMESTAMP;

-- name: DeleteUserToken :exec
DELETE FROM user_tokens WHERE user_id = ?;

-- name: GetActiveUserByTokenHash :one
SELECT u.* FROM users AS u
JOIN user_tokens AS t ON t.user_id = u.id
WHERE t.token_hash = ? AND u.disabled_at IS NULL;

-- name: ListManagedUsers :many
SELECT u.*, t.user_id IS NOT NULL AS has_token
FROM users AS u
LEFT JOIN user_tokens AS t ON t.user_id = u.id
ORDER BY CASE u.role WHEN 'owner' THEN 0 ELSE 1 END, u.id;
