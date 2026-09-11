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
	page := portalRequest(h, "GET", "/admin/movies/search?q=Halloween&year=2026", nil, cookie)
	ref := regexp.MustCompile(`name="search_reference" value="([^"]+)"`).FindStringSubmatch(page.Body.String())
	if len(ref) != 2 || !strings.Contains(page.Body.String(), "1978") || !strings.Contains(page.Body.String(), ">Next</a>") {
		t.Fatal(page.Body.String())
	}
	if !strings.Contains(page.Body.String(), "year=2026") || !strings.Contains(page.Body.String(), "Add to 2026") {
		t.Fatal("year missing from form or pagination")
	}
	form := url.Values{"year": {"2026"}, "search_reference": {ref[1]}, "movie_id": {"948"}, "title": {"Forged title"}, "overview": {"Forged overview"}}
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
	for _, want := range []string{"added to 2026", "already in 2026"} {
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
	if err := db.QueryRow("SELECT count(*) FROM challenge_movies").Scan(&count); err != nil || count != 1 {
		t.Fatal("yearly pick not saved", err, count)
	}
	public := portalRequest(h, "GET", "/challenges/2026", nil)
	if public.Code != 200 || !strings.Contains(public.Body.String(), "Unscheduled") || !strings.Contains(public.Body.String(), "Halloween") {
		t.Fatal("unscheduled pick not visible", public.Body.String())
	}
	// A cached search still issues a new reference for an intentional repeat pick.
	page = portalRequest(h, "GET", "/admin/movies/search?q=Halloween&year=2026", nil, cookie)
	repeatRef := regexp.MustCompile(`name="search_reference" value="([^"]+)"`).FindStringSubmatch(page.Body.String())
	if len(repeatRef) != 2 || repeatRef[1] == ref[1] || stub.calls != 1 {
		t.Fatal("cached search did not issue a fresh reference", page.Body.String(), stub.calls)
	}
	form.Set("search_reference", repeatRef[1])
	if w := portalRequest(h, "POST", "/admin/movies", form, cookie); w.Code != 200 || !strings.Contains(w.Body.String(), "added to 2026") {
		t.Fatal("intentional repeat pick failed", w.Code, w.Body.String())
	}
	if err := db.QueryRow("SELECT count(*) FROM challenge_movies").Scan(&count); err != nil || count != 2 {
		t.Fatal("repeat pick not saved", err, count)
	}
	form.Set("year", "invalid")
	if w := portalRequest(h, "POST", "/admin/movies", form, cookie); w.Code != 400 {
		t.Fatal("invalid year accepted")
	}
	form.Set("year", "2026")
	// Logging out revokes the session even while its cached search still exists.
	portalRequest(h, "POST", "/admin/logout", nil, cookie)
	if w := portalRequest(h, "POST", "/admin/movies", form, cookie); w.Code != 403 {
		t.Fatal("logged-out session saved movie", w.Code)
	}
}

func TestMovieSearchCacheExpiryAndCapacity(t *testing.T) {
	var cache movieSearchCache
	session := [32]byte{1}
	key := cache.put(cachedMovieSearch{session: session, query: "Movie", expires: time.Now().Add(searchCacheTTL)})
	entry := cache.entries[key]
	entry.expires = time.Now().Add(-time.Second)
	cache.entries[key] = entry
	if _, ok := cache.get(key, session); ok {
		t.Fatal("expired search accepted")
	}
	for i := 0; i < maxCachedSearches+10; i++ {
		cache.put(cachedMovieSearch{session: session, query: "Movie", expires: time.Now().Add(searchCacheTTL)})
	}
	if len(cache.entries) != maxCachedSearches {
		t.Fatal("unbounded cache")
	}
	var restarted movieSearchCache
	if _, ok := restarted.get(key, session); ok {
		t.Fatal("unknown search accepted")
	}
}

func TestMovieSearchPageCacheExpiry(t *testing.T) {
	var cache movieSearchCache
	session := [32]byte{1}
	expires := time.Now().Add(time.Minute)
	key := cache.put(cachedMovieSearch{session: session, query: "Movie", results: tmdb.SearchResults{Page: 1, TotalPages: 3, TotalResults: 45}, expires: expires})
	entry, ok := cache.find(session, "Movie", 1)
	if !ok || entry.results.TotalPages != 3 || entry.results.TotalResults != 45 {
		t.Fatal("pagination metadata not cached", entry, ok)
	}
	newKey := cache.put(entry)
	if !cache.entries[newKey].expires.Equal(expires) {
		t.Fatal("reusing a cached page extended its lifetime")
	}
	for _, key := range []string{key, newKey} {
		entry := cache.entries[key]
		entry.expires = time.Now().Add(-time.Second)
		cache.entries[key] = entry
	}
	if _, ok := cache.find(session, "Movie", 1); ok {
		t.Fatal("expired page was reused")
	}
}
