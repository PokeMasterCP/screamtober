-- Never store or return the raw session cookie value.
-- name: CreateUserSession :exec
INSERT INTO user_sessions (session_hash, user_id, token_hash, expires_at, max_expires_at)
VALUES (?, ?, ?, ?, ?);

-- A session is valid only while its issuing token is current and access is enabled.
-- name: GetUserSession :one
SELECT sqlc.embed(u), s.expires_at, s.max_expires_at
FROM user_sessions AS s
JOIN user_tokens AS t ON t.user_id = s.user_id AND t.token_hash = s.token_hash
JOIN users AS u ON u.id = s.user_id
WHERE s.session_hash = sqlc.arg(session_hash) AND s.expires_at > sqlc.arg(now) AND u.disabled_at IS NULL;

-- name: RenewUserSession :exec
UPDATE user_sessions SET expires_at = ? WHERE session_hash = ?;

-- name: DeleteUserSession :exec
DELETE FROM user_sessions WHERE session_hash = ?;

-- name: DeleteUserSessions :exec
DELETE FROM user_sessions WHERE user_id = ?;

-- name: DeleteExpiredUserSessions :exec
DELETE FROM user_sessions WHERE expires_at <= sqlc.arg(now);

-- Keeps each person's most recently used sessions.
-- name: TrimUserSessions :exec
DELETE FROM user_sessions
WHERE user_sessions.user_id = sqlc.arg(user_id) AND session_hash NOT IN (
    SELECT recent.session_hash FROM user_sessions AS recent
    WHERE recent.user_id = sqlc.arg(user_id)
    ORDER BY recent.expires_at DESC, recent.rowid DESC
    LIMIT sqlc.arg(keep)
);
