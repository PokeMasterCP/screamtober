-- The caller must derive user_id from the authenticated user, not form input.
-- A mismatched entry/challenge returns no rows and cannot update another year.
-- name: UpsertRating :one
INSERT INTO ratings (user_id, challenge_movie_id, score)
SELECT sqlc.arg(user_id), cm.id, sqlc.arg(score)
FROM challenge_movies AS cm
WHERE cm.id = sqlc.arg(challenge_movie_id) AND cm.challenge_id = sqlc.arg(challenge_id)
ON CONFLICT (user_id, challenge_movie_id) DO UPDATE SET
    score = excluded.score,
    updated_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: ListChallengeRatings :many
SELECT r.id, r.user_id, r.challenge_movie_id, r.score, r.updated_at,
       u.display_name
FROM ratings AS r
JOIN challenge_movies AS cm ON cm.id = r.challenge_movie_id
JOIN users AS u ON u.id = r.user_id
WHERE cm.challenge_id = ?
ORDER BY cm.position, r.user_id;
