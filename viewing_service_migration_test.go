package main

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestViewingServiceUpgradePreservesExistingData(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "screamtober.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Close() }()
	migrations, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, migrations, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	// Build the actual previously deployed schema, without the new column.
	if _, err := provider.UpTo(ctx, 1); err != nil {
		t.Fatal(err)
	}
	execSchema(t, db, `
 INSERT INTO users (id, display_name, role) VALUES (1, 'Owner', 'owner'), (2, 'Member', 'member');
 INSERT INTO user_tokens (user_id, token_hash) VALUES (1, zeroblob(32));
 INSERT INTO movies (id, tmdb_id, title) VALUES (1, 123, 'Existing movie');
 INSERT INTO challenges (id, year) VALUES (1, 2025), (2, 2026);
 INSERT INTO challenge_movies (id, challenge_id, movie_id, position, watched_at, submission_key) VALUES
 (1, 1, 1, 2, '2025-10-02', 'old-pick'), (2, 2, 1, NULL, NULL, 'current-pick');
 INSERT INTO ratings (user_id, challenge_movie_id, score) VALUES (1, 1, 5), (2, 1, 3), (2, 2, 4);`)
	queries := []string{
		"SELECT * FROM users ORDER BY id", "SELECT * FROM user_tokens ORDER BY user_id",
		"SELECT * FROM movies ORDER BY id", "SELECT * FROM challenges ORDER BY id",
		"SELECT id, challenge_id, movie_id, position, watched_at, submission_key FROM challenge_movies ORDER BY id",
		"SELECT * FROM ratings ORDER BY id",
	}
	snapshot := func() [][][]any {
		t.Helper()
		var result [][][]any
		for _, query := range queries {
			rows, err := db.Query(query)
			if err != nil {
				t.Fatal(err)
			}
			columns, err := rows.Columns()
			if err != nil {
				t.Fatal(err)
			}
			var table [][]any
			for rows.Next() {
				values := make([]any, len(columns))
				pointers := make([]any, len(columns))
				for i := range values {
					pointers[i] = &values[i]
				}
				if err := rows.Scan(pointers...); err != nil {
					t.Fatal(err)
				}
				table = append(table, values)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows.Close()
			result = append(result, table)
		}
		return result
	}
	before := snapshot()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	// Exercise the same startup upgrade used by production, twice for idempotence.
	for range 2 {
		db, _, err = openDatabase(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, snapshot()) {
			t.Fatal("upgrade changed existing data")
		}
		var count int
		if err := db.QueryRow("SELECT count(*) FROM challenge_movies WHERE viewing_service = ''").Scan(&count); err != nil || count != 2 {
			t.Fatal("existing picks did not default to Not decided", count, err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
	db, _, err = openDatabase(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	execSchema(t, db, "UPDATE challenge_movies SET viewing_service='netflix' WHERE id=2")
	provider, err = goose.NewProvider(goose.DialectSQLite3, db, migrations, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.DownTo(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, snapshot()) {
		t.Fatal("rollback changed pre-existing data")
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, snapshot()) {
		t.Fatal("reapplying migration changed pre-existing data")
	}
}
