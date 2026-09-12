package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

// TMDB returns 20 results per page; showing half keeps the add button within reach.
const movieSearchPageSize = 10

// Two displayed pages cover one TMDB page, and TMDB serves at most 500 pages.
const movieSearchMaxPage = 1000

func (movieSearchPage) Services() []viewingService { return selectableViewingServices() }

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
	Current   []adminMovie
}

type adminMovie struct {
	ID             int64
	Title          string
	Position       sql.NullInt64
	WatchedAt      sql.NullString
	ViewingService string
	PosterURL      string
}

func (m adminMovie) Service() viewingService { return findViewingService(m.ViewingService) }

func (h *movieSearchHandler) search(w http.ResponseWriter, r *http.Request) {
	data := movieSearchPage{Query: strings.TrimSpace(r.URL.Query().Get("q"))}
	data.Year = time.Now().Year()
	if raw := r.URL.Query().Get("year"); raw != "" {
		data.Year, _ = strconv.Atoi(raw)
	}
	status := http.StatusOK
	if data.Year >= 1 && data.Year <= 9999 {
		switch {
		case r.URL.Query().Get("deleted") == "1":
			data.Notice = fmt.Sprintf("The movie was deleted from %d. Its ratings were removed; the catalog record remains available.", data.Year)
		case r.URL.Query().Get("updated") == "1":
			data.Notice = fmt.Sprintf("The viewing service was updated for the %d lineup.", data.Year)
		}
	}
	page := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		page, _ = strconv.Atoi(raw)
	}
	if r.URL.Query().Has("q") {
		if data.Year < 1 || data.Year > 9999 || page < 1 || page > movieSearchMaxPage || data.Query == "" || !utf8.ValidString(data.Query) || utf8.RuneCountInString(data.Query) > 200 {
			data.Error = fmt.Sprintf("Enter a movie title between 1 and 200 characters, a year between 1 and 9999, and a page between 1 and %d.", movieSearchMaxPage)
			status = http.StatusBadRequest
			setRequestEvent(r, slog.LevelWarn, "movie search", "outcome", "invalid_query")
		} else {
			upstreamPage := (page + 1) / 2
			offset := ((page - 1) % 2) * movieSearchPageSize
			session, _ := cookieKey(r, adminSessionCookie)
			entry, cached := h.cache.find(session, data.Query, upstreamPage)
			var err error
			if !cached {
				// Leave time to render within the server's ten-second write timeout.
				ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
				var results tmdb.SearchResults
				results, err = h.movies.SearchMovies(ctx, data.Query, tmdb.SearchOptions{Page: upstreamPage})
				cancel()
				entry = cachedMovieSearch{session: session, query: data.Query, results: results, expires: time.Now().Add(searchCacheTTL)}
			}
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
				results := entry.results
				data.Searched = true
				data.Page = page
				shown := results.Results[min(offset, len(results.Results)):min(offset+movieSearchPageSize, len(results.Results))]
				data.Empty = len(shown) == 0
				for _, movie := range shown {
					year := "Year unknown"
					if movie.ReleaseDate != nil {
						if date, err := time.Parse("2006-01-02", *movie.ReleaseDate); err == nil {
							year = date.Format("2006")
						}
					}
					data.Results = append(data.Results, movieChoice{ID: movie.ID, Title: movie.Title, Year: year})
				}
				// A fresh reference allows intentional repeat picks while keeping POST retries idempotent.
				data.Reference = h.cache.put(entry)
				link := func(p int) string {
					return "/admin/movies/search?" + url.Values{"q": {data.Query}, "page": {strconv.Itoa(p)}, "year": {strconv.Itoa(data.Year)}}.Encode()
				}
				if page > 1 {
					data.Previous = link(page - 1)
				}
				if page < movieSearchMaxPage && (offset+movieSearchPageSize < len(results.Results) || upstreamPage < results.TotalPages) {
					data.Next = link(page + 1)
				}

				setRequestEvent(r, slog.LevelInfo, "movie search", "outcome", "success", "search_term", data.Query)
			}
		}
	}
	h.render(w, r, status, data)
}

func (h *movieSearchHandler) loadCurrent(ctx context.Context, data *movieSearchPage) error {
	challenge, err := h.admin.queries.GetChallengeByYear(ctx, int64(data.Year))
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	rows, err := h.admin.queries.ListChallengeMovies(ctx, challenge.ID)
	if err != nil {
		return err
	}
	for _, movie := range rows {
		data.Current = append(data.Current, adminMovie{
			ID: movie.ID, Title: movie.Title, Position: movie.Position, WatchedAt: movie.WatchedAt,
			ViewingService: movie.ViewingService, PosterURL: moviePosterURL(movie.PosterPath),
		})
	}
	return nil
}

func (h *movieSearchHandler) render(w http.ResponseWriter, r *http.Request, status int, data movieSearchPage) {
	if data.Year >= 1 && data.Year <= 9999 {
		if err := h.loadCurrent(r.Context(), &data); err != nil {
			h.fail(w, r, err)
			return
		}
	}
	h.admin.render(w, r, "admin_movie_search.html", status, data)
}

func (h *movieSearchHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	setRequestEvent(r, slog.LevelError, "movie management failed", "error", err)
	http.Error(w, "Unable to load movie management. Please try again later.", http.StatusInternalServerError)
}

type movieChoice struct {
	ID          int64
	Title, Year string
}
