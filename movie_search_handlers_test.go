package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pokemastercp/screamtober/internal/tmdb"
)

type searchStub struct {
	calls  int
	result tmdb.SearchResults
	err    error
}

func (s *searchStub) SearchMovies(ctx context.Context, query string, options tmdb.SearchOptions) (tmdb.SearchResults, error) {
	s.calls++
	if _, ok := ctx.Deadline(); !ok {
		panic("search must have deadline")
	}
	return s.result, s.err
}

func TestMovieSearchPage(t *testing.T) {
	db, err := openDatabase(context.Background(), filepath.Join(t.TempDir(), "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a, _ := portalFixture(t, db)
	stub := &searchStub{result: tmdb.SearchResults{Page: 1, Results: []tmdb.MovieSummary{{ID: 948, Title: "<script>alert(1)</script>"}}, TotalPages: 1, TotalResults: 1}}
	h, err := newHandlerWithMovieSearch(a, db, stub)
	if err != nil {
		t.Fatal(err)
	}
	cookie := adminCookie(t, h)
	for _, tt := range []struct {
		path     string
		status   int
		contains string
		calls    int
	}{
		{"/admin/movies/search", 200, `name="q"`, 0},
		{"/admin/movies/search?q=", 400, "Enter a movie title", 0},
		{"/admin/movies/search?q=" + url.QueryEscape(strings.Repeat("x", 201)), 400, "Enter a movie title", 0},
		{"/admin/movies/search?q=Halloween", 200, "948", 1},
	} {
		w := portalRequest(h, "GET", tt.path, nil, cookie)
		if w.Code != tt.status || !strings.Contains(w.Body.String(), tt.contains) || stub.calls != tt.calls {
			t.Fatalf("%s: status %d calls %d body %s", tt.path, w.Code, stub.calls, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "<script>") {
			t.Fatal("unescaped external content")
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("missing no-store")
		}
	}
	for _, tt := range []struct {
		err    error
		status int
		text   string
	}{
		{nil, 200, "No movies found"},
		{tmdb.ErrNotConfigured, 503, "not configured"},
		{tmdb.ErrRateLimited, 503, "Please wait"},
		{tmdb.ErrUnauthorized, 502, "unavailable"},
		{context.DeadlineExceeded, 502, "unavailable"},
	} {
		stub.result.Results = []tmdb.MovieSummary{}
		stub.err = tt.err
		w := portalRequest(h, "GET", "/admin/movies/search?q=Halloween", nil, cookie)
		if w.Code != tt.status || !strings.Contains(w.Body.String(), tt.text) {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
	}
}

func TestMovieSearchRequiresAdmin(t *testing.T) {
	db, err := openDatabase(context.Background(), filepath.Join(t.TempDir(), "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a, _ := portalFixture(t, db)
	stub := &searchStub{}
	h, err := newHandlerWithMovieSearch(a, db, stub)
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GET", "POST"} {
		w := portalRequest(h, method, "/admin/movies/search?q=Halloween", nil)
		if w.Code < 300 {
			t.Fatalf("unauthorized status %d", w.Code)
		}
	}
	cookie := adminCookie(t, h)
	w := portalRequest(h, "POST", "/admin/movies/search", nil, cookie)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST = %d", w.Code)
	}
	if stub.calls != 0 {
		t.Fatal("unauthorized search reached TMDB")
	}
	// An invalid personal cookie must not confer administrator access.
	w = portalRequest(h, "GET", "/admin/movies/search?q=Halloween", nil, &http.Cookie{Name: sessionCookie, Value: "personal"})
	if w.Code != http.StatusSeeOther {
		t.Fatal(w.Code)
	}

}

func TestMovieSearchCompletionLog(t *testing.T) {
	db, err := openDatabase(context.Background(), filepath.Join(t.TempDir(), "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a, _ := portalFixture(t, db)
	stub := &searchStub{result: tmdb.SearchResults{Page: 1, Results: []tmdb.MovieSummary{}}}
	h, err := newHandlerWithMovieSearch(a, db, stub)
	if err != nil {
		t.Fatal(err)
	}
	cookie := adminCookie(t, h)
	for _, tt := range []struct {
		name, query, message, outcome string
		err                           error
	}{
		{"form", "", "http request", "", nil},
		{"success", "?q=%20Halloween%20%26%20II%20&api_key=excluded-secret", "movie search", "success", nil},
		{"validation", "?q=", "movie search", "invalid_query", nil},
		{"configuration", "?q=%20Halloween%20%26%20II%20&api_key=excluded-secret", "movie search", "not_configured", tmdb.ErrNotConfigured},
		{"rate limit", "?q=%20Halloween%20%26%20II%20&api_key=excluded-secret", "movie search", "rate_limited", tmdb.ErrRateLimited},
		{"failure", "?q=%20Halloween%20%26%20II%20&api_key=excluded-secret", "movie search", "upstream_failure", tmdb.ErrUnavailable},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stub.err = tt.err
			var output bytes.Buffer
			logger, err := newLogger(&output, "info")
			if err != nil {
				t.Fatal(err)
			}
			portalRequest(requestLogging(logger, h), "GET", "/admin/movies/search"+tt.query, nil, cookie)
			var entry map[string]any
			// Unmarshal also rejects multiple log records for the same request.
			if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
				t.Fatal(err)
			}
			if entry["message"] != tt.message {
				t.Fatalf("unexpected message: %v", entry)
			}
			if tt.outcome != "" && entry["outcome"] != tt.outcome {
				t.Fatalf("unexpected outcome: %v", entry)
			}
			if _, exists := entry["operation"]; exists {
				t.Fatal("redundant operation field")
			}
			if tt.outcome != "" && tt.outcome != "invalid_query" {
				if entry["search_term"] != "Halloween & II" {
					t.Fatalf("unexpected search term: %v", entry)
				}
			} else if _, exists := entry["search_term"]; exists {
				t.Fatal("search term logged without a valid search")
			}
			if strings.Contains(output.String(), "excluded-secret") || strings.Contains(output.String(), "api_key") {
				t.Fatal("raw query string leaked into log")
			}
		})
	}
}
