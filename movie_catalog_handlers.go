package main

import (
	"database/sql"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/pokemastercp/screamtober/internal/store"
)

func (h *movieSearchHandler) add(w http.ResponseWriter, r *http.Request) {
	data := movieSearchPage{}
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
		rows, err := h.admin.queries.AddMovieToCatalog(r.Context(), store.AddMovieToCatalogParams{
			TmdbID: movie.ID, Title: movie.Title, ReleaseDate: nullableMovieText(movie.ReleaseDate), PosterPath: nullableMovieText(movie.PosterPath), Overview: nullableMovieText(movie.Overview),
		})
		if err != nil {
			fail(500, "The movie could not be saved. Please try again.", "database_failure")
			return
		}
		outcome := "success"
		data.Notice = movie.Title + " added to your catalog."
		if rows == 0 {
			outcome = "already_exists"
			data.Notice = movie.Title + " is already in your catalog."
		}
		setRequestEvent(r, slog.LevelInfo, "add movie", "outcome", outcome, "tmdb_id", id)
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
