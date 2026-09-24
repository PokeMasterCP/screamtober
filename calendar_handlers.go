package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/pokemastercp/screamtober/internal/store"
)

type calendarMovie struct {
	store.ListChallengeMoviesRow
	PosterURL string
}
type calendarPage struct {
	Year            int64
	Movies          []calendarMovie
	Days            []int
	Revision, Error string
	Saved           bool
}

func calendarRevision(movies []store.ListChallengeMoviesRow) string {
	hash := sha256.New()
	for _, m := range movies {
		fmt.Fprintf(hash, "%d:%d:%t;", m.ID, m.Position.Int64, m.Position.Valid)
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func calendarYear(w http.ResponseWriter, r *http.Request) (int64, bool) {
	year, ok := int64(time.Now().Year()), true
	if raw := r.URL.Query().Get("year"); raw != "" {
		year, ok = parseYear(raw)
	}
	if !ok {
		eventRejected(r, "invalid_year")
		http.Error(w, "Choose a year between 1 and 9999.", http.StatusBadRequest)
		return 0, false
	}
	addEventAttrs(r, "year", year)
	return year, true
}

func (h *adminHandler) calendar(w http.ResponseWriter, r *http.Request) {
	year, ok := calendarYear(w, r)
	if !ok {
		return
	}
	h.showCalendar(w, r, year, http.StatusOK, "")
}

func (h *adminHandler) calendarFailure(w http.ResponseWriter, r *http.Request, step string, err error) {
	eventFailed(r, step, err)
	http.Error(w, "Unable to arrange movies. Please try again later.", http.StatusInternalServerError)
}

func (h *adminHandler) showCalendar(w http.ResponseWriter, r *http.Request, year int64, status int, message string) {
	data := calendarPage{Year: year, Error: message, Saved: r.URL.Query().Get("saved") == "1" && message == ""}
	for day := 1; day <= 31; day++ {
		data.Days = append(data.Days, day)
	}
	challenge, err := h.queries.GetChallengeByYear(r.Context(), year)
	var movies []store.ListChallengeMoviesRow
	if err == nil {
		movies, err = h.queries.ListChallengeMovies(r.Context(), challenge.ID)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		h.calendarFailure(w, r, "load arrangement", err)
		return
	}
	data.Revision = calendarRevision(movies)
	for _, movie := range movies {
		view := calendarMovie{ListChallengeMoviesRow: movie, PosterURL: moviePosterURL(movie.PosterPath)}
		data.Movies = append(data.Movies, view)
	}
	h.render(w, r, "admin_calendar.html", status, data)
}

var errCalendarConflict = errors.New("calendar changed")
var errCalendarInvalid = errors.New("invalid arrangement")

func (h *adminHandler) saveCalendar(w http.ResponseWriter, r *http.Request) {
	startEvent(r, "calendar.save")
	year, ok := calendarYear(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err := r.ParseForm(); err != nil {
		eventRejected(r, "invalid_form")
		h.showCalendar(w, r, year, 400, "The form could not be read. Please try again.")
		return
	}
	err := withTransaction(r.Context(), h.db, func(q *store.Queries) error {
		challenge, err := q.GetChallengeByYear(r.Context(), year)
		if err != nil {
			return err
		}
		return saveCalendarPositions(r.Context(), q, challenge.ID, r.PostForm)
	})
	switch {
	case errors.Is(err, errCalendarConflict):
		eventRejected(r, "stale_revision")
		h.showCalendar(w, r, year, 409, "This lineup changed in another tab. The latest saved arrangement is shown; please arrange it again.")
	case errors.Is(err, errCalendarInvalid):
		eventRejected(r, "invalid_arrangement")
		h.showCalendar(w, r, year, 400, "Choose a different day from 1–31 for each movie, or leave it unscheduled. No changes were saved; the saved arrangement is shown.")
	case errors.Is(err, sql.ErrNoRows):
		eventRejected(r, "no_challenge")
		h.showCalendar(w, r, year, 404, "Add movies to this year before arranging them.")
	case err != nil:
		h.calendarFailure(w, r, "save arrangement", err)
	default:
		eventSucceeded(r)
		http.Redirect(w, r, fmt.Sprintf("/admin/calendar?year=%d&saved=1", year), http.StatusSeeOther)
	}
}

func saveCalendarPositions(ctx context.Context, q *store.Queries, challengeID int64, form map[string][]string) error {
	movies, err := q.ListChallengeMovies(ctx, challengeID)
	if err != nil {
		return err
	}
	revision := form["revision"]
	if len(revision) != 1 || revision[0] != calendarRevision(movies) {
		return errCalendarConflict
	}
	if len(form) != len(movies)+1 {
		return errCalendarInvalid
	}
	positions := make(map[int64]sql.NullInt64)
	used := make(map[int64]bool)
	for _, movie := range movies {
		values := form[fmt.Sprintf("entry_%d", movie.ID)]
		if len(values) != 1 {
			return errCalendarInvalid
		}
		var position sql.NullInt64
		if values[0] != "" {
			day, err := strconv.ParseInt(values[0], 10, 64)
			if err != nil || day < 1 || day > 31 || used[day] {
				return errCalendarInvalid
			}
			position = sql.NullInt64{Int64: day, Valid: true}
			used[day] = true
		}
		positions[movie.ID] = position
	}
	// Clear first so swaps cannot collide with the unique day constraint.
	if err := q.ClearChallengePositions(ctx, challengeID); err != nil {
		return err
	}
	for id, position := range positions {
		count, err := q.SetChallengePosition(ctx, store.SetChallengePositionParams{ID: id, ChallengeID: challengeID, Position: position})
		if err != nil {
			return err
		}
		if count != 1 {
			return errCalendarConflict
		}
	}
	return nil
}
