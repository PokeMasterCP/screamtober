-- name: UpsertMovie :one
INSERT INTO movies (tmdb_id, title, release_date, poster_path, overview)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (tmdb_id) DO UPDATE SET
    title = excluded.title,
    release_date = excluded.release_date,
    poster_path = excluded.poster_path,
    overview = excluded.overview
RETURNING *;

-- name: GetMovieByTMDBID :one
SELECT * FROM movies WHERE tmdb_id = ?;
