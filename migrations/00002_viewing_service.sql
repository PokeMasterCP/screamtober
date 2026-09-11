-- +goose Up
ALTER TABLE challenge_movies ADD COLUMN viewing_service TEXT NOT NULL DEFAULT '';

-- +goose Down
-- Removes viewing-service selections only; challenge entries and ratings remain.
ALTER TABLE challenge_movies DROP COLUMN viewing_service;
