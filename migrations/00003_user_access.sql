-- +goose Up
ALTER TABLE users ADD COLUMN disabled_at TEXT;

CREATE TABLE user_tokens (
    user_id INTEGER PRIMARY KEY REFERENCES users (id),
    token_hash BLOB NOT NULL UNIQUE CHECK (length(token_hash) = 32),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
) STRICT;

-- +goose Down
-- Removes personal credentials and disabled status, preserving users and ratings.
DROP TABLE user_tokens;
ALTER TABLE users DROP COLUMN disabled_at;
