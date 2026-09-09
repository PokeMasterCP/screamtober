// Package tmdb retrieves movie metadata from TMDB without persisting it.
package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	ErrNotConfigured   = errors.New("TMDB_API_KEY is required for movie lookups")
	ErrInvalidID       = errors.New("TMDB movie ID must be a positive 32-bit integer")
	ErrNotFound        = errors.New("TMDB movie not found")
	ErrUnauthorized    = errors.New("TMDB rejected the API key")
	ErrRateLimited     = errors.New("TMDB rate limit reached; try again later")
	ErrUnavailable     = errors.New("TMDB request failed")
	ErrInvalidResponse = errors.New("TMDB returned invalid movie data")
)

// Movie contains the basic details returned by TMDB. Optional fields remain nil
// when absent or null; an unknown release date may also be an empty string.
type Movie struct {
	ID           int64   `json:"id"`
	Title        string  `json:"title"`
	Overview     *string `json:"overview"`
	ReleaseDate  *string `json:"release_date"`
	Runtime      *int    `json:"runtime"`
	PosterPath   *string `json:"poster_path"`
	BackdropPath *string `json:"backdrop_path"`
	Genres       []Genre `json:"genres"`
}

type Genre struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type Client struct {
	apiKey     string
	httpClient *http.Client
	baseURL    string
}

// New creates a client using a TMDB v3 API key (not a read access token).
// Configuration is checked on lookup so existing local data needs no API key.
func New(apiKey string) *Client {
	return &Client{
		apiKey:  strings.TrimSpace(apiKey),
		baseURL: "https://api.themoviedb.org/3",
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			// Do not forward the credential to a redirect destination.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// GetMovie fetches one movie in English. It does not retry automatically.
// Errors never include request URLs, credentials, or upstream response bodies.
func (c *Client) GetMovie(ctx context.Context, id int64) (Movie, error) {
	if id <= 0 || id > 1<<31-1 {
		return Movie{}, ErrInvalidID
	}
	if c.apiKey == "" {
		return Movie{}, ErrNotConfigured
	}
	query := url.Values{"api_key": {c.apiKey}, "language": {"en-US"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/movie/"+strconv.FormatInt(id, 10)+"?"+query.Encode(), nil)
	if err != nil {
		return Movie{}, ErrUnavailable
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Movie{}, ctx.Err()
		}
		// http.Client errors can contain the complete URL, including api_key.
		return Movie{}, ErrUnavailable
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return Movie{}, ErrNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		return Movie{}, ErrUnauthorized
	case http.StatusTooManyRequests:
		return Movie{}, ErrRateLimited
	default:
		return Movie{}, fmt.Errorf("%w (HTTP %d)", ErrUnavailable, resp.StatusCode)
	}
	const maxResponseBytes = 1 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return Movie{}, ErrUnavailable
	}
	var movie Movie
	if len(body) > maxResponseBytes || json.Unmarshal(body, &movie) != nil || movie.ID != id || strings.TrimSpace(movie.Title) == "" {
		return Movie{}, ErrInvalidResponse
	}
	return movie, nil
}
