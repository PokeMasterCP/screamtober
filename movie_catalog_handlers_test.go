package main

import (
	"context"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pokemastercp/screamtober/internal/store"
	"github.com/pokemastercp/screamtober/internal/tmdb"
)

func TestAddMovieFromSearch(t *testing.T) {
	db, err := openDatabase(context.Background(), filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a, _ := portalFixture(t, db)
	date, overview := "1978-10-24", "Original overview"
	stub := &searchStub{result: tmdb.SearchResults{Page: 1, TotalPages: 2, TotalResults: 21, Results: []tmdb.MovieSummary{{ID: 948, Title: "Halloween", ReleaseDate: &date, Overview: &overview}}}}
	h, err := newHandlerWithMovieSearch(a, db, stub)
	if err != nil {
		t.Fatal(err)
	}
	cookie := adminCookie(t, h)
	page := portalRequest(h, "GET", "/admin/movies/search?q=Halloween", nil, cookie)
	ref := regexp.MustCompile(`name="search_reference" value="([^"]+)"`).FindStringSubmatch(page.Body.String())
	if len(ref) != 2 || !strings.Contains(page.Body.String(), "1978") || !strings.Contains(page.Body.String(), ">Next</a>") {
		t.Fatal(page.Body.String())
	}
	form := url.Values{"search_reference": {ref[1]}, "movie_id": {"948"}, "title": {"Forged title"}, "overview": {"Forged overview"}}
	other := adminCookie(t, h)
	if w := portalRequest(h, "POST", "/admin/movies", form, other); w.Code != 400 {
		t.Fatal("another session used search", w.Code)
	}
	if w := portalRequest(h, "POST", "/admin/movies", form); w.Code != 403 {
		t.Fatal("visitor saved movie", w.Code)
	}
	r := httptest.NewRequest("POST", "http://example.com/admin/movies", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://attacker.example")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin save accepted", w.Code)
	}
	form.Set("movie_id", "999")
	if w := portalRequest(h, "POST", "/admin/movies", form, cookie); w.Code != 400 {
		t.Fatal("uncached movie accepted", w.Code)
	}
	form.Set("movie_id", "948")
	for _, want := range []string{"added to your catalog", "already in your catalog"} {
		w := portalRequest(h, "POST", "/admin/movies", form, cookie)
		if w.Code != 200 || !strings.Contains(w.Body.String(), want) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	movie, err := store.New(db).GetMovieByTMDBID(context.Background(), 948)
	if err != nil || movie.Title != "Halloween" || movie.Overview.String != overview || movie.ReleaseDate.String != date || movie.PosterPath.Valid {
		t.Fatal(movie, err)
	}
	if stub.calls != 1 {
		t.Fatal("save made another TMDB call", stub.calls)
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM challenge_movies").Scan(&count); err != nil || count != 0 {
		t.Fatal("catalog save changed watchlist", err, count)
	}
	// Logging out revokes the session even while its cached search still exists.
	portalRequest(h, "POST", "/admin/logout", nil, cookie)
	if w := portalRequest(h, "POST", "/admin/movies", form, cookie); w.Code != 403 {
		t.Fatal("logged-out session saved movie", w.Code)
	}
}

func TestMovieSearchCacheExpiryAndCapacity(t *testing.T) {
	var cache movieSearchCache
	session := [32]byte{1}
	key := cache.put(session, "Movie", nil)
	entry := cache.entries[key]
	entry.expires = time.Now().Add(-time.Second)
	cache.entries[key] = entry
	if _, ok := cache.get(key, session); ok {
		t.Fatal("expired search accepted")
	}
	for i := 0; i < maxCachedSearches+10; i++ {
		cache.put(session, "Movie", nil)
	}
	if len(cache.entries) != maxCachedSearches {
		t.Fatal("unbounded cache")
	}
	var restarted movieSearchCache
	if _, ok := restarted.get(key, session); ok {
		t.Fatal("unknown search accepted")
	}
}
