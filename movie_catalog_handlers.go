package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

func (h *movieSearchHandler) add(w http.ResponseWriter, r *http.Request) {
	startEvent(r, "movie.add")
	data := movieSearchPage{Year: time.Now().Year()}
	reject := func(status int, message, reason string) {
		data.Error = message
		eventRejected(r, reason)
		h.render(w, r, status, data)
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		reject(400, "The selection could not be read. Please search again.", "invalid_selection")
		return
	}
	year, validYear := parseYear(r.PostForm.Get("year"))
	if !validYear {
		reject(400, "Choose a year between 1 and 9999.", "invalid_year")
		return
	}
	data.Year = int(year)
	addEventAttrs(r, "year", year)
	service := r.PostForm.Get("viewing_service")
	if len(r.PostForm["viewing_service"]) > 1 || !validViewingService(service) {
		reject(400, "Choose a supported viewing service. Please search again.", "invalid_viewing_service")
		return
	}
	session, _ := cookieKey(r, adminSessionCookie)
	entry, ok := h.cache.get(r.PostForm.Get("search_reference"), session)
	if !ok {
		reject(400, "These search results have expired. Please search again.", "expired_search")
		return
	}
	data.Query = entry.query
	id, err := strconv.ParseInt(r.PostForm.Get("movie_id"), 10, 64)
	if err != nil {
		reject(400, "Select a movie from the search results. Please search again.", "invalid_selection")
		return
	}
	addEventAttrs(r, "tmdb_id", id)
	for _, movie := range entry.results.Results {
		if movie.ID != id {
			continue
		}
		duplicate, err := addYearlyMovie(r.Context(), h.admin.db, movie, int(year), r.PostForm.Get("search_reference"), service)
		if errors.Is(err, errYearFull) {
			reject(409, "That year already has 31 movies. Choose another year or remove a pick before adding more.", "year_full")
			return
		}
		if err != nil {
			eventFailed(r, "add movie", err)
			data.Error = "The movie could not be saved. Please try again."
			h.render(w, r, http.StatusInternalServerError, data)
			return
		}
		data.Notice = fmt.Sprintf("%s added to %d as an unscheduled pick and saved in your catalog.", movie.Title, year)
		if duplicate {
			data.Notice = fmt.Sprintf("This selection of %s is already in %d.", movie.Title, year)
		}
		eventSucceeded(r, "duplicate", duplicate)
		h.render(w, r, http.StatusOK, data)
		return
	}
	reject(400, "Select a movie from the search results. Please search again.", "invalid_selection")
}

func (h *movieSearchHandler) updateService(w http.ResponseWriter, r *http.Request) {
	startEvent(r, "movie.update_service")
	year, entryID, ok := challengeMovieIDs(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		eventRejected(r, "invalid_form")
		http.Error(w, "The viewing service could not be saved. Please try again.", http.StatusBadRequest)
		return
	}
	values := r.PostForm["viewing_service"]
	if len(values) != 1 || !validViewingService(values[0]) {
		eventRejected(r, "invalid_viewing_service")
		http.Error(w, "Choose a supported viewing service. Please try again.", http.StatusBadRequest)
		return
	}
	if err := setYearlyMovieService(r.Context(), h.admin.db, entryID, int(year), values[0]); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			eventRejected(r, "not_found")
			http.NotFound(w, r)
			return
		}
		eventFailed(r, "update viewing service", err)
		http.Error(w, "The viewing service could not be saved. Please try again later.", http.StatusInternalServerError)
		return
	}
	eventSucceeded(r, "viewing_service", values[0])
	http.Redirect(w, r, fmt.Sprintf("/admin/movies/search?year=%d&updated=1", year), http.StatusSeeOther)
}

func (h *movieSearchHandler) delete(w http.ResponseWriter, r *http.Request) {
	startEvent(r, "movie.delete")
	year, entryID, ok := challengeMovieIDs(w, r)
	if !ok {
		return
	}
	if err := deleteYearlyMovie(r.Context(), h.admin.db, entryID, int(year)); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			eventRejected(r, "not_found")
			http.NotFound(w, r)
			return
		}
		eventFailed(r, "delete movie", err)
		http.Error(w, "The movie could not be deleted. Please try again later.", http.StatusInternalServerError)
		return
	}
	eventSucceeded(r)
	http.Redirect(w, r, fmt.Sprintf("/admin/movies/search?year=%d&deleted=1", year), http.StatusSeeOther)
}

func nullableMovieText(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *value, Valid: true}
}
