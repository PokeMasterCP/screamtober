package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pokemastercp/screamtober/internal/tmdb"
)

type searchStub struct {
	calls  int
	pages  []int
	result tmdb.SearchResults
	err    error
}

func (s *searchStub) SearchMovies(ctx context.Context, query string, options tmdb.SearchOptions) (tmdb.SearchResults, error) {
	s.calls++
	s.pages = append(s.pages, options.Page)
	if _, ok := ctx.Deadline(); !ok {
		panic("search must have deadline")
	}
	return s.result, s.err
}

func TestMovieSearchPage(t *testing.T) {
	db, _, err := openDatabase(context.Background(), filepath.Join(t.TempDir(), "search.db"))
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
	for i, tt := range []struct {
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
		w := portalRequest(h, "GET", fmt.Sprintf("/admin/movies/search?q=Halloween%d", i), nil, cookie)
		if w.Code != tt.status || !strings.Contains(w.Body.String(), tt.text) {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
	}
}

func TestMovieSearchPageSize(t *testing.T) {
	db, _, err := openDatabase(context.Background(), filepath.Join(t.TempDir(), "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a, _ := portalFixture(t, db)
	var results []tmdb.MovieSummary
	for i := int64(1); i <= 20; i++ {
		results = append(results, tmdb.MovieSummary{ID: 1000 + i, Title: fmt.Sprintf("Result %02d", i)})
	}
	stub := &searchStub{result: tmdb.SearchResults{Page: 1, Results: results, TotalPages: 1, TotalResults: 20}}
	h, err := newHandlerWithMovieSearch(a, db, stub)
	if err != nil {
		t.Fatal(err)
	}
	cookie := adminCookie(t, h)
	first := portalRequest(h, "GET", "/admin/movies/search?q=Result&year=2026", nil, cookie).Body.String()
	if strings.Count(first, `name="movie_id"`) != movieSearchPageSize || !strings.Contains(first, "Result 01") || strings.Contains(first, "Result 11") {
		t.Fatal("first page did not show the first ten results", first)
	}
	if !strings.Contains(first, "page=2") || strings.Contains(first, "Previous") {
		t.Fatal("first page pagination")
	}
	second := portalRequest(h, "GET", "/admin/movies/search?q=Result&year=2026&page=2", nil, cookie).Body.String()
	if strings.Count(second, `name="movie_id"`) != movieSearchPageSize || strings.Contains(second, "Result 10") || !strings.Contains(second, "Result 20") {
		t.Fatal("second page did not show the last ten results", second)
	}
	if strings.Contains(second, ">Next</a>") || !strings.Contains(second, "page=1") {
		t.Fatal("second page pagination")
	}
	portalRequest(h, "GET", "/admin/movies/search?q=Result&year=2026", nil, cookie)
	if stub.calls != 1 {
		t.Fatalf("browsing both halves and returning fetched TMDB %d times; want 1", stub.calls)
	}
	stub.result = tmdb.SearchResults{Page: 2, Results: []tmdb.MovieSummary{{ID: 1021, Title: "Result 21"}}, TotalPages: 2, TotalResults: 21}
	third := portalRequest(h, "GET", "/admin/movies/search?q=Result&year=2026&page=3", nil, cookie)
	if third.Code != 200 || !strings.Contains(third.Body.String(), "Result 21") || strings.Contains(third.Body.String(), ">Next</a>") || stub.calls != 2 || stub.pages[1] != 2 {
		t.Fatal("new upstream page was not fetched correctly", stub.pages, third.Body.String())
	}
	back := portalRequest(h, "GET", "/admin/movies/search?q=Result&year=2027&page=2", nil, cookie)
	if back.Code != 200 || !strings.Contains(back.Body.String(), "Result 20") || stub.calls != 2 {
		t.Fatal("returning to a cached upstream page did not reuse it", back.Body.String(), stub.calls)
	}
	portalRequest(h, "GET", "/admin/movies/search?q=Different&page=3", nil, cookie)
	if stub.calls != 3 {
		t.Fatal("different query reused unrelated results", stub.calls)
	}
	other := adminCookie(t, h)
	portalRequest(h, "GET", "/admin/movies/search?q=Result&page=3", nil, other)
	if stub.calls != 4 {
		t.Fatal("different session reused results", stub.calls)
	}
	stub.err = tmdb.ErrUnavailable
	for i := 0; i < 2; i++ {
		if w := portalRequest(h, "GET", "/admin/movies/search?q=Failure", nil, cookie); w.Code != 502 {
			t.Fatal("upstream failure status", w.Code)
		}
	}
	if stub.calls != 6 {
		t.Fatal("upstream failure was cached", stub.calls)
	}
	if w := portalRequest(h, "GET", "/admin/movies/search?q=Result&page=1001", nil, cookie); w.Code != 400 {
		t.Fatal("page limit", w.Code)
	}
}

func TestMovieSearchRequiresAdmin(t *testing.T) {
	db, _, err := openDatabase(context.Background(), filepath.Join(t.TempDir(), "search.db"))
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
	db, _, err := openDatabase(context.Background(), filepath.Join(t.TempDir(), "search.db"))
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
		name, query, outcome, reason string
		err                          error
	}{
		{"form", "", "", "", nil},
		{"success", "?q=%20Halloween%20%26%20II%20&api_key=excluded-secret", "success", "", nil},
		{"validation", "?q=", "rejected", "invalid_query", nil},
		{"configuration", "?q=%20Halloween%20%26%20II%20&api_key=excluded-secret", "failed", "not_configured", tmdb.ErrNotConfigured},
		{"rate limit", "?q=%20Halloween%20%26%20II%20&api_key=excluded-secret", "failed", "rate_limited", tmdb.ErrRateLimited},
		{"rejected key", "?q=%20Halloween%20%26%20II%20&api_key=excluded-secret", "failed", "unauthorized", tmdb.ErrUnauthorized},
		{"failure", "?q=%20Halloween%20%26%20II%20&api_key=excluded-secret", "failed", "upstream_error", tmdb.ErrUnavailable},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Each scenario needs a cache miss to exercise its upstream outcome.
			cookie = adminCookie(t, h)
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
			field := func(key string) string { value, _ := entry[key].(string); return value }
			wantEvent := "movie.search"
			if tt.outcome == "" {
				wantEvent = ""
			}
			if entry["message"] != "http request" || field("event") != wantEvent {
				t.Fatalf("unexpected event: %v", entry)
			}
			if field("outcome") != tt.outcome || field("reason") != tt.reason {
				t.Fatalf("unexpected outcome: %v", entry)
			}
			if tt.err != nil && entry["error"] != tt.err.Error() {
				t.Fatalf("upstream error missing: %v", entry)
			}
			if tt.outcome == "success" || tt.outcome == "failed" {
				if entry["search_term"] != "Halloween & II" || entry["tmdb_cache"] != "miss" {
					t.Fatalf("unexpected search details: %v", entry)
				}
			} else if _, exists := entry["search_term"]; exists {
				t.Fatal("search term logged without a valid search")
			}
			if strings.Contains(output.String(), "excluded-secret") || strings.Contains(output.String(), "api_key") {
				t.Fatal("raw query string leaked into log")
			}
		})
	}
	t.Run("cached page", func(t *testing.T) {
		stub.err = nil
		cookie := adminCookie(t, h)
		portalRequest(h, "GET", "/admin/movies/search?q=Halloween", nil, cookie)
		calls := stub.calls
		r := httptest.NewRequest("GET", "/admin/movies/search?q=Halloween", nil)
		r.AddCookie(cookie)
		_, entry := loggedRequest(t, h, r)
		if stub.calls != calls || entry["outcome"] != "success" || entry["tmdb_cache"] != "hit" || entry["tmdb_ms"] != nil {
			t.Fatalf("cached search must not call TMDB: %v", entry)
		}
	})
}
