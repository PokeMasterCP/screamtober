package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"

	"github.com/pokemastercp/screamtober/internal/store"
	"github.com/pokemastercp/screamtober/internal/tmdb"
)

var errYearFull = errors.New("year already has 31 movies")

// Save the catalog metadata and yearly pick together. A retry of the same
// selection is idempotent; a new search can intentionally add another appearance.
func addYearlyMovie(ctx context.Context, db *sql.DB, movie tmdb.MovieSummary, year int, reference string, service string) (bool, error) {
	if !validViewingService(service) {
		return false, errViewingService
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	q := store.New(tx)
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d", reference, year, movie.ID)))
	key := sql.NullString{String: fmt.Sprintf("%x", digest), Valid: true}
	if _, err := q.GetEntryBySubmission(ctx, key); err == nil {
		return true, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	challenge, err := q.EnsureChallenge(ctx, int64(year))
	if err != nil {
		return false, err
	}
	count, err := q.CountChallengeMovies(ctx, challenge.ID)
	if err != nil {
		return false, err
	}
	if count >= 31 {
		return false, errYearFull
	}
	_, err = q.AddMovieToCatalog(ctx, store.AddMovieToCatalogParams{TmdbID: movie.ID, Title: movie.Title, ReleaseDate: nullableMovieText(movie.ReleaseDate), PosterPath: nullableMovieText(movie.PosterPath), Overview: nullableMovieText(movie.Overview)})
	if err != nil {
		return false, err
	}
	cached, err := q.GetMovieByTMDBID(ctx, movie.ID)
	if err != nil {
		return false, err
	}
	_, err = q.AddUnscheduledMovie(ctx, store.AddUnscheduledMovieParams{ChallengeID: challenge.ID, MovieID: cached.ID, SubmissionKey: key, ViewingService: service})
	if err != nil {
		return false, err
	}
	return false, tx.Commit()
}

// Update only the viewing service for one challenge entry. The movie, schedule,
// watched state, and ratings are intentionally left untouched.
func setYearlyMovieService(ctx context.Context, db *sql.DB, entryID int64, year int, service string) error {
	if !validViewingService(service) {
		return errViewingService
	}
	updated, err := store.New(db).SetChallengeMovieViewingService(ctx, store.SetChallengeMovieViewingServiceParams{
		ViewingService: service, ID: entryID, Year: int64(year),
	})
	if err != nil {
		return err
	}
	if updated != 1 {
		return sql.ErrNoRows
	}
	return nil
}

// Deleting a pick intentionally removes only that year's entry and its ratings.
// The catalog movie remains available to other challenge years and repeats.
func deleteYearlyMovie(ctx context.Context, db *sql.DB, entryID int64, year int) error {
	deleted, err := store.New(db).DeleteChallengeMovie(ctx, store.DeleteChallengeMovieParams{ID: entryID, Year: int64(year)})
	if err != nil {
		return err
	}
	if deleted != 1 {
		return sql.ErrNoRows
	}
	return nil
}
