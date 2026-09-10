-- name: CreateChallenge :one
INSERT INTO challenges (year) VALUES (?) RETURNING *;

-- name: GetChallengeByYear :one
SELECT * FROM challenges WHERE year = ?;

-- name: ListChallenges :many
SELECT * FROM challenges ORDER BY year DESC;

-- name: EnsureChallenge :one
INSERT INTO challenges (year) VALUES (?)
ON CONFLICT (year) DO UPDATE SET year = excluded.year
RETURNING *;
