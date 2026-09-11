package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
)

func TestViewingServiceUpgradePreservesExistingData(t *testing.T) {
	for _, preexisting := range []bool{false, true} {
		name := "production schema"
		if preexisting {
			name = "earlier PR schema"
		}
		t.Run(name, func(t *testing.T) { testViewingServiceUpgrade(t, preexisting) })
	}
}

func testViewingServiceUpgrade(t *testing.T, preexisting bool) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "screamtober.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Close() }()
	provider, err := newMigrationProvider(db)
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
	if preexisting {
		execSchema(t, db, "ALTER TABLE challenge_movies ADD COLUMN viewing_service TEXT NOT NULL DEFAULT ''")
		execSchema(t, db, "UPDATE challenge_movies SET viewing_service='shudder' WHERE id=2")
	}
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
		db, err = openDatabase(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, snapshot()) {
			t.Fatal("upgrade changed existing data")
		}
		var count int
		if err := db.QueryRow("SELECT count(*) FROM challenge_movies WHERE viewing_service = ''").Scan(&count); err != nil || count != map[bool]int{false: 2, true: 1}[preexisting] {
			t.Fatal("existing picks did not default to Not decided", count, err)
		}
		if preexisting {
			var service string
			if err := db.QueryRow("SELECT viewing_service FROM challenge_movies WHERE id=2").Scan(&service); err != nil || service != "shudder" {
				t.Fatal("existing choice lost", service, err)
			}
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
	db, err = openDatabase(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	execSchema(t, db, "UPDATE challenge_movies SET viewing_service='netflix' WHERE id=2")
	provider, err = newMigrationProvider(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Down(ctx); err != nil {
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

func TestViewingServiceMigrationRejectsIncompatibleColumn(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "invalid.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	provider, err := newMigrationProvider(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 1); err != nil {
		t.Fatal(err)
	}
	execSchema(t, db, "ALTER TABLE challenge_movies ADD COLUMN viewing_service INTEGER")
	if _, err := provider.Up(ctx); err == nil {
		t.Fatal("incompatible existing column accepted")
	}
	var version int
	if err := db.QueryRow("SELECT MAX(version_id) FROM goose_db_version WHERE is_applied=1").Scan(&version); err != nil || version != 1 {
		t.Fatal("failed migration was recorded as applied", version, err)
	}
}
