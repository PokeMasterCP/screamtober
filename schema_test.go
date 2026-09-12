package main

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

func schemaFixture(t *testing.T) *sql.DB {
	t.Helper()
	db, err := openDatabase(context.Background(), filepath.Join(t.TempDir(), "schema.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	execSchema(t, db, `
INSERT INTO users (id, display_name, role) VALUES
    (1, 'Owner', 'owner'), (2, 'Member', 'member');
INSERT INTO movies (id, tmdb_id, title) VALUES (1, 123, 'Test movie');
INSERT INTO challenges (id, year) VALUES (1, 2026), (2, 2027);
INSERT INTO challenge_movies (id, challenge_id, movie_id, position) VALUES
    (1, 1, 1, 1), (2, 1, 1, 2), (3, 2, 1, 1);
INSERT INTO ratings (user_id, challenge_movie_id, score) VALUES
    (1, 1, 1), (2, 1, 5), (2, 2, 2), (2, 3, 3);`)
	return db
}

func execSchema(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestSchemaConstraints(t *testing.T) {
	db := schemaFixture(t)
	for _, tt := range []struct{ name, query string }{
		{"second owner", `INSERT INTO users (display_name, role) VALUES ('Other', 'owner')`},
		{"invalid role", `INSERT INTO users (display_name, role) VALUES ('Other', 'admin')`},
		{"duplicate year", `INSERT INTO challenges (year) VALUES (2026)`},
		{"duplicate metadata", `INSERT INTO movies (tmdb_id, title) VALUES (123, 'Duplicate')`},
		{"duplicate slot", `INSERT INTO challenge_movies (challenge_id, movie_id, position) VALUES (1, 1, 1)`},
		{"slot zero", `INSERT INTO challenge_movies (challenge_id, movie_id, position) VALUES (1, 1, 0)`},
		{"slot 32", `INSERT INTO challenge_movies (challenge_id, movie_id, position) VALUES (1, 1, 32)`},
		{"fractional slot", `INSERT INTO challenge_movies (challenge_id, movie_id, position) VALUES (1, 1, 2.5)`},
		{"rating zero", `UPDATE ratings SET score = 0 WHERE user_id = 2`},
		{"rating six", `UPDATE ratings SET score = 6 WHERE user_id = 2`},
		{"fractional stars", `UPDATE ratings SET score = 3.5 WHERE user_id = 2`},
		{"missing score", `UPDATE ratings SET score = NULL WHERE user_id = 2`},
		{"duplicate rating", `INSERT INTO ratings (user_id, challenge_movie_id, score) VALUES (2, 1, 4)`},
		{"unknown user", `INSERT INTO ratings (user_id, challenge_movie_id, score) VALUES (99, 1, 4)`},
		{"unknown entry", `INSERT INTO ratings (user_id, challenge_movie_id, score) VALUES (2, 99, 4)`},
		{"unknown movie", `INSERT INTO challenge_movies (challenge_id, movie_id, position) VALUES (1, 99, 3)`},
		{"unknown challenge", `INSERT INTO challenge_movies (challenge_id, movie_id, position) VALUES (99, 1, 3)`},
		{"delete referenced movie", `DELETE FROM movies WHERE id = 1`},
		{"delete referenced year", `DELETE FROM challenges WHERE id = 1`},
		{"delete rating author", `DELETE FROM users WHERE id = 2`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := db.ExecContext(context.Background(), tt.query); err == nil {
				t.Fatal("expected constraint violation")
			}
		})
	}
	for position := 3; position <= 31; position++ {
		execSchema(t, db, `INSERT INTO challenge_movies (challenge_id, movie_id, position) VALUES (1, 1, ?)`, position)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM challenge_movies WHERE challenge_id = 1`).Scan(&count); err != nil || count != 31 {
		t.Fatalf("watchlist size = %d, error = %v", count, err)
	}
}

func TestHouseholdLimit(t *testing.T) {
	db := schemaFixture(t)
	execSchema(t, db, `INSERT INTO users (display_name, role) VALUES ('Member 2', 'member'), ('Member 3', 'member')`)
	for _, query := range []string{
		`INSERT INTO users (display_name, role) VALUES ('Member 4', 'member')`,
		`UPDATE users SET role = 'member' WHERE id = 1`,
		`UPDATE users SET role = 'owner' WHERE id = 2`,
	} {
		if _, err := db.Exec(query); err == nil {
			t.Fatalf("expected household constraint for %s", query)
		}
	}
	execSchema(t, db, `UPDATE users SET role = 'member', display_name = 'Renamed' WHERE id = 2`)
}

func TestDeleteEntryCascadesOnlyItsRatings(t *testing.T) {
	db := schemaFixture(t)
	// A raw DELETE must enforce the same cleanup as the HTTP handler.
	execSchema(t, db, `DELETE FROM challenge_movies WHERE id = 1`)
	for entry, want := range map[int]int{1: 0, 2: 1, 3: 1} {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM ratings WHERE challenge_movie_id = ?`, entry).Scan(&count); err != nil || count != want {
			t.Fatalf("entry %d ratings = %d, want %d, error = %v", entry, count, want, err)
		}
	}
	for _, table := range []string{"movies", "challenges", "challenge_movies"} {
		var count int
		want := 2
		if table == "movies" {
			want = 1
		}
		if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != want {
			t.Fatalf("%s count = %d, want %d, error = %v", table, count, want, err)
		}
	}
}

func TestRatingEditsAndYearIsolation(t *testing.T) {
	db := schemaFixture(t)
	execSchema(t, db, `UPDATE ratings SET score = 4, updated_at = CURRENT_TIMESTAMP WHERE user_id = 2 AND challenge_movie_id = 1`)
	execSchema(t, db, `UPDATE challenge_movies SET watched_at = CURRENT_TIMESTAMP WHERE id = 1`)
	for _, tt := range []struct{ user, entry, score int }{{1, 1, 1}, {2, 1, 4}, {2, 2, 2}, {2, 3, 3}} {
		var score int
		if err := db.QueryRow(`SELECT score FROM ratings WHERE user_id = ? AND challenge_movie_id = ?`, tt.user, tt.entry).Scan(&score); err != nil || score != tt.score {
			t.Fatalf("user %d entry %d score = %d, error = %v", tt.user, tt.entry, score, err)
		}
	}
	var watched int
	if err := db.QueryRow(`SELECT count(*) FROM challenge_movies WHERE watched_at IS NOT NULL`).Scan(&watched); err != nil || watched != 1 {
		t.Fatalf("watched entries = %d, error = %v", watched, err)
	}
}

func TestSchemaRollbackAndRebuild(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "upgrade.db")
	db, err := openDatabase(ctx, path)
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
	if _, err := provider.DownTo(ctx, 0); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name IN ('users', 'movies', 'challenges', 'challenge_movies', 'ratings', 'user_tokens')`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("tables after rollback = %d, error = %v", count, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = openDatabase(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name IN ('users', 'movies', 'challenges', 'challenge_movies', 'ratings', 'user_tokens')`).Scan(&count); err != nil || count != 6 {
		t.Fatalf("tables after upgrade = %d, error = %v", count, err)
	}
}

func TestEntryIDsNotReusedAfterClearingLineup(t *testing.T) {
	db := schemaFixture(t)
	execSchema(t, db, "DELETE FROM challenge_movies")
	var id int64
	if err := db.QueryRow(`INSERT INTO challenge_movies (challenge_id, movie_id) VALUES (1, 1) RETURNING id`).Scan(&id); err != nil || id <= 3 {
		t.Fatalf("entry identity reused: id = %d, error = %v", id, err)
	}
}
