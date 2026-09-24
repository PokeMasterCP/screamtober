package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/pokemastercp/screamtober/internal/store"
)

func (h *challengeHandler) rate(w http.ResponseWriter, r *http.Request) {
	startEvent(r, "rating.save")
	user := r.Context().Value(authenticatedUserKey{}).(store.User)
	year, id, ok := challengeMovieIDs(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		eventRejected(r, "invalid_form")
		http.Error(w, "Unable to read rating. Return to the movie and choose 1–5 stars.", http.StatusBadRequest)
		return
	}
	values := r.PostForm["score"]
	if len(values) != 1 || len(values[0]) != 1 || values[0][0] < '1' || values[0][0] > '5' {
		eventRejected(r, "invalid_score")
		http.Error(w, "Choose a whole-star rating from 1 to 5. Return to the movie to try again.", http.StatusBadRequest)
		return
	}
	fail := func(step string, err error) {
		eventFailed(r, step, err)
		http.Error(w, "Unable to save rating. Please try again later.", http.StatusInternalServerError)
	}
	challenge, err := h.queries.GetChallengeByYear(r.Context(), year)
	if errors.Is(err, sql.ErrNoRows) {
		eventRejected(r, "not_found")
		http.NotFound(w, r)
		return
	}
	if err != nil {
		fail("get challenge", err)
		return
	}
	var firstWatch bool
	err = withTransaction(r.Context(), h.db, func(q *store.Queries) error {
		// Returns sql.ErrNoRows unless the entry belongs to this year.
		if _, err := q.UpsertRating(r.Context(), store.UpsertRatingParams{
			UserID: user.ID, Score: int64(values[0][0] - '0'), ChallengeMovieID: id, ChallengeID: challenge.ID,
		}); err != nil {
			return err
		}
		marked, err := q.MarkChallengeMovieWatched(r.Context(), store.MarkChallengeMovieWatchedParams{ID: id, ChallengeID: challenge.ID})
		firstWatch = marked == 1
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		eventRejected(r, "not_found")
		http.NotFound(w, r)
		return
	}
	if err != nil {
		fail("save rating", err)
		return
	}
	eventSucceeded(r, "first_watch", firstWatch)
	http.Redirect(w, r, fmt.Sprintf("/challenges/%d#movie-%d", year, id), http.StatusSeeOther)
}
