-- +goose Up
-- Ratings move from 1–5 to 1–10; existing scores double so each keeps its place
-- on the new scale. SQLite cannot alter a CHECK constraint, so the table is
-- rebuilt in this migration's transaction, keeping rating IDs and timestamps.
CREATE TABLE ratings_ten_point (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users (id),
    challenge_movie_id INTEGER NOT NULL REFERENCES challenge_movies (id) ON DELETE CASCADE,
    score INTEGER NOT NULL CHECK (score BETWEEN 1 AND 10),
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (user_id, challenge_movie_id)
) STRICT;

INSERT INTO ratings_ten_point (id, user_id, challenge_movie_id, score, updated_at)
SELECT id, user_id, challenge_movie_id, score * 2, updated_at FROM ratings;

DROP TABLE ratings;
ALTER TABLE ratings_ten_point RENAME TO ratings;
CREATE INDEX ratings_challenge_movie_id ON ratings (challenge_movie_id);

-- +goose Down
-- Lossy for odd scores: they round up to the next 1–5 score (7 becomes 4).
-- Even scores, including every migrated rating, return to their original value.
CREATE TABLE ratings_five_point (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users (id),
    challenge_movie_id INTEGER NOT NULL REFERENCES challenge_movies (id) ON DELETE CASCADE,
    score INTEGER NOT NULL CHECK (score BETWEEN 1 AND 5),
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (user_id, challenge_movie_id)
) STRICT;

INSERT INTO ratings_five_point (id, user_id, challenge_movie_id, score, updated_at)
SELECT id, user_id, challenge_movie_id, (score + 1) / 2, updated_at FROM ratings;

DROP TABLE ratings;
ALTER TABLE ratings_five_point RENAME TO ratings;
CREATE INDEX ratings_challenge_movie_id ON ratings (challenge_movie_id);
