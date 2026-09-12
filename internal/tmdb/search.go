package tmdb

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

var ErrInvalidSearch = errors.New("TMDB search requires a title, a page from 1–500 (or 0 for the first)")

// SearchOptions selects a page; zero requests the first page.
type SearchOptions struct {
	Page int
}

// MovieSummary contains the metadata cached for challenge pages.
type MovieSummary struct {
	ID          int64   `json:"id"`
	Title       string  `json:"title"`
	ReleaseDate *string `json:"release_date"`
	Overview    *string `json:"overview"`
	PosterPath  *string `json:"poster_path"`
}

type SearchResults struct {
	Page         int            `json:"page"`
	Results      []MovieSummary `json:"results"`
	TotalPages   int            `json:"total_pages"`
	TotalResults int            `json:"total_results"`
}

// SearchMovies searches titles in English, excluding adult results. It fetches
// only the requested page; an empty results array is a successful search.
func (c *Client) SearchMovies(ctx context.Context, title string, options SearchOptions) (SearchResults, error) {
	title = strings.TrimSpace(title)
	if title == "" || options.Page < 0 || options.Page > 500 {
		return SearchResults{}, ErrInvalidSearch
	}
	page := options.Page
	if page == 0 {
		page = 1
	}
	query := url.Values{"query": {title}, "page": {strconv.Itoa(page)}, "include_adult": {"false"}}
	var result SearchResults
	if err := c.get(ctx, "/search/movie", query, &result); err != nil {
		return SearchResults{}, err
	}
	if result.Page != page || result.Results == nil || result.TotalPages < 0 || result.TotalResults < 0 {
		return SearchResults{}, ErrInvalidResponse
	}
	for _, movie := range result.Results {
		if movie.ID <= 0 || movie.ID > 1<<31-1 || strings.TrimSpace(movie.Title) == "" {
			return SearchResults{}, ErrInvalidResponse
		}
	}
	return result, nil
}
