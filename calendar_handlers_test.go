package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/pokemastercp/screamtober/internal/store"
)

func TestCalendarArrangement(t *testing.T) {
	db := schemaFixture(t)
	execSchema(t, db, `UPDATE movies SET poster_path='/poster.jpg'; UPDATE challenge_movies SET watched_at='2026-10-01' WHERE id=1`)
	_, h := portalFixture(t, db)
	admin := adminCookie(t, h)
	q := store.New(db)
	ctx := context.Background()
	original, _ := q.ListChallengeMovies(ctx, 1)
	ratings, _ := q.ListChallengeRatings(ctx, 1)
	other, _ := q.ListChallengeMovies(ctx, 2)
	path := "/admin/calendar?year=2026"
	page := portalRequest(h, "GET", path, nil, admin)
	if page.Code != 200 {
		t.Fatal(page.Code, page.Body.String())
	}
	for _, want := range []string{"https://image.tmdb.org/t/p/w500/poster.jpg", `data-day="31"`, `name="entry_1"`, `value="1" selected`} {
		if !strings.Contains(page.Body.String(), want) {
			t.Errorf("missing %s", want)
		}
	}
	form := url.Values{"revision": {calendarRevision(original)}, "entry_1": {"2"}, "entry_2": {"1"}}
	if w := portalRequest(h, "POST", path, form, admin); w.Code != 303 {
		t.Fatal(w.Code, w.Body.String())
	}
	changed, _ := q.ListChallengeMovies(ctx, 1)
	if changed[0].ID != 2 || changed[1].ID != 1 || changed[1].WatchedAt.String != "2026-10-01" {
		t.Fatal(changed)
	}
	afterRatings, _ := q.ListChallengeRatings(ctx, 1)
	afterOther, _ := q.ListChallengeMovies(ctx, 2)
	sort.Slice(ratings, func(i, j int) bool { return ratings[i].ID < ratings[j].ID })
	sort.Slice(afterRatings, func(i, j int) bool { return afterRatings[i].ID < afterRatings[j].ID })
	if !reflect.DeepEqual(ratings, afterRatings) || !reflect.DeepEqual(other, afterOther) {
		t.Fatal("ratings or another year changed")
	}
	if w := portalRequest(h, "POST", path, form, admin); w.Code != 409 {
		t.Fatal("stale save", w.Code)
	}
	for _, fields := range []url.Values{
		{"entry_1": {"1"}, "entry_2": {"1"}},
		{"entry_1": {"32"}, "entry_2": {""}},
		{"entry_1": {"-1"}, "entry_2": {""}},
		{"entry_1": {"1", "2"}, "entry_2": {""}},
		{"entry_1": {"1"}, "entry_3": {"2"}},
		{"entry_1": {"1"}},
	} {
		fields.Set("revision", calendarRevision(changed))
		if w := portalRequest(h, "POST", path, fields, admin); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
		got, _ := q.ListChallengeMovies(ctx, 1)
		if !reflect.DeepEqual(got, changed) {
			t.Fatal("invalid save mutated lineup")
		}
	}
	form = url.Values{"revision": {calendarRevision(changed)}, "entry_1": {"31"}, "entry_2": {""}}
	if w := portalRequest(h, "POST", path, form, admin); w.Code != 303 {
		t.Fatal(w.Code, w.Body.String())
	}
	got, _ := q.ListChallengeMovies(ctx, 1)
	if got[0].Position.Int64 != 31 || got[1].Position.Valid {
		t.Fatal(got)
	}
	// A database error partway through a save must roll back the entire arrangement.
	execSchema(t, db, `CREATE TRIGGER reject_calendar BEFORE UPDATE OF position ON challenge_movies WHEN NEW.position=5 BEGIN SELECT RAISE(ABORT,'test failure'); END`)
	form = url.Values{"revision": {calendarRevision(got)}, "entry_1": {"5"}, "entry_2": {"6"}}
	if w := portalRequest(h, "POST", path, form, admin); w.Code != 500 {
		t.Fatal(w.Code)
	}
	rollback, _ := q.ListChallengeMovies(ctx, 1)
	if !reflect.DeepEqual(got, rollback) {
		t.Fatal("partial save")
	}
}

func TestCalendarAccessAndEmptyStates(t *testing.T) {
	db := schemaFixture(t)
	_, h := portalFixture(t, db)
	setTestUserToken(t, db, 1, testToken)
	personal := personalCookie(t, h, testToken)
	for _, cookie := range []*http.Cookie{nil, personal} {
		cookies := []*http.Cookie{}
		if cookie != nil {
			cookies = append(cookies, cookie)
		}
		for _, method := range []string{"GET", "POST"} {
			w := portalRequest(h, method, "/admin/calendar?year=2026", nil, cookies...)
			if w.Code != 303 && w.Code != 403 {
				t.Fatalf("unauthorized %s: %d", method, w.Code)
			}
		}
	}
	admin := adminCookie(t, h)
	for _, tt := range []struct {
		path string
		code int
		text string
	}{
		{"/admin/calendar?year=2025", 200, "No movies added for 2025"},
		{"/admin/calendar?year=nope", 400, "Choose a year"},
		{"/admin/calendar?year=0", 400, "Choose a year"},
		{"/admin/calendar?year=10000", 400, "Choose a year"},
	} {
		w := portalRequest(h, "GET", tt.path, nil, admin)
		if w.Code != tt.code || !strings.Contains(w.Body.String(), tt.text) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	req := httptest.NewRequest("POST", "https://example.com/admin/calendar?year=2026", strings.NewReader(""))
	req.Header.Set("Origin", "https://evil.example")
	req.AddCookie(admin)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal("cross-origin save", w.Code)
	}
	execSchema(t, db, `UPDATE movies SET title='<script>alert(1)</script>',poster_path='//evil.example/a.jpg'`)
	w = portalRequest(h, "GET", "/admin/calendar?year=2026", nil, admin)
	if w.Code != 200 || strings.Contains(w.Body.String(), "<script>alert(1)</script>") || strings.Contains(w.Body.String(), "evil.example") {
		t.Fatal("unsafe metadata")
	}
}
