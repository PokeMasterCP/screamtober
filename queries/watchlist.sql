-- name: ListChallengeMovies :many
SELECT cm.id, cm.challenge_id, cm.movie_id, cm.position, cm.watched_at, cm.viewing_service,
       m.tmdb_id, m.title, m.release_date, m.poster_path, m.overview
FROM challenge_movies AS cm
JOIN movies AS m ON m.id = cm.movie_id
WHERE cm.challenge_id = ?
ORDER BY cm.position IS NULL, cm.position, cm.id;

-- Scope the update to one entry in the requested year.
-- name: SetChallengeMovieViewingService :execrows
UPDATE challenge_movies
SET viewing_service = sqlc.arg(viewing_service)
WHERE challenge_movies.id = sqlc.arg(id)
  AND challenge_movies.challenge_id = (SELECT challenges.id FROM challenges WHERE challenges.year = sqlc.arg(year));

-- Delete only the requested year's entry; its ratings cascade automatically.
-- name: DeleteChallengeMovie :execrows
DELETE FROM challenge_movies
WHERE challenge_movies.id = sqlc.arg(id)
  AND challenge_movies.challenge_id = (SELECT challenges.id FROM challenges WHERE challenges.year = sqlc.arg(year));

-- name: AddUnscheduledMovie :one
INSERT INTO challenge_movies (challenge_id, movie_id, submission_key, viewing_service)
VALUES (?, ?, ?, ?)
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

-- Preserve the first watched timestamp when another rating is submitted or edited.
-- name: MarkChallengeMovieWatched :execrows
UPDATE challenge_movies
SET watched_at = COALESCE(watched_at, CURRENT_TIMESTAMP)
WHERE id = sqlc.arg(id) AND challenge_id = sqlc.arg(challenge_id);
