-- +goose Up
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    display_name TEXT NOT NULL CHECK (length(trim(display_name)) > 0),
    role TEXT NOT NULL CHECK (role IN ('owner', 'member')),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    disabled_at TEXT
) STRICT;

CREATE TABLE user_tokens (
    user_id INTEGER PRIMARY KEY REFERENCES users (id),
    token_hash BLOB NOT NULL UNIQUE CHECK (length(token_hash) = 32),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
) STRICT;

CREATE UNIQUE INDEX users_one_owner ON users (role) WHERE role = 'owner';

-- +goose StatementBegin
CREATE TRIGGER users_member_limit_insert
BEFORE INSERT ON users
WHEN NEW.role = 'member' AND (SELECT count(*) FROM users WHERE role = 'member') >= 3
BEGIN
    SELECT RAISE(ABORT, 'at most three members are allowed');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER users_member_limit_update
BEFORE UPDATE OF role ON users
WHEN NEW.role = 'member' AND OLD.role != 'member'
    AND (SELECT count(*) FROM users WHERE role = 'member') >= 3
BEGIN
    SELECT RAISE(ABORT, 'at most three members are allowed');
END;
-- +goose StatementEnd

CREATE TABLE movies (
    id INTEGER PRIMARY KEY,
    tmdb_id INTEGER NOT NULL UNIQUE CHECK (tmdb_id > 0),
    title TEXT NOT NULL CHECK (length(trim(title)) > 0),
    release_date TEXT,
    poster_path TEXT,
    overview TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
) STRICT;

CREATE TABLE challenges (
    id INTEGER PRIMARY KEY,
    year INTEGER NOT NULL UNIQUE
) STRICT;

CREATE TABLE challenge_movies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    challenge_id INTEGER NOT NULL REFERENCES challenges (id),
    movie_id INTEGER NOT NULL REFERENCES movies (id),
    position INTEGER CHECK (position BETWEEN 1 AND 31),
    watched_at TEXT,
    submission_key TEXT UNIQUE,
    UNIQUE (challenge_id, position)
) STRICT;

CREATE INDEX challenge_movies_movie_id ON challenge_movies (movie_id);

-- +goose StatementBegin
CREATE TRIGGER challenge_movies_limit_insert
BEFORE INSERT ON challenge_movies
WHEN (SELECT count(*) FROM challenge_movies WHERE challenge_id = NEW.challenge_id) >= 31
BEGIN
    SELECT RAISE(ABORT, 'at most 31 entries are allowed');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER challenge_movies_limit_update
BEFORE UPDATE OF challenge_id ON challenge_movies
WHEN (SELECT count(*) FROM challenge_movies WHERE challenge_id = NEW.challenge_id) >= 31 AND NEW.challenge_id != OLD.challenge_id
BEGIN
    SELECT RAISE(ABORT, 'at most 31 entries are allowed');
END;
-- +goose StatementEnd

CREATE TABLE ratings (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users (id),
    challenge_movie_id INTEGER NOT NULL REFERENCES challenge_movies (id) ON DELETE CASCADE,
    score INTEGER NOT NULL CHECK (score BETWEEN 1 AND 5),
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (user_id, challenge_movie_id)
) STRICT;

CREATE INDEX ratings_challenge_movie_id ON ratings (challenge_movie_id);

-- +goose Down
-- Destructive: rollback removes all challenge, catalog, user, and rating data.
DROP TABLE ratings;
DROP TABLE challenge_movies;
DROP TABLE challenges;
DROP TABLE movies;
DROP TABLE user_tokens;
DROP TABLE users;
