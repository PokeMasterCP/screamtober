package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func ratingRequest(h http.Handler, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestRatingCreateEditAndPublicCards(t *testing.T) {
	db := schemaFixture(t)
	h := challengeHTTPFixture(t, db)
	cookie := loginCookie(t, h)
	execSchema(t, db, `DELETE FROM ratings WHERE user_id = 2 AND challenge_movie_id = 1`)
	for _, score := range []string{"1", "5", "3"} {
		w := ratingRequest(h, "/challenges/2026/movies/1/rating", "score="+score+"&user_id=1", cookie)
		if w.Code != 303 || w.Header().Get("Location") != "/challenges/2026#movie-1" {
			t.Fatalf("save = %d %s", w.Code, w.Body.String())
		}
		var count, actual int
		if err := db.QueryRow(`SELECT count(*), max(score) FROM ratings WHERE user_id = 2 AND challenge_movie_id = 1`).Scan(&count, &actual); err != nil || count != 1 || actual != int(score[0]-'0') {
			t.Fatalf("rating = %d, %d, %v", count, actual, err)
		}
	}
	// Repeated movies and other authors/years retain independent votes.
	var unchanged int
	if err := db.QueryRow(`SELECT count(*) FROM ratings WHERE (user_id=1 AND challenge_movie_id=1 AND score=1) OR (user_id=2 AND challenge_movie_id=2 AND score=2) OR (user_id=2 AND challenge_movie_id=3 AND score=3)`).Scan(&unchanged); err != nil || unchanged != 3 {
		t.Fatalf("unrelated ratings changed: %d %v", unchanged, err)
	}
	for _, c := range []*http.Cookie{nil, cookie} {
		w := authRequest(h, "GET", "/challenges/2026", "", c)
		body := w.Body.String()
		for _, want := range []string{"Household rating: 2.0 / 5 (2 ratings)", "Member: 3 / 5", "Owner: 1 / 5"} {
			if w.Code != 200 || !strings.Contains(body, want) {
				t.Fatalf("missing %q", want)
			}
		}
		if strings.Contains(body, `class="rating-form"`) != (c != nil) {
			t.Fatal("wrong rating controls")
		}
		if c != nil && !strings.Contains(body, `<option value="3" selected>3 stars</option>`) {
			t.Fatal("own rating not selected")
		}
	}
	// The owner's personal session can also rate.
	setTestUserToken(t, db, 1, testToken+"owner")
	login := authRequest(h, "POST", "/login", testToken+"owner", nil)
	owner := activeSessionCookie(t, login, sessionCookie)
	if w := ratingRequest(h, "/challenges/2027/movies/3/rating", "score=4", owner); w.Code != 303 {
		t.Fatalf("owner save = %d", w.Code)
	}
}

func TestRatingRejectsInvalidInput(t *testing.T) {
	db := schemaFixture(t)
	h := challengeHTTPFixture(t, db)
	cookie := loginCookie(t, h)
	for _, body := range []string{"", "score=0", "score=6", "score=-1", "score=3.5", "score=abc", "score=9999999999999999999999", "score=1&score=5", "score=%ZZ", "score=" + strings.Repeat("1", 4097)} {
		if w := ratingRequest(h, "/challenges/2026/movies/1/rating", body, cookie); w.Code != 400 {
			t.Errorf("body %.30q = %d", body, w.Code)
		}
	}
	for _, path := range []string{"/challenges/no/movies/1/rating", "/challenges/+2026/movies/1/rating", "/challenges/2025/movies/1/rating", "/challenges/2027/movies/1/rating", "/challenges/2026/movies/999/rating", "/challenges/2026/movies/0/rating", "/challenges/2026/movies/no/rating"} {
		if w := ratingRequest(h, path, "score=4", cookie); w.Code != 404 {
			t.Errorf("%s = %d", path, w.Code)
		}
	}
	var watchedCount int
	if err := db.QueryRow(`SELECT count(*) FROM challenge_movies WHERE watched_at IS NOT NULL`).Scan(&watchedCount); err != nil || watchedCount != 0 {
		t.Fatalf("rejected request changed watched status: %d, %v", watchedCount, err)
	}
	var score int
	if err := db.QueryRow(`SELECT score FROM ratings WHERE user_id=2 AND challenge_movie_id=1`).Scan(&score); err != nil || score != 5 {
		t.Fatal("invalid request changed rating")
	}
}

func TestRatingAuthorizationAndCSRF(t *testing.T) {
	for _, scenario := range []string{"anonymous", "admin", "both sessions", "disabled", "replaced", "cross-origin", "cross-site", "get"} {
		t.Run(scenario, func(t *testing.T) {
			db := schemaFixture(t)
			h := challengeHTTPFixture(t, db)
			cookie := loginCookie(t, h)
			r := httptest.NewRequest("POST", "/challenges/2026/movies/1/rating", strings.NewReader("score=4"))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			want := 401
			switch scenario {
			case "anonymous":
				cookie = nil
			case "admin", "both sessions":
				login := authRequest(h, "POST", "/login", testAdminToken, nil)
				r.AddCookie(activeSessionCookie(t, login, adminSessionCookie))
				if scenario == "admin" {
					cookie = nil
				}
				want = 403
			case "disabled":
				execSchema(t, db, `UPDATE users SET disabled_at = CURRENT_TIMESTAMP WHERE id=2`)
			case "replaced":
				setTestUserToken(t, db, 2, "replacement-token")
			case "cross-origin":
				r.Header.Set("Origin", "https://evil.example")
				want = 403
			case "cross-site":
				r.Header.Set("Sec-Fetch-Site", "cross-site")
				want = 403
			case "get":
				r.Method = "GET"
				want = 405
			}
			if cookie != nil {
				r.AddCookie(cookie)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != want {
				t.Fatalf("status = %d, want %d: %s", w.Code, want, w.Body.String())
			}
			var watchedCount int
			if err := db.QueryRow(`SELECT count(*) FROM challenge_movies WHERE watched_at IS NOT NULL`).Scan(&watchedCount); err != nil || watchedCount != 0 {
				t.Fatalf("rejected request changed watched status: %d, %v", watchedCount, err)
			}
			var score int
			if err := db.QueryRow(`SELECT score FROM ratings WHERE user_id=2 AND challenge_movie_id=1`).Scan(&score); err != nil || score != 5 {
				t.Fatal("unauthorized write")
			}
		})
	}
}

func TestRatingDatabaseFailure(t *testing.T) {
	db := schemaFixture(t)
	h := challengeHTTPFixture(t, db)
	cookie := loginCookie(t, h)
	execSchema(t, db, `ALTER TABLE ratings RENAME TO unavailable_ratings`)
	var logs bytes.Buffer
	logger, err := newLogger(&logs, "info")
	if err != nil {
		t.Fatal(err)
	}
	w := ratingRequest(requestLogging(logger, h), "/challenges/2026/movies/1/rating", "score=4", cookie)
	if w.Code != 500 || w.Body.String() != "Unable to save rating. Please try again later.\n" {
		t.Fatalf("failure = %d %s", w.Code, w.Body.String())
	}
	var event map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &event); err != nil || event["message"] != "save rating" || event["error"] == nil {
		t.Fatalf("missing single failure event: %s", logs.String())
	}
}

func TestRatingMarksOnlyItsEntryWatched(t *testing.T) {
	db := schemaFixture(t)
	execSchema(t, db, `DELETE FROM ratings`)
	h := challengeHTTPFixture(t, db)
	member := loginCookie(t, h)
	if w := ratingRequest(h, "/challenges/2026/movies/1/rating", "score=4", member); w.Code != 303 {
		t.Fatal(w.Code, w.Body.String())
	}
	var watched string
	if err := db.QueryRow(`SELECT watched_at FROM challenge_movies WHERE id=1`).Scan(&watched); err != nil || watched == "" {
		t.Fatalf("watched time = %q, %v", watched, err)
	}
	var otherWatched int
	if err := db.QueryRow(`SELECT count(*) FROM challenge_movies WHERE id != 1 AND watched_at IS NOT NULL`).Scan(&otherWatched); err != nil || otherWatched != 0 {
		t.Fatalf("other entries changed: %d, %v", otherWatched, err)
	}
	page := authRequest(h, "GET", "/challenges/2026", "", member).Body.String()
	if !strings.Contains(page, `class="feature-layout" id="movie-2"`) || !strings.Contains(page, "1 watched") {
		t.Fatal("rating did not advance shared progress")
	}
	// Use a fixed historical value to prove edits and other authors do not replace it.
	execSchema(t, db, `UPDATE challenge_movies SET watched_at='2026-10-01 20:00:00' WHERE id=1`)
	setTestUserToken(t, db, 1, testToken+"owner")
	owner := personalCookie(t, h, testToken+"owner")
	for _, cookie := range []*http.Cookie{member, owner} {
		if w := ratingRequest(h, "/challenges/2026/movies/1/rating", "score=5", cookie); w.Code != 303 {
			t.Fatal(w.Code)
		}
		if err := db.QueryRow(`SELECT watched_at FROM challenge_movies WHERE id=1`).Scan(&watched); err != nil || watched != "2026-10-01 20:00:00" {
			t.Fatalf("watched time replaced: %q, %v", watched, err)
		}
	}
}

func TestRatingAndWatchedStateRollbackTogether(t *testing.T) {
	for _, operation := range []string{"rating", "watched"} {
		t.Run(operation, func(t *testing.T) {
			db := schemaFixture(t)
			execSchema(t, db, `DELETE FROM ratings WHERE challenge_movie_id=1`)
			if operation == "rating" {
				execSchema(t, db, `CREATE TRIGGER reject_rating BEFORE INSERT ON ratings BEGIN SELECT RAISE(ABORT, 'rating rejected'); END`)
			} else {
				execSchema(t, db, `CREATE TRIGGER reject_watched BEFORE UPDATE OF watched_at ON challenge_movies BEGIN SELECT RAISE(ABORT, 'watched rejected'); END`)
			}
			h := challengeHTTPFixture(t, db)
			if w := ratingRequest(h, "/challenges/2026/movies/1/rating", "score=4", loginCookie(t, h)); w.Code != 500 {
				t.Fatal(w.Code)
			}
			var ratings, watched int
			if err := db.QueryRow(`SELECT count(*) FROM ratings WHERE challenge_movie_id=1`).Scan(&ratings); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT count(*) FROM challenge_movies WHERE watched_at IS NOT NULL`).Scan(&watched); err != nil {
				t.Fatal(err)
			}
			if ratings != 0 || watched != 0 {
				t.Fatalf("partial save: %d ratings, %d watched", ratings, watched)
			}
		})
	}
}
