-- name: AddChallengeMovie :one
INSERT INTO challenge_movies (challenge_id, movie_id, position)
VALUES (?, ?, ?)
RETURNING *;

-- name: ListChallengeMovies :many
SELECT cm.id, cm.challenge_id, cm.movie_id, cm.position, cm.watched_at,
       m.tmdb_id, m.title, m.release_date, m.poster_path, m.overview
FROM challenge_movies AS cm
JOIN movies AS m ON m.id = cm.movie_id
WHERE cm.challenge_id = ?
ORDER BY cm.position IS NULL, cm.position, cm.id;

-- Pass NULL to mark an entry unwatched. Both identifiers scope the update.
-- name: SetChallengeMovieWatchedAt :one
UPDATE challenge_movies
SET watched_at = sqlc.narg(watched_at)
WHERE id = sqlc.arg(id) AND challenge_id = sqlc.arg(challenge_id)
RETURNING *;

-- name: AddUnscheduledMovie :one
INSERT INTO challenge_movies (challenge_id, movie_id, submission_key)
VALUES (?, ?, ?)
RETURNING *;

-- name: GetEntryBySubmission :one
SELECT * FROM challenge_movies WHERE submission_key = ?;

-- name: CountChallengeMovies :one
SELECT count(*) FROM challenge_movies WHERE challenge_id = ?;

-- name: ClearChallengePositions :exec
UPDATE challenge_movies SET position = NULL WHERE challenge_id = ?;

-- name: SetChallengePosition :execrows
UPDATE challenge_movies SET position = sqlc.narg(position)
WHERE id = sqlc.arg(id) AND challenge_id = sqlc.arg(challenge_id);
