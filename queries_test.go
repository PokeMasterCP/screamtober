package main

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/pokemastercp/screamtober/internal/store"
)

func TestRatingQueries(t *testing.T) {
	db := schemaFixture(t)
	q := store.New(db)
	ctx := context.Background()
	execSchema(t, db, `UPDATE ratings SET updated_at = '2000-01-01 00:00:00' WHERE user_id = 2 AND challenge_movie_id = 1`)
	var beforeID int64
	if err := db.QueryRow(`SELECT id FROM ratings WHERE user_id=2 AND challenge_movie_id=1`).Scan(&beforeID); err != nil {
		t.Fatal(err)
	}
	params := store.UpsertRatingParams{UserID: 2, ChallengeID: 1, ChallengeMovieID: 1, Score: 4}
	after, err := q.UpsertRating(ctx, params)
	if err != nil || after.ID != beforeID || after.Score != 4 || after.UpdatedAt == "2000-01-01 00:00:00" {
		t.Fatalf("rating update = %+v, %v", after, err)
	}
	params.ChallengeID = 2
	if _, err := q.UpsertRating(ctx, params); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross-year rating: %v", err)
	}
	rows, err := q.ListChallengeRatings(ctx, 1)
	if err != nil || len(rows) != 3 {
		t.Fatalf("ratings = %+v, %v", rows, err)
	}
	for _, row := range rows {
		if row.ChallengeMovieID == 3 || row.DisplayName == "" {
			t.Fatalf("incorrect join: %+v", row)
		}
	}
}
