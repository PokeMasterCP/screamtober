package tmdb

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestSearchMovies(t *testing.T) {
	for _, options := range []SearchOptions{{}, {Year: 1978, Page: 2}} {
		client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			page, year := "1", ""
			if options.Page == 2 {
				page, year = "2", "1978"
			}
			if r.Method != "GET" || r.URL.Path != "/3/search/movie" || q.Get("query") != "L'été & Halloween?" || q.Get("primary_release_year") != year || q.Get("page") != page || q.Get("include_adult") != "false" || q.Get("api_key") != "test-secret" || q.Get("language") != "en-US" {
				t.Error("unexpected search request")
			}
			fmt.Fprintf(w, `{"page":%s,"total_pages":2,"total_results":21,"results":[{"id":948,"title":"Halloween","release_date":"1978-10-24","poster_path":null},{"id":949,"title":"Another movie"}]}`, page)
		})
		result, err := client.SearchMovies(context.Background(), "  L'été & Halloween?  ", options)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Results) != 2 || result.Results[0].ID != 948 || result.Results[0].ReleaseDate == nil || *result.Results[0].ReleaseDate != "1978-10-24" || result.Results[1].ReleaseDate != nil || result.TotalPages != 2 || result.TotalResults != 21 {
			t.Fatalf("unexpected results: %+v", result)
		}
	}
}

func TestSearchMoviesResponses(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"empty", 200, `{"page":1,"results":[],"total_pages":0,"total_results":0}`, nil},
		{"null", 200, `null`, ErrInvalidResponse},
		{"missing results", 200, `{"page":1}`, ErrInvalidResponse},
		{"wrong page", 200, `{"page":2,"results":[]}`, ErrInvalidResponse},
		{"invalid movie", 200, `{"page":1,"results":[{"id":0,"title":"Movie"}]}`, ErrInvalidResponse},
		{"missing title", 200, `{"page":1,"results":[{"id":1}]}`, ErrInvalidResponse},
		{"malformed", 200, `test-secret`, ErrInvalidResponse},
		{"unauthorized", 401, `test-secret`, ErrUnauthorized},
		{"rate limited", 429, `test-secret`, ErrRateLimited},
		{"unavailable", 503, `test-secret`, ErrUnavailable},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tt.status); fmt.Fprint(w, tt.body) })
			result, err := client.SearchMovies(context.Background(), "Halloween", SearchOptions{})
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
			if err != nil && strings.Contains(err.Error(), "test-secret") {
				t.Fatal("secret exposed")
			}
			if err == nil && (result.Results == nil || len(result.Results) != 0) {
				t.Fatal("expected empty results array")
			}
		})
	}
}

func TestSearchMoviesValidation(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected request") })
	for _, tt := range []struct {
		title   string
		options SearchOptions
	}{
		{" \t", SearchOptions{}}, {"Movie", SearchOptions{Year: -1}}, {"Movie", SearchOptions{Year: 10000}}, {"Movie", SearchOptions{Page: -1}}, {"Movie", SearchOptions{Page: 501}},
	} {
		if _, err := client.SearchMovies(context.Background(), tt.title, tt.options); !errors.Is(err, ErrInvalidSearch) {
			t.Fatalf("error = %v", err)
		}
	}
	client.apiKey = ""
	if _, err := client.SearchMovies(context.Background(), "Movie", SearchOptions{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatal(err)
	}
}
