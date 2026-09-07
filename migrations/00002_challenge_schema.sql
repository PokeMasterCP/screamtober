-- +goose Up
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    display_name TEXT NOT NULL CHECK (length(trim(display_name)) > 0),
    role TEXT NOT NULL CHECK (role IN ('owner', 'member')),
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
    id INTEGER PRIMARY KEY,
    challenge_id INTEGER NOT NULL REFERENCES challenges (id),
    movie_id INTEGER NOT NULL REFERENCES movies (id),
    position INTEGER NOT NULL CHECK (position BETWEEN 1 AND 31),
    watched_at TEXT,
    UNIQUE (challenge_id, position)
) STRICT;

CREATE INDEX challenge_movies_movie_id ON challenge_movies (movie_id);

CREATE TABLE ratings (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users (id),
    challenge_movie_id INTEGER NOT NULL REFERENCES challenge_movies (id),
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
DROP TABLE users;
