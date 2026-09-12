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

func TestSearchTransportResponses(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"optional fields absent", 200, `{"page":1,"results":[{"id":11,"title":"Movie"}]}`, nil},
		{"optional fields null", 200, `{"page":1,"results":[{"id":11,"title":"Movie","overview":null,"release_date":""}]}`, nil},
		{"not found", 404, "test-secret", ErrNotFound},
		{"invalid key", 401, "test-secret", ErrUnauthorized},
		{"forbidden", 403, "test-secret", ErrUnauthorized},
		{"rate limited", 429, "test-secret", ErrRateLimited},
		{"upstream failure", 500, "test-secret", ErrUnavailable},
		{"malformed JSON", 200, "test-secret", ErrInvalidResponse},
		{"null", 200, "null", ErrInvalidResponse},
		{"missing title", 200, `{"id":11}`, ErrInvalidResponse},

		{"trailing JSON", 200, `{"page":1,"results":[]}{}`, ErrInvalidResponse},
		{"oversized", 200, strings.Repeat(" ", (1<<20)+1), ErrInvalidResponse},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			})
			_, err := client.SearchMovies(context.Background(), "Movie", SearchOptions{})
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
			if err != nil && strings.Contains(err.Error(), "test-secret") {
				t.Fatal("error exposed credential or response body")
			}
		})
	}
}

func TestSearchTransportCancellationAndTimeout(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.SearchMovies(ctx, "Movie", SearchOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request: %v", err)
	}
	client.httpClient.Timeout = 20 * time.Millisecond
	if _, err := client.SearchMovies(context.Background(), "Movie", SearchOptions{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("timed out request: %v", err)
	}
}

func TestSearchTransportDoesNotFollowRedirect(t *testing.T) {
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("redirect was followed") }))
	defer destination.Close()
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusFound)
	})
	if _, err := client.SearchMovies(context.Background(), "Movie", SearchOptions{}); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}
