-- +goose Up
-- Personal sessions survive restarts; only hashes of cookie values are stored.
-- Expiry columns are Unix seconds: expires_at slides with use up to max_expires_at.
CREATE TABLE user_sessions (
    session_hash BLOB PRIMARY KEY CHECK (length(session_hash) = 32),
    user_id INTEGER NOT NULL REFERENCES users (id),
    token_hash BLOB NOT NULL CHECK (length(token_hash) = 32),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at INTEGER NOT NULL,
    max_expires_at INTEGER NOT NULL,
    CHECK (expires_at <= max_expires_at)
) STRICT;

CREATE INDEX user_sessions_user_id ON user_sessions (user_id);
CREATE INDEX user_sessions_expires_at ON user_sessions (expires_at);

-- +goose Down
-- Signs out every personal session; profiles, tokens, and ratings remain.
DROP TABLE user_sessions;
