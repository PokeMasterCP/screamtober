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

// addedMovie describes the challenge entry a selection produced.
type addedMovie struct {
	entryID int64
	// duplicate marks a retried submission that added nothing.
	duplicate bool
	// catalogued marks a movie stored in the catalog for the first time.
	catalogued bool
}

// Save the catalog metadata and yearly pick together. A retry of the same
// selection is idempotent; a new search can intentionally add another appearance.
func addYearlyMovie(ctx context.Context, db *sql.DB, movie tmdb.MovieSummary, year int, reference string, service string) (addedMovie, error) {
	if !validViewingService(service) {
		return addedMovie{}, errViewingService
	}
	var added addedMovie
	err := withTransaction(ctx, db, func(q *store.Queries) error {
		digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d", reference, year, movie.ID)))
		key := sql.NullString{String: fmt.Sprintf("%x", digest), Valid: true}
		if entry, err := q.GetEntryBySubmission(ctx, key); err == nil {
			added.entryID, added.duplicate = entry.ID, true
			return nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		challenge, err := q.EnsureChallenge(ctx, int64(year))
		if err != nil {
			return err
		}
		count, err := q.CountChallengeMovies(ctx, challenge.ID)
		if err != nil {
			return err
		}
		if count >= 31 {
			return errYearFull
		}
		inserted, err := q.AddMovieToCatalog(ctx, store.AddMovieToCatalogParams{TmdbID: movie.ID, Title: movie.Title, ReleaseDate: nullableMovieText(movie.ReleaseDate), PosterPath: nullableMovieText(movie.PosterPath), Overview: nullableMovieText(movie.Overview)})
		if err != nil {
			return err
		}
		added.catalogued = inserted == 1
		cached, err := q.GetMovieByTMDBID(ctx, movie.ID)
		if err != nil {
			return err
		}
		entry, err := q.AddUnscheduledMovie(ctx, store.AddUnscheduledMovieParams{ChallengeID: challenge.ID, MovieID: cached.ID, SubmissionKey: key, ViewingService: service})
		added.entryID = entry.ID
		return err
	})
	return added, err
}

// Update only the viewing service for one challenge entry. The movie, schedule,
// watched state, and ratings are intentionally left untouched.
func setYearlyMovieService(ctx context.Context, db *sql.DB, entryID int64, year int, service string) error {
	if !validViewingService(service) {
		return errViewingService
	}
	updated, err := newQueries(db).SetChallengeMovieViewingService(ctx, store.SetChallengeMovieViewingServiceParams{
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
	deleted, err := newQueries(db).DeleteChallengeMovie(ctx, store.DeleteChallengeMovieParams{ID: entryID, Year: int64(year)})
	if err != nil {
		return err
	}
	if deleted != 1 {
		return sql.ErrNoRows
	}
	return nil
}
