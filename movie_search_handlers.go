package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pokemastercp/screamtober/internal/tmdb"
)

type movieSearcher interface {
	SearchMovies(context.Context, string, tmdb.SearchOptions) (tmdb.SearchResults, error)
}

type movieSearchHandler struct {
	admin  *adminHandler
	movies movieSearcher
}

type movieSearchPage struct {
	Query string
	Error string
	JSON  string
	Empty bool
}

func (h *movieSearchHandler) search(w http.ResponseWriter, r *http.Request) {
	data := movieSearchPage{Query: strings.TrimSpace(r.URL.Query().Get("q"))}
	status := http.StatusOK
	if r.URL.Query().Has("q") {
		if data.Query == "" || !utf8.ValidString(data.Query) || utf8.RuneCountInString(data.Query) > 200 {
			data.Error = "Enter a movie title between 1 and 200 characters."
			status = http.StatusBadRequest
			setRequestEvent(r, slog.LevelWarn, "movie search", "outcome", "invalid_query")
		} else {
			// Leave time to render within the server's ten-second write timeout.
			ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
			results, err := h.movies.SearchMovies(ctx, data.Query, tmdb.SearchOptions{})
			cancel()
			if err != nil {
				status = http.StatusBadGateway
				data.Error = "Movie search is unavailable right now. Please try again later."
				outcome := "upstream_failure"
				switch {
				case errors.Is(err, tmdb.ErrNotConfigured):
					status = http.StatusServiceUnavailable
					data.Error = "Movie search is not configured. Set TMDB_API_KEY on the server and restart the app."
					outcome = "not_configured"
				case errors.Is(err, tmdb.ErrRateLimited):
					status = http.StatusServiceUnavailable
					data.Error = "TMDB is busy. Please wait a moment before searching again."
					outcome = "rate_limited"
				}
				setRequestEvent(r, slog.LevelWarn, "movie search", "outcome", outcome, "search_term", data.Query)
			} else {
				body, err := json.MarshalIndent(results, "", "  ")
				if err != nil {
					h.admin.fail(w, r, "encode movie search", err)
					return
				}
				data.JSON = string(body)
				data.Empty = len(results.Results) == 0
				setRequestEvent(r, slog.LevelInfo, "movie search", "outcome", "success", "search_term", data.Query)
			}
		}
	}
	h.admin.render(w, r, "admin_movie_search.html", status, data)
}
