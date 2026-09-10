package main

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/pokemastercp/screamtober/internal/store"
)

func TestWatchlistQueries(t *testing.T) {
	ctx := context.Background()
	db := schemaFixture(t)
	q := store.New(db)
	// Refreshing metadata must retain movie IDs used by past challenges.
	movie, err := q.UpsertMovie(ctx, store.UpsertMovieParams{TmdbID: 123, Title: "Updated title"})
	if err != nil || movie.ID != 1 || movie.ReleaseDate.Valid {
		t.Fatalf("refresh movie = %+v, error = %v", movie, err)
	}
	newMovie, err := q.UpsertMovie(ctx, store.UpsertMovieParams{TmdbID: 456, Title: "New movie", Overview: sql.NullString{String: "Overview", Valid: true}})
	if err != nil {
		t.Fatal(err)
	}
	cached, err := q.GetMovieByTMDBID(ctx, 456)
	if err != nil || cached.ID != newMovie.ID || cached.Overview.String != "Overview" {
		t.Fatalf("cached movie = %+v, error = %v", cached, err)
	}
	entry, err := q.AddChallengeMovie(ctx, store.AddChallengeMovieParams{ChallengeID: 1, MovieID: newMovie.ID, Position: sql.NullInt64{Int64: 3, Valid: true}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ challengeID, size int64 }{{1, 3}, {2, 1}, {99, 0}} {
		rows, err := q.ListChallengeMovies(ctx, tt.challengeID)
		if err != nil || int64(len(rows)) != tt.size {
			t.Fatalf("challenge %d rows = %+v, error = %v", tt.challengeID, rows, err)
		}
		for i, row := range rows {
			if row.ChallengeID != tt.challengeID || row.Position.Int64 != int64(i+1) {
				t.Fatalf("incorrect challenge or ordering: %+v", rows)
			}
			if row.MovieID == 1 && row.Title != "Updated title" {
				t.Fatalf("metadata join = %+v", row)
			}
		}
	}
	params := store.SetChallengeMovieWatchedAtParams{
		ID: entry.ID, ChallengeID: 2, WatchedAt: sql.NullString{String: "2026-10-03 20:00:00", Valid: true},
	}
	if _, err := q.SetChallengeMovieWatchedAt(ctx, params); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross-year update error = %v", err)
	}
	params.ChallengeID = 1
	marked, err := q.SetChallengeMovieWatchedAt(ctx, params)
	if err != nil || marked.WatchedAt != params.WatchedAt {
		t.Fatalf("mark watched = %+v, error = %v", marked, err)
	}
	params.WatchedAt = sql.NullString{}
	unmarked, err := q.SetChallengeMovieWatchedAt(ctx, params)
	if err != nil || unmarked.WatchedAt.Valid {
		t.Fatalf("mark unwatched = %+v, error = %v", unmarked, err)
	}
}

func TestRatingQueries(t *testing.T) {
	ctx := context.Background()
	db := schemaFixture(t)
	q := store.New(db)
	execSchema(t, db, `UPDATE ratings SET updated_at = '2000-01-01 00:00:00' WHERE user_id = 2 AND challenge_movie_id = 1`)
	before, err := q.GetUserRating(ctx, store.GetUserRatingParams{UserID: 2, ChallengeMovieID: 1})
	if err != nil {
		t.Fatal(err)
	}
	params := store.UpsertRatingParams{UserID: 2, ChallengeID: 1, ChallengeMovieID: 1, Score: 4}
	after, err := q.UpsertRating(ctx, params)
	if err != nil || after.ID != before.ID || after.Score != 4 || after.UpdatedAt == before.UpdatedAt {
		t.Fatalf("rating update = %+v, error = %v", after, err)
	}
	for _, score := range []int64{0, 6} {
		params.Score = score
		if _, err := q.UpsertRating(ctx, params); err == nil {
			t.Fatalf("accepted invalid score %d", score)
		}
	}
	params.Score, params.ChallengeID = 5, 2
	if _, err := q.UpsertRating(ctx, params); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross-year rating error = %v", err)
	}
	// A new rating by another household member must not replace existing votes.
	created, err := q.UpsertRating(ctx, store.UpsertRatingParams{UserID: 1, ChallengeID: 1, ChallengeMovieID: 2, Score: 5})
	if err != nil || created.Score != 5 {
		t.Fatalf("new rating = %+v, error = %v", created, err)
	}
	for _, tt := range []struct{ user, entry, score int64 }{{1, 1, 1}, {2, 1, 4}, {2, 2, 2}, {2, 3, 3}} {
		rating, err := q.GetUserRating(ctx, store.GetUserRatingParams{UserID: tt.user, ChallengeMovieID: tt.entry})
		if err != nil || rating.Score != tt.score {
			t.Fatalf("user %d entry %d rating = %+v, error = %v", tt.user, tt.entry, rating, err)
		}
	}
	rows, err := q.ListChallengeRatings(ctx, 1)
	if err != nil || len(rows) != 4 {
		t.Fatalf("challenge ratings = %+v, error = %v", rows, err)
	}
	for _, row := range rows {
		if row.ChallengeMovieID == 3 || row.DisplayName == "" {
			t.Fatalf("incorrect rating join: %+v", row)
		}
	}
}

func TestUserAndChallengeQueries(t *testing.T) {
	ctx := context.Background()
	q := store.New(schemaFixture(t))
	user, err := q.CreateUser(ctx, store.CreateUserParams{DisplayName: "Another member", Role: "member"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := q.GetUser(ctx, user.ID)
	if err != nil || got != user {
		t.Fatalf("user = %+v, error = %v", got, err)
	}
	users, err := q.ListUsers(ctx)
	if err != nil || len(users) != 3 {
		t.Fatalf("users = %+v, error = %v", users, err)
	}
	if _, err := q.CreateUser(ctx, store.CreateUserParams{DisplayName: "Other owner", Role: "owner"}); err == nil {
		t.Fatal("accepted a second owner")
	}
	challenge, err := q.CreateChallenge(ctx, 2028)
	if err != nil {
		t.Fatal(err)
	}
	gotChallenge, err := q.GetChallengeByYear(ctx, 2028)
	if err != nil || gotChallenge != challenge {
		t.Fatalf("challenge = %+v, error = %v", gotChallenge, err)
	}
	challenges, err := q.ListChallenges(ctx)
	if err != nil || len(challenges) != 3 || challenges[0].Year != 2028 || challenges[2].Year != 2026 {
		t.Fatalf("challenge history = %+v, error = %v", challenges, err)
	}
	if _, err := q.GetUser(ctx, 999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing user error = %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := q.ListChallenges(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled query error = %v", err)
	}
}

func TestQueriesTransactionRollback(t *testing.T) {
	ctx := context.Background()
	db := schemaFixture(t)
	q := store.New(db)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	transactionQueries := q.WithTx(tx)
	challenge, err := transactionQueries.CreateChallenge(ctx, 2028)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transactionQueries.AddChallengeMovie(ctx, store.AddChallengeMovieParams{ChallengeID: challenge.ID, MovieID: 999, Position: sql.NullInt64{Int64: 1, Valid: true}}); err == nil {
		t.Fatal("expected foreign key violation")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := q.GetChallengeByYear(ctx, 2028); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("rolled back challenge error = %v", err)
	}
}
