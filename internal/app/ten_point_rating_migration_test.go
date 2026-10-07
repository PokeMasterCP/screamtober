package app

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/pokemastercp/screamtober/migrations"
	"github.com/pressly/goose/v3"
)

func TestTenPointRatingUpgrade(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "screamtober.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Close() }()
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, migrations.Files, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	// Build the previously deployed 1–5 schema with history across two years.
	if _, err := provider.UpTo(ctx, 3); err != nil {
		t.Fatal(err)
	}
	execSchema(t, db, `
 INSERT INTO users (id, display_name, role) VALUES (1, 'Owner', 'owner'), (2, 'Member', 'member');
 INSERT INTO movies (id, tmdb_id, title) VALUES (1, 123, 'Existing movie');
 INSERT INTO challenges (id, year) VALUES (1, 2025), (2, 2026);
 INSERT INTO challenge_movies (id, challenge_id, movie_id, position, watched_at) VALUES
 (1, 1, 1, 1, '2025-10-01'), (2, 1, 1, 2, '2025-10-02'), (3, 2, 1, 1, '2026-10-01');
 INSERT INTO ratings (id, user_id, challenge_movie_id, score, updated_at) VALUES
 (10, 1, 1, 1, '2025-10-01 20:00:00'), (11, 2, 1, 5, '2025-10-01 21:00:00'),
 (12, 1, 2, 3, '2025-10-02 20:00:00'), (13, 2, 3, 4, '2026-10-01 20:00:00');`)
	ratings := func() string {
		t.Helper()
		rows, err := db.Query(`SELECT id, user_id, challenge_movie_id, score, updated_at FROM ratings ORDER BY id`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var result string
		for rows.Next() {
			var id, user, entry, score int
			var updated string
			if err := rows.Scan(&id, &user, &entry, &score, &updated); err != nil {
				t.Fatal(err)
			}
			result += fmt.Sprintf("%d:%d/%d=%d@%s ", id, user, entry, score, updated)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return result
	}
	const original = "10:1/1=1@2025-10-01 20:00:00 11:2/1=5@2025-10-01 21:00:00 12:1/2=3@2025-10-02 20:00:00 13:2/3=4@2026-10-01 20:00:00 "
	const doubled = "10:1/1=2@2025-10-01 20:00:00 11:2/1=10@2025-10-01 21:00:00 12:1/2=6@2025-10-02 20:00:00 13:2/3=8@2026-10-01 20:00:00 "
	if got := ratings(); got != original {
		t.Fatalf("fixture = %s", got)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	// The startup upgrade doubles scores once, keeping IDs and timestamps.
	for range 2 {
		db, _, err = openDatabase(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		if got := ratings(); got != doubled {
			t.Fatalf("upgraded = %s", got)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
	db, _, err = openDatabase(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	// The rebuilt table keeps its constraints, index, and entry cascade.
	execSchema(t, db, `INSERT INTO ratings (user_id, challenge_movie_id, score) VALUES (2, 2, 7)`)
	for _, query := range []string{
		`UPDATE ratings SET score = 11 WHERE id = 10`,
		`UPDATE ratings SET score = 0 WHERE id = 10`,
		`INSERT INTO ratings (user_id, challenge_movie_id, score) VALUES (1, 1, 4)`,
		`INSERT INTO ratings (user_id, challenge_movie_id, score) VALUES (99, 1, 4)`,
	} {
		if _, err := db.Exec(query); err == nil {
			t.Fatalf("expected constraint violation for %s", query)
		}
	}
	var indexed int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type = 'index' AND name = 'ratings_challenge_movie_id' AND tbl_name = 'ratings'`).Scan(&indexed); err != nil || indexed != 1 {
		t.Fatalf("rating index = %d, %v", indexed, err)
	}
	execSchema(t, db, `DELETE FROM challenge_movies WHERE id = 3`)
	var remaining int
	if err := db.QueryRow(`SELECT count(*) FROM ratings WHERE challenge_movie_id = 3`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("ratings for deleted entry = %d, %v", remaining, err)
	}
	// Rolling back restores migrated scores exactly; new odd scores round up.
	provider, err = goose.NewProvider(goose.DialectSQLite3, db, migrations.Files, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.DownTo(ctx, 3); err != nil {
		t.Fatal(err)
	}
	var restored, rounded int
	if err := db.QueryRow(`SELECT
	  (SELECT count(*) FROM ratings WHERE (id, score) IN (VALUES (10, 1), (11, 5), (12, 3))),
	  (SELECT score FROM ratings WHERE user_id = 2 AND challenge_movie_id = 2)`).Scan(&restored, &rounded); err != nil || restored != 3 || rounded != 4 {
		t.Fatalf("rollback restored %d, rounded 7 to %d, %v", restored, rounded, err)
	}
	if _, err := db.Exec(`UPDATE ratings SET score = 6 WHERE id = 10`); err == nil {
		t.Fatal("rollback did not restore the 1–5 constraint")
	}
}
