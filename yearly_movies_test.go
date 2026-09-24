package main

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/pokemastercp/screamtober/internal/store"
	"github.com/pokemastercp/screamtober/internal/tmdb"
)

func TestYearlyMovies(t *testing.T) {
	ctx := context.Background()
	db := schemaFixture(t)
	movie := tmdb.MovieSummary{ID: 456, Title: "New pick"}
	entries := make(map[int]int64)
	for _, tt := range []struct {
		year                  int
		ref                   string
		duplicate, catalogued bool
	}{{2028, "first", false, true}, {2028, "first", true, false}, {2028, "repeat", false, false}, {2029, "first", false, false}} {
		added, err := addYearlyMovie(ctx, db, movie, tt.year, tt.ref, "")
		if err != nil || added.duplicate != tt.duplicate || added.catalogued != tt.catalogued || added.entryID == 0 {
			t.Fatal(added, err)
		}
		if first, retry := entries[tt.year], added.entryID; tt.duplicate && retry != first {
			t.Fatalf("retry reported entry %d, want %d", retry, first)
		} else if !tt.duplicate && entries[tt.year] == 0 {
			entries[tt.year] = added.entryID
		}
	}
	q := store.New(db)
	challenge, err := q.GetChallengeByYear(ctx, 2028)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := q.ListChallengeMovies(ctx, challenge.ID)
	if err != nil || len(rows) != 2 || rows[0].Position.Valid || rows[1].MovieID != rows[0].MovieID {
		t.Fatal(rows, err)
	}
	for i := len(rows); i < 31; i++ {
		execSchema(t, db, `INSERT INTO challenge_movies (challenge_id,movie_id) VALUES (?,?)`, challenge.ID, rows[0].MovieID)
	}
	_, err = addYearlyMovie(ctx, db, tmdb.MovieSummary{ID: 999, Title: "Overflow"}, 2028, "full", "")
	if !errors.Is(err, errYearFull) {
		t.Fatal(err)
	}
	if _, err := q.GetMovieByTMDBID(ctx, 999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("partial catalog save", err)
	}
	if _, err := db.Exec(`INSERT INTO challenge_movies (challenge_id,movie_id) VALUES (?,?)`, challenge.ID, rows[0].MovieID); err == nil {
		t.Fatal("database accepted 32nd entry")
	}
	if _, err := db.Exec(`UPDATE challenge_movies SET challenge_id=? WHERE id=1`, challenge.ID); err == nil {
		t.Fatal("move bypassed yearly limit")
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM ratings`).Scan(&count); err != nil || count != 4 {
		t.Fatal("ratings changed", err, count)
	}
}
