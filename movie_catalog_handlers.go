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
		h.admin.render(w, r, "admin_movie_search.html", status, data)
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
	for _, movie := range entry.movies {
		if movie.ID != id {
			continue
		}
		duplicate, err := addYearlyMovie(r.Context(), h.admin.db, movie, year, r.PostForm.Get("search_reference"))
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
		h.admin.render(w, r, "admin_movie_search.html", http.StatusOK, data)
		return
	}
	fail(400, "Select a movie from the search results. Please search again.", "invalid_selection")
}

func nullableMovieText(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *value, Valid: true}
}
