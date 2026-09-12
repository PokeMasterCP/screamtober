-- name: GetMovieByTMDBID :one
SELECT * FROM movies WHERE tmdb_id = ?;

-- name: AddMovieToCatalog :execrows
INSERT INTO movies (tmdb_id, title, release_date, poster_path, overview)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (tmdb_id) DO NOTHING;
