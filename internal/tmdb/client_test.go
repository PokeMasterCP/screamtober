package tmdb

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := New("test-secret")
	client.baseURL = server.URL + "/3"
	return client
}

func TestGetMovie(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/3/movie/11" || r.URL.Query().Get("api_key") != "test-secret" || r.URL.Query().Get("language") != "en-US" || r.Header.Get("Accept") != "application/json" {
			t.Error("unexpected TMDB request")
		}
		fmt.Fprint(w, `{"id":11,"title":"Star Wars","overview":"A space adventure.","release_date":"1977-05-25","runtime":121,"poster_path":"/poster.jpg","backdrop_path":null,"genres":[{"id":12,"name":"Adventure"}],"extra_field":true}`)
	})
	movie, err := client.GetMovie(context.Background(), 11)
	if err != nil {
		t.Fatal(err)
	}
	if movie.ID != 11 || movie.Title != "Star Wars" || movie.Overview == nil || *movie.Overview != "A space adventure." || movie.ReleaseDate == nil || *movie.ReleaseDate != "1977-05-25" || movie.Runtime == nil || *movie.Runtime != 121 || movie.PosterPath == nil || *movie.PosterPath != "/poster.jpg" || movie.BackdropPath != nil || len(movie.Genres) != 1 || movie.Genres[0].Name != "Adventure" {
		t.Fatalf("unexpected movie: %+v", movie)
	}
}

func TestGetMovieResponses(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"optional fields absent", 200, `{"id":11,"title":"Movie"}`, nil},
		{"optional fields null", 200, `{"id":11,"title":"Movie","runtime":null,"overview":null,"release_date":""}`, nil},
		{"not found", 404, "test-secret", ErrNotFound},
		{"invalid key", 401, "test-secret", ErrUnauthorized},
		{"forbidden", 403, "test-secret", ErrUnauthorized},
		{"rate limited", 429, "test-secret", ErrRateLimited},
		{"upstream failure", 500, "test-secret", ErrUnavailable},
		{"malformed JSON", 200, "test-secret", ErrInvalidResponse},
		{"null", 200, "null", ErrInvalidResponse},
		{"missing title", 200, `{"id":11}`, ErrInvalidResponse},
		{"wrong ID", 200, `{"id":12,"title":"Movie"}`, ErrInvalidResponse},
		{"trailing JSON", 200, `{"id":11,"title":"Movie"}{}`, ErrInvalidResponse},
		{"oversized", 200, strings.Repeat(" ", (1<<20)+1), ErrInvalidResponse},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			})
			_, err := client.GetMovie(context.Background(), 11)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
			if err != nil && strings.Contains(err.Error(), "test-secret") {
				t.Fatal("error exposed credential or response body")
			}
		})
	}
}

func TestGetMovieValidation(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected external request") })
	for _, id := range []int64{-1, 0, 1 << 31} {
		if _, err := client.GetMovie(context.Background(), id); !errors.Is(err, ErrInvalidID) {
			t.Errorf("ID %d: %v", id, err)
		}
	}
	client.apiKey = ""
	if _, err := client.GetMovie(context.Background(), 11); !errors.Is(err, ErrNotConfigured) {
		t.Fatal(err)
	}
}

func TestGetMovieCancellationAndTimeout(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.GetMovie(ctx, 11); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request: %v", err)
	}
	client.httpClient.Timeout = 20 * time.Millisecond
	if _, err := client.GetMovie(context.Background(), 11); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("timed out request: %v", err)
	}
}

func TestGetMovieDoesNotFollowRedirect(t *testing.T) {
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("redirect was followed") }))
	defer destination.Close()
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusFound)
	})
	if _, err := client.GetMovie(context.Background(), 11); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}
