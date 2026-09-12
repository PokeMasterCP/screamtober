package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/pokemastercp/screamtober/internal/store"
)

func (h *challengeHandler) rate(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(authenticatedUserKey{}).(store.User)
	year, id, ok := challengeMovieIDs(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Unable to read rating. Return to the movie and choose 1–5 stars.", http.StatusBadRequest)
		return
	}
	values := r.PostForm["score"]
	if len(values) != 1 || len(values[0]) != 1 || values[0][0] < '1' || values[0][0] > '5' {
		setRequestEvent(r, slog.LevelWarn, "save rating", "outcome", "invalid_score")
		http.Error(w, "Choose a whole-star rating from 1 to 5. Return to the movie to try again.", http.StatusBadRequest)
		return
	}
	fail := func(err error) {
		setRequestEvent(r, slog.LevelError, "save rating", "outcome", "database_failure", "error", err)
		http.Error(w, "Unable to save rating. Please try again later.", http.StatusInternalServerError)
	}
	challenge, err := h.queries.GetChallengeByYear(r.Context(), year)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		fail(err)
		return
	}
	err = withTransaction(r.Context(), h.db, func(q *store.Queries) error {
		if _, err := q.UpsertRating(r.Context(), store.UpsertRatingParams{
			UserID: user.ID, Score: int64(values[0][0] - '0'), ChallengeMovieID: id, ChallengeID: challenge.ID,
		}); err != nil {
			return err
		}
		count, err := q.MarkChallengeMovieWatched(r.Context(), store.MarkChallengeMovieWatchedParams{ID: id, ChallengeID: challenge.ID})
		if err != nil {
			return err
		}
		if count != 1 {
			return sql.ErrNoRows
		}
		return nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		fail(err)
		return
	}
	setRequestEvent(r, slog.LevelInfo, "save rating", "outcome", "success", "user_id", user.ID, "challenge_movie_id", id, "year", year)
	http.Redirect(w, r, fmt.Sprintf("/challenges/%d#movie-%d", year, id), http.StatusSeeOther)
}
