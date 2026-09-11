-- sqlc schema for Go migration 2 in database_migrations.go.
ALTER TABLE challenge_movies ADD COLUMN viewing_service TEXT NOT NULL DEFAULT '';
