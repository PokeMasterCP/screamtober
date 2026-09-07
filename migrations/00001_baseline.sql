-- +goose Up
-- Establish migration tracking without deciding the application schema yet.
SELECT 1;

-- +goose Down
SELECT 1;
