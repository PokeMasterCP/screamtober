package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func challengeHTTPFixture(t *testing.T, db *sql.DB) http.Handler {
	t.Helper()
	a, err := newAuth(testAdminToken, true, db)
	if err != nil {
		t.Fatal(err)
	}
	setTestUserToken(t, db, 2, testToken)
	h, err := newHandler(a, db)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestChallengePages(t *testing.T) {
	db := schemaFixture(t)
	execSchema(t, db, `UPDATE challenge_movies SET watched_at = CURRENT_TIMESTAMP WHERE id = 1`)
	h := challengeHTTPFixture(t, db)
	for _, tt := range []struct {
		path     string
		status   int
		contains []string
	}{
		{"/", 200, []string{"2027 movie challenge", "1 of 31 movies selected · 0 watched", "Household rating: 3.0 / 5 (1 rating)"}},
		{"/challenges/2026", 200, []string{"2026 movie challenge", "2 of 31 movies selected · 1 watched", "Household rating: 3.0 / 5 (2 ratings)", "Household rating: 2.0 / 5 (1 rating)", "Owner: 1 / 5", "Member: 5 / 5"}},
		{"/challenges/2025", 404, nil},
		{"/challenges/nonsense", 404, nil},
		{"/challenges/999999999999999999999", 404, nil},
		{"/challenges/+2026", 404, nil},
		{"/challenges/2026/extra", 404, nil},
	} {
		t.Run(tt.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if w.Code != tt.status {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
			}
			for _, want := range tt.contains {
				if !strings.Contains(w.Body.String(), want) {
					t.Errorf("missing %q in %s", want, w.Body.String())
				}
			}
			if tt.status == 200 && (w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "text/html; charset=utf-8") {
				t.Fatal("missing HTML/cache headers")
			}
		})
	}
	// A request sees current database values, without requiring a server restart.
	execSchema(t, db, `UPDATE ratings SET score = 5 WHERE challenge_movie_id = 3`)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(w.Body.String(), "Household rating: 5.0 / 5") || strings.Contains(w.Body.String(), "Owner: 1 / 5") {
		t.Fatal("page has stale data or includes ratings from another year")
	}
}

func TestChallengeEmptyStates(t *testing.T) {
	_, empty := authFixture(t, testToken, true)
	w := httptest.NewRecorder()
	empty.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "No challenges yet.") {
		t.Fatalf("empty home = %d %s", w.Code, w.Body.String())
	}
	db := schemaFixture(t)
	execSchema(t, db, `INSERT INTO challenges (year) VALUES (2028)`)
	h := challengeHTTPFixture(t, db)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/challenges/2028", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "No movies selected for this year yet.") {
		t.Fatalf("empty challenge = %d %s", w.Code, w.Body.String())
	}
	execSchema(t, db, `DELETE FROM ratings`)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/challenges/2026", nil))
	if w.Code != 200 || strings.Count(w.Body.String(), "No ratings yet.") != 2 || strings.Contains(w.Body.String(), "Household rating:") {
		t.Fatal("missing ratings must not become a zero average")
	}
}

func TestChallengeEscapingAndSignedInState(t *testing.T) {
	db := schemaFixture(t)
	execSchema(t, db, `UPDATE movies SET title = '<b>Movie</b>', overview = '<em>Overview</em>'; UPDATE users SET display_name = '<b>Member</b>' WHERE id = 2`)
	h := challengeHTTPFixture(t, db)
	login := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(url.Values{"token": {testToken}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(login, r)
	if login.Code != http.StatusSeeOther {
		t.Fatalf("login = %d", login.Code)
	}
	for _, signedIn := range []bool{false, true} {
		r := httptest.NewRequest(http.MethodGet, "/challenges/2026", nil)
		if signedIn {
			r.AddCookie(activeSessionCookie(t, login, sessionCookie))
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		body := w.Body.String()
		if w.Code != 200 || strings.Contains(body, "<b>Movie</b>") || !strings.Contains(body, "&lt;b&gt;Movie&lt;/b&gt;") || !strings.Contains(body, "&lt;em&gt;Overview&lt;/em&gt;") || !strings.Contains(body, "&lt;b&gt;Member&lt;/b&gt;") {
			t.Fatalf("unescaped or missing public data: %s", body)
		}
		if strings.Contains(body, "Sign out") != signedIn || strings.Contains(body, `href="/login"`) == signedIn {
			t.Fatal("incorrect authentication controls")
		}
		// The temporary shared login grants no challenge mutation access.
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
			write := httptest.NewRequest(method, "/challenges/2026", nil)
			if signedIn {
				write.AddCookie(activeSessionCookie(t, login, sessionCookie))
			}
			response := httptest.NewRecorder()
			h.ServeHTTP(response, write)
			if response.Code != http.StatusMethodNotAllowed {
				t.Fatalf("%s status = %d", method, response.Code)
			}
		}
	}
}

func TestChallengeDatabaseFailureLogging(t *testing.T) {
	for _, failure := range []string{"closed database", "missing ratings table"} {
		t.Run(failure, func(t *testing.T) {
			db := schemaFixture(t)
			h := challengeHTTPFixture(t, db)
			if failure == "closed database" {
				db.Close()
			} else {
				execSchema(t, db, `ALTER TABLE ratings RENAME TO unavailable_ratings`)
			}
			var logs bytes.Buffer
			logger, err := newLogger(&logs, "info")
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			requestLogging(logger, h).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
			if w.Code != 500 || w.Body.String() != "Unable to load challenge. Please try again later.\n" {
				t.Fatalf("error response = %d %s", w.Code, w.Body.String())
			}
			var event map[string]any
			if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &event); err != nil {
				t.Fatalf("expected one combined log event: %v", err)
			}
			if event["message"] != "load challenge failed" || event["level"] != "error" || event["error"] == nil || event["request_id"] == nil {
				t.Fatalf("missing failure details: %v", event)
			}
		})
	}
}
