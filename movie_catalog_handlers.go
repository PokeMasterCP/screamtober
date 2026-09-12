package main

import (
	"database/sql"
	"log/slog"
	"net/http"
	"strconv"

	"errors"
	"fmt"
	"time"
)

func (h *movieSearchHandler) add(w http.ResponseWriter, r *http.Request) {
	data := movieSearchPage{Year: time.Now().Year()}
	fail := func(status int, message, outcome string) {
		data.Error = message
		setRequestEvent(r, slog.LevelWarn, "add movie", "outcome", outcome)
		h.render(w, r, status, data)
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		fail(400, "The selection could not be read. Please search again.", "invalid_selection")
		return
	}
	year, err := strconv.Atoi(r.PostForm.Get("year"))
	if err != nil || year < 1 || year > 9999 {
		fail(400, "Choose a year between 1 and 9999.", "invalid_year")
		return
	}
	data.Year = year
	service := r.PostForm.Get("viewing_service")
	if len(r.PostForm["viewing_service"]) > 1 || !validViewingService(service) {
		fail(400, "Choose a supported viewing service. Please search again.", "invalid_viewing_service")
		return
	}
	session, _ := cookieKey(r, adminSessionCookie)
	entry, ok := h.cache.get(r.PostForm.Get("search_reference"), session)
	if !ok {
		fail(400, "These search results have expired. Please search again.", "expired_search")
		return
	}
	data.Query = entry.query
	id, err := strconv.ParseInt(r.PostForm.Get("movie_id"), 10, 64)
	if err != nil {
		fail(400, "Select a movie from the search results. Please search again.", "invalid_selection")
		return
	}
	for _, movie := range entry.results.Results {
		if movie.ID != id {
			continue
		}
		duplicate, err := addYearlyMovie(r.Context(), h.admin.db, movie, year, r.PostForm.Get("search_reference"), service)
		if errors.Is(err, errYearFull) {
			fail(409, "That year already has 31 movies. Choose another year or remove a pick before adding more.", "year_full")
			return
		}
		if err != nil {
			fail(500, "The movie could not be saved. Please try again.", "database_failure")
			return
		}
		outcome := "success"
		data.Notice = fmt.Sprintf("%s added to %d as an unscheduled pick and saved in your catalog.", movie.Title, year)
		if duplicate {
			outcome = "already_exists"
			data.Notice = fmt.Sprintf("This selection of %s is already in %d.", movie.Title, year)
		}

		setRequestEvent(r, slog.LevelInfo, "add movie", "outcome", outcome, "tmdb_id", id, "year", year)
		h.render(w, r, http.StatusOK, data)
		return
	}
	fail(400, "Select a movie from the search results. Please search again.", "invalid_selection")
}

func (h *movieSearchHandler) updateService(w http.ResponseWriter, r *http.Request) {
	year, entryID, ok := adminChallengeMovieIDs(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		setRequestEvent(r, slog.LevelWarn, "update viewing service", "outcome", "invalid_form")
		http.Error(w, "The viewing service could not be saved. Please try again.", http.StatusBadRequest)
		return
	}
	values := r.PostForm["viewing_service"]
	if len(values) != 1 || !validViewingService(values[0]) {
		setRequestEvent(r, slog.LevelWarn, "update viewing service", "outcome", "invalid_viewing_service", "year", year, "entry_id", entryID)
		http.Error(w, "Choose a supported viewing service. Please try again.", http.StatusBadRequest)
		return
	}
	if err := setYearlyMovieService(r.Context(), h.admin.db, entryID, year, values[0]); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		setRequestEvent(r, slog.LevelError, "update viewing service failed", "error", err, "year", year, "entry_id", entryID)
		http.Error(w, "The viewing service could not be saved. Please try again later.", http.StatusInternalServerError)
		return
	}
	setRequestEvent(r, slog.LevelInfo, "update viewing service", "outcome", "success", "year", year, "entry_id", entryID)
	http.Redirect(w, r, fmt.Sprintf("/admin/movies/search?year=%d&updated=1", year), http.StatusSeeOther)
}

func adminChallengeMovieIDs(w http.ResponseWriter, r *http.Request) (int, int64, bool) {
	yearValue, entryValue := r.PathValue("year"), r.PathValue("id")
	year, yearErr := strconv.Atoi(yearValue)
	entryID, entryErr := strconv.ParseInt(entryValue, 10, 64)
	if yearErr != nil || year < 1 || year > 9999 || strconv.Itoa(year) != yearValue || entryErr != nil || entryID <= 0 || strconv.FormatInt(entryID, 10) != entryValue {
		http.NotFound(w, r)
		return 0, 0, false
	}
	return year, entryID, true
}

func (h *movieSearchHandler) delete(w http.ResponseWriter, r *http.Request) {
	year, entryID, ok := adminChallengeMovieIDs(w, r)
	if !ok {
		return
	}
	if err := deleteYearlyMovie(r.Context(), h.admin.db, entryID, year); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		setRequestEvent(r, slog.LevelError, "delete movie failed", "error", err, "year", year, "entry_id", entryID)
		http.Error(w, "The movie could not be deleted. Please try again later.", http.StatusInternalServerError)
		return
	}
	setRequestEvent(r, slog.LevelInfo, "delete movie", "outcome", "success", "year", year, "entry_id", entryID)
	http.Redirect(w, r, fmt.Sprintf("/admin/movies/search?year=%d&deleted=1", year), http.StatusSeeOther)
}

func nullableMovieText(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *value, Valid: true}
}
