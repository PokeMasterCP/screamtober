package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/pokemastercp/screamtober/internal/store"
)

func (h *challengeHandler) rate(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(authenticatedUserKey{}).(store.User)
	year, err := strconv.ParseInt(r.PathValue("year"), 10, 64)
	if err != nil || strconv.FormatInt(year, 10) != r.PathValue("year") {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 || strconv.FormatInt(id, 10) != r.PathValue("id") {
		http.NotFound(w, r)
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
	_, err = h.queries.UpsertRating(r.Context(), store.UpsertRatingParams{
		UserID: user.ID, Score: int64(values[0][0] - '0'), ChallengeMovieID: id, ChallengeID: challenge.ID,
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
