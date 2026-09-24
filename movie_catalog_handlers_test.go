package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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

func TestUpdateViewingServiceAndDeleteMovie(t *testing.T) {
	db := schemaFixture(t)
	execSchema(t, db, `UPDATE challenge_movies SET watched_at = CURRENT_TIMESTAMP WHERE id = 1`)
	_, h := portalFixture(t, db)
	admin := adminCookie(t, h)

	page := portalRequest(h, "GET", "/admin/movies/search?year=2026", nil, admin)
	body := page.Body.String()
	if page.Code != http.StatusOK || !strings.Contains(body, "2026 lineup") || !strings.Contains(body, "Test movie") || !strings.Contains(body, "Where we’re watching") || !strings.Contains(body, `action="/admin/challenges/2026/movies/1/service"`) {
		t.Fatalf("current picks missing: %d %s", page.Code, body)
	}
	if strings.Contains(body, "Change movie") || strings.Contains(body, "Choosing replacement") || strings.Contains(body, "entry_id") {
		t.Fatal("replacement flow remains in the lineup UI")
	}
	if w := portalRequest(h, "GET", "/admin/challenges/2026/movies/1/service", nil, admin); w.Code != http.StatusMethodNotAllowed {
		t.Fatal("GET service endpoint allowed")
	}
	if w := portalRequest(h, "GET", "/admin/challenges/2026/movies/1/delete", nil, admin); w.Code != http.StatusMethodNotAllowed {
		t.Fatal("GET delete endpoint allowed")
	}
	if w := portalRequest(h, "POST", "/admin/challenges/2026/movies/1/service", url.Values{"viewing_service": {"netflix"}}); w.Code != http.StatusForbidden {
		t.Fatal("visitor updated viewing service")
	}
	if w := portalRequest(h, "POST", "/admin/challenges/2026/movies/1/delete", nil); w.Code != http.StatusForbidden {
		t.Fatal("visitor deleted movie")
	}
	if w := portalRequest(h, "POST", "/admin/challenges/2026/movies/1/service", url.Values{"viewing_service": {"unsupported"}}, admin); w.Code != http.StatusBadRequest {
		t.Fatal("unsupported viewing service accepted")
	}
	if w := portalRequest(h, "POST", "/admin/challenges/2026/movies/1/service", url.Values{"viewing_service": {"netflix"}}, admin); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin/movies/search?year=2026&updated=1" {
		t.Fatalf("service update response = %d %q", w.Code, w.Header().Get("Location"))
	}
	q := store.New(db)
	challenge, err := q.GetChallengeByYear(context.Background(), 2026)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := q.ListChallengeMovies(context.Background(), challenge.ID)
	if err != nil || len(entries) == 0 {
		t.Fatalf("entries = %+v, %v", entries, err)
	}
	entry := entries[0]
	if err != nil || entry.MovieID != 1 || entry.Position.Int64 != 1 || !entry.WatchedAt.Valid || entry.ViewingService != "netflix" {
		t.Fatalf("service update changed the movie entry = %+v, error = %v", entry, err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM ratings WHERE challenge_movie_id = 1`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("service update changed ratings = %d, error = %v", count, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM ratings WHERE challenge_movie_id = 2`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("other pick ratings changed = %d, error = %v", count, err)
	}
	if _, err := q.GetMovieByTMDBID(context.Background(), 123); err != nil {
		t.Fatalf("catalog movie removed: %v", err)
	}

	if w := portalRequest(h, "POST", "/admin/challenges/2026/movies/2/delete", nil, admin); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin/movies/search?year=2026&deleted=1" {
		t.Fatalf("delete response = %d %q", w.Code, w.Header().Get("Location"))
	}
	if err := db.QueryRow(`SELECT count(*) FROM challenge_movies WHERE id=2`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("deleted entry still exists: %d, %v", count, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM ratings WHERE challenge_movie_id = 2`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("deleted ratings = %d, error = %v", count, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM ratings WHERE challenge_movie_id = 3`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("other year's ratings changed = %d, error = %v", count, err)
	}
	if w := portalRequest(h, "POST", "/admin/challenges/2026/movies/3/delete", nil, admin); w.Code != http.StatusNotFound {
		t.Fatal("cross-year delete accepted")
	}
	if w := portalRequest(h, "POST", "/admin/challenges/2026/movies/1/delete", nil, admin); w.Code != http.StatusSeeOther {
		t.Fatalf("second delete response = %d", w.Code)
	}
}

// An old tab must never mutate a new pick after the last entry is deleted.
func TestDeletedMovieRejectsStaleRequests(t *testing.T) {
	db := schemaFixture(t)
	_, h := portalFixture(t, db)
	admin := adminCookie(t, h)
	setTestUserToken(t, db, 2, "stale-rating-member")
	member := personalCookie(t, h, "stale-rating-member")
	if w := portalRequest(h, "POST", "/admin/challenges/2027/movies/3/delete", nil, admin); w.Code != http.StatusSeeOther {
		t.Fatalf("delete = %d: %s", w.Code, w.Body.String())
	}
	if _, err := addYearlyMovie(context.Background(), db, tmdb.MovieSummary{ID: 456, Title: "New pick"}, 2027, "new-pick", "plex"); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		path   string
		form   url.Values
		cookie *http.Cookie
	}{
		{"/admin/challenges/2027/movies/3/service", url.Values{"viewing_service": {"netflix"}}, admin},
		{"/admin/challenges/2027/movies/3/delete", nil, admin},
		{"/challenges/2027/movies/3/rating", url.Values{"score": {"5"}}, member},
	} {
		if w := portalRequest(h, "POST", tt.path, tt.form, tt.cookie); w.Code != http.StatusNotFound {
			t.Errorf("stale request %s = %d: %s", tt.path, w.Code, w.Body.String())
		}
	}
	entries, err := store.New(db).ListChallengeMovies(context.Background(), 2)
	if err != nil || len(entries) != 1 || entries[0].ID <= 3 || entries[0].ViewingService != "plex" || entries[0].Title != "New pick" {
		t.Fatalf("replacement changed: %+v, error = %v", entries, err)
	}
	var ratings int
	if err := db.QueryRow(`SELECT count(*) FROM ratings WHERE challenge_movie_id = ?`, entries[0].ID).Scan(&ratings); err != nil || ratings != 0 {
		t.Fatalf("replacement ratings = %d, error = %v", ratings, err)
	}
}

func TestUpdateViewingServiceScope(t *testing.T) {
	db := schemaFixture(t)
	ctx := context.Background()
	for _, tt := range []struct {
		entry int64
		year  int
	}{{3, 2026}, {1, 2099}, {999, 2026}} {
		if err := setYearlyMovieService(ctx, db, tt.entry, tt.year, "netflix"); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("update %+v = %v, want missing entry", tt, err)
		}
	}
	// Saving an unchanged selection still succeeds, including clearing a service.
	for _, service := range []string{"netflix", "netflix", "", ""} {
		if err := setYearlyMovieService(ctx, db, 1, 2026, service); err != nil {
			t.Fatal(err)
		}
	}
	var changed int
	if err := db.QueryRow(`SELECT count(*) FROM challenge_movies WHERE viewing_service != ''`).Scan(&changed); err != nil || changed != 0 {
		t.Fatalf("unexpected service changes = %d, error = %v", changed, err)
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

func TestAddMovieDatabaseFailureLog(t *testing.T) {
	db := schemaFixture(t)
	a, _ := portalFixture(t, db)
	stub := &searchStub{result: tmdb.SearchResults{Page: 1, TotalPages: 1, Results: []tmdb.MovieSummary{{ID: 456, Title: "New movie"}}}}
	h, err := newHandlerWithMovieSearch(a, db, stub)
	if err != nil {
		t.Fatal(err)
	}
	admin := adminCookie(t, h)
	page := portalRequest(h, "GET", "/admin/movies/search?q=New&year=2026", nil, admin)
	ref := regexp.MustCompile(`name="search_reference" value="([^"]+)"`).FindStringSubmatch(page.Body.String())
	if len(ref) != 2 {
		t.Fatal("search reference missing")
	}
	execSchema(t, db, `CREATE TRIGGER reject_catalog BEFORE INSERT ON movies BEGIN SELECT RAISE(ABORT,'catalog write failed'); END`)
	var logs bytes.Buffer
	logger, _ := newLogger(&logs, "info")
	w := portalRequest(requestLogging(logger, h), "POST", "/admin/movies", url.Values{"year": {"2026"}, "movie_id": {"456"}, "search_reference": {ref[1]}}, admin)
	if w.Code != 500 || strings.Contains(w.Body.String(), "catalog write failed") {
		t.Fatalf("response = %d %s", w.Code, w.Body.String())
	}
	var event map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &event); err != nil {
		t.Fatalf("expected one completion event: %s", logs.String())
	}
	if event["event"] != "movie.add" || event["outcome"] != "failed" || event["step"] != "add movie" || event["level"] != "error" || !strings.Contains(fmt.Sprint(event["error"]), "catalog write failed") {
		t.Fatalf("missing diagnostic: %v", event)
	}
}
