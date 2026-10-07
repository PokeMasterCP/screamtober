package app

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"

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
		http.Error(w, "Unable to read rating. Return to the movie and choose a score from 1 to 10.", http.StatusBadRequest)
		return
	}
	score, ok := parseScore(r.PostForm["score"])
	if !ok {
		eventRejected(r, "invalid_score")
		http.Error(w, "Choose a whole-number rating from 1 to 10. Return to the movie to try again.", http.StatusBadRequest)
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
			UserID: user.ID, Score: score, ChallengeMovieID: id, ChallengeID: challenge.ID,
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

// maxScore is the top of the whole-number rating scale, which starts at 1.
const maxScore = 10

// parseScore accepts exactly one score written in canonical form, such as "7".
func parseScore(values []string) (int64, bool) {
	if len(values) != 1 {
		return 0, false
	}
	score, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil || score < 1 || score > maxScore || strconv.FormatInt(score, 10) != values[0] {
		return 0, false
	}
	return score, true
}

// removeRating deletes the person's own rating. Removing an entry's last rating
// also clears its watched time, so an accidental rating can be fully undone.
func (h *challengeHandler) removeRating(w http.ResponseWriter, r *http.Request) {
	startEvent(r, "rating.delete")
	user := r.Context().Value(authenticatedUserKey{}).(store.User)
	year, id, ok := challengeMovieIDs(w, r)
	if !ok {
		return
	}
	fail := func(step string, err error) {
		eventFailed(r, step, err)
		http.Error(w, "Unable to remove rating. Please try again later.", http.StatusInternalServerError)
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
	var removed, unwatched bool
	err = withTransaction(r.Context(), h.db, func(q *store.Queries) error {
		deleted, err := q.DeleteRating(r.Context(), store.DeleteRatingParams{UserID: user.ID, ChallengeMovieID: id, ChallengeID: challenge.ID})
		if err != nil {
			return err
		}
		if deleted == 0 {
			// Repeated submissions are harmless once the rating is gone.
			exists, err := q.ChallengeMovieExists(r.Context(), store.ChallengeMovieExistsParams{ID: id, ChallengeID: challenge.ID})
			if err == nil && !exists {
				err = sql.ErrNoRows
			}
			return err
		}
		removed = true
		cleared, err := q.ClearUnratedChallengeMovieWatched(r.Context(), store.ClearUnratedChallengeMovieWatchedParams{ID: id, ChallengeID: challenge.ID})
		unwatched = cleared == 1
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		eventRejected(r, "not_found")
		http.NotFound(w, r)
		return
	}
	if err != nil {
		fail("remove rating", err)
		return
	}
	eventSucceeded(r, "removed", removed, "unwatched", unwatched)
	http.Redirect(w, r, fmt.Sprintf("/challenges/%d#movie-%d", year, id), http.StatusSeeOther)
}
