package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
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
	cache  movieSearchCache
}

type movieSearchPage struct {
	Year      int
	Query     string
	Error     string
	Results   []movieChoice
	Reference string
	Previous  string
	Next      string
	Page      int
	Notice    string
	Searched  bool
	Empty     bool
}

func (h *movieSearchHandler) search(w http.ResponseWriter, r *http.Request) {
	data := movieSearchPage{Query: strings.TrimSpace(r.URL.Query().Get("q"))}
	data.Year = time.Now().Year()
	if raw := r.URL.Query().Get("year"); raw != "" {
		data.Year, _ = strconv.Atoi(raw)
	}
	status := http.StatusOK
	page := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		page, _ = strconv.Atoi(raw)
	}
	if r.URL.Query().Has("q") {
		if data.Year < 1 || data.Year > 9999 || page < 1 || page > 500 || data.Query == "" || !utf8.ValidString(data.Query) || utf8.RuneCountInString(data.Query) > 200 {
			data.Error = "Enter a movie title between 1 and 200 characters, a year between 1 and 9999, and a page between 1 and 500."
			status = http.StatusBadRequest
			setRequestEvent(r, slog.LevelWarn, "movie search", "outcome", "invalid_query")
		} else {
			// Leave time to render within the server's ten-second write timeout.
			ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
			results, err := h.movies.SearchMovies(ctx, data.Query, tmdb.SearchOptions{Page: page})
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
				data.Searched = true
				data.Page = page
				data.Empty = len(results.Results) == 0
				for _, movie := range results.Results {
					year := "Year unknown"
					if movie.ReleaseDate != nil {
						if date, err := time.Parse("2006-01-02", *movie.ReleaseDate); err == nil {
							year = date.Format("2006")
						}
					}
					data.Results = append(data.Results, movieChoice{ID: movie.ID, Title: movie.Title, Year: year})
				}
				session, _ := cookieKey(r, adminSessionCookie)
				data.Reference = h.cache.put(session, data.Query, results.Results)
				link := func(p int) string {
					return "/admin/movies/search?" + url.Values{"q": {data.Query}, "page": {strconv.Itoa(p)}, "year": {strconv.Itoa(data.Year)}}.Encode()
				}
				if page > 1 {
					data.Previous = link(page - 1)
				}
				if page < results.TotalPages && page < 500 {
					data.Next = link(page + 1)
				}

				setRequestEvent(r, slog.LevelInfo, "movie search", "outcome", "success", "search_term", data.Query)
			}
		}
	}
	h.admin.render(w, r, "admin_movie_search.html", status, data)
}

type movieChoice struct {
	ID          int64
	Title, Year string
}
