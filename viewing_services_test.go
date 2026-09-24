package main

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/pokemastercp/screamtober/internal/store"
	"github.com/pokemastercp/screamtober/internal/tmdb"
)

func TestViewingServiceEntries(t *testing.T) {
	db := schemaFixture(t)
	ctx := context.Background()
	movie := tmdb.MovieSummary{ID: 321, Title: "Service pick"}
	for _, pick := range []struct {
		year         int
		ref, service string
	}{{2028, "a", "netflix"}, {2028, "b", "plex"}, {2029, "a", "theaters"}} {
		if _, err := addYearlyMovie(ctx, db, movie, pick.year, pick.ref, pick.service); err != nil {
			t.Fatal(err)
		}
	}
	// A POST retry must not overwrite the service chosen by the original submission.
	if added, err := addYearlyMovie(ctx, db, movie, 2028, "a", "shudder"); err != nil || !added.duplicate {
		t.Fatal(added, err)
	}
	q := store.New(db)
	for year, want := range map[int64][]string{2028: {"netflix", "plex"}, 2029: {"theaters"}} {
		challenge, err := q.GetChallengeByYear(ctx, year)
		if err != nil {
			t.Fatal(err)
		}
		entries, err := q.ListChallengeMovies(ctx, challenge.ID)
		if err != nil || len(entries) != len(want) {
			t.Fatal(entries, err)
		}
		for i, entry := range entries {
			if entry.ViewingService != want[i] {
				t.Fatal(entry)
			}
		}
	}
	if _, err := addYearlyMovie(ctx, db, movie, 2030, "invalid", "forged"); !errors.Is(err, errViewingService) {
		t.Fatal(err)
	}
	if _, err := q.GetChallengeByYear(ctx, 2030); err == nil {
		t.Fatal("invalid selection created a challenge")
	}
}

func TestViewingServiceFormAndPublicDisplay(t *testing.T) {
	db := schemaFixture(t)
	a, _ := portalFixture(t, db)
	stub := &searchStub{result: tmdb.SearchResults{Page: 1, TotalPages: 1, Results: []tmdb.MovieSummary{{ID: 321, Title: "Service pick"}}}}
	h, err := newHandlerWithMovieSearch(a, db, stub)
	if err != nil {
		t.Fatal(err)
	}
	cookie := adminCookie(t, h)
	page := portalRequest(h, "GET", "/admin/movies/search?q=Service&year=2028", nil, cookie)
	ref := regexp.MustCompile(`name="search_reference" value="([^"]+)"`).FindStringSubmatch(page.Body.String())
	if len(ref) != 2 {
		t.Fatal(page.Body.String())
	}
	for _, service := range selectableViewingServices() {
		if !strings.Contains(page.Body.String(), service.Icon) || !strings.Contains(html.UnescapeString(page.Body.String()), service.Name) {
			t.Fatal("missing choice", service)
		}
		asset := portalRequest(h, "GET", service.Icon, nil)
		if asset.Code != 200 || !strings.HasPrefix(asset.Header().Get("Content-Type"), "image/") {
			t.Fatal("missing icon", service, asset.Code)
		}
	}
	form := url.Values{"year": {"2028"}, "search_reference": {ref[1]}, "movie_id": {"321"}, "viewing_service": {"forged"}}
	if res := portalRequest(h, "POST", "/admin/movies", form, cookie); res.Code != 400 || !strings.Contains(res.Body.String(), "supported viewing service") {
		t.Fatal(res.Code, res.Body.String())
	}
	form["viewing_service"] = []string{"netflix", "plex"}
	if res := portalRequest(h, "POST", "/admin/movies", form, cookie); res.Code != 400 {
		t.Fatal(res.Code)
	}
	form.Set("viewing_service", "netflix")
	if res := portalRequest(h, "POST", "/admin/movies", form); res.Code != 403 {
		t.Fatal("visitor wrote service", res.Code)
	}
	if res := portalRequest(h, "POST", "/admin/movies", form, cookie); res.Code != 200 {
		t.Fatal(res.Code, res.Body.String())
	}
	setTestUserToken(t, db, 2, "service-member-token")
	member := personalCookie(t, h, "service-member-token")
	if res := portalRequest(h, "POST", "/admin/movies", form, member); res.Code != 403 {
		t.Fatal("member wrote service", res.Code)
	}
	for _, watched := range []bool{false, true} {
		if watched {
			execSchema(t, db, `UPDATE challenge_movies SET watched_at='2028-10-01' WHERE viewing_service='netflix'`)
		}
		for _, cookies := range [][]*http.Cookie{nil, {member}} {
			res := portalRequest(h, "GET", "/challenges/2028", nil, cookies...)
			label := "Watching on"
			if watched {
				label = "Watched on"
			}
			if res.Code != 200 || !strings.Contains(res.Body.String(), label) || !strings.Contains(res.Body.String(), "/assets/services/netflix.svg") {
				t.Fatal(res.Code, res.Body.String())
			}
		}
	}
}

func TestRetiredViewingService(t *testing.T) {
	original := viewingServices
	viewingServices = append(append([]viewingService(nil), original...), viewingService{ID: "retired", Name: "Former service", Icon: "/assets/services/plex.svg", Retired: true})
	defer func() { viewingServices = original }()
	if validViewingService("retired") {
		t.Fatal("retired service accepted for a new pick")
	}
	for _, service := range selectableViewingServices() {
		if service.ID == "retired" {
			t.Fatal("retired choice shown")
		}
	}
	if service := findViewingService("retired"); service.Name != "Former service" || service.Icon == "" {
		t.Fatal("historical service lost", service)
	}
}
