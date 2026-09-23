package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
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
		{"/challenges/2027", 200, []string{"2027 movie challenge", "1 of 31 movies selected · 0 watched", "Household rating: 3.0 / 5 (1 rating)"}},
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
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/challenges/2027", nil))
	if !strings.Contains(w.Body.String(), "Household rating: 5.0 / 5") || strings.Contains(w.Body.String(), "Owner: 1 / 5") {
		t.Fatal("page has stale data or includes ratings from another year")
	}
}

func TestChallengeEmptyStates(t *testing.T) {
	_, empty := authFixture(t, testToken, true)
	w := httptest.NewRecorder()
	empty.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "The lineup is still in the making.") {
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
	if w.Code != 200 || strings.Count(w.Body.String(), "No ratings yet.") != 1 || !strings.Contains(w.Body.String(), "Nobody has rated this one yet.") || strings.Contains(w.Body.String(), "Household rating:") {
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
	if login.Code != http.StatusOK {
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
		// Personal sessions cannot write directly to challenge pages.
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

func TestChallengePosterURLs(t *testing.T) {
	db := schemaFixture(t)
	h := challengeHTTPFixture(t, db)
	cookie := loginCookie(t, h)
	for _, tt := range []struct {
		name string
		path any
		want string
	}{
		{"cached poster", "/poster-123.jpg", "https://image.tmdb.org/t/p/w500/poster-123.jpg"},
		{"missing poster", nil, ""},
		{"empty poster", "", ""},
		{"absolute URL", "https://evil.example/poster.jpg", ""},
		{"protocol relative URL", "//evil.example/poster.jpg", ""},
		{"path traversal", "/../poster.jpg", ""},
		{"attribute injection", `/poster.jpg" onerror="alert(1)`, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			execSchema(t, db, `UPDATE movies SET poster_path = ?`, tt.path)
			for _, session := range []*http.Cookie{nil, cookie} {
				w := authRequest(h, "GET", "/challenges/2026", "", session)
				body := w.Body.String()
				if w.Code != http.StatusOK {
					t.Fatalf("challenge = %d", w.Code)
				}
				if tt.want != "" {
					// Each repeated challenge entry has its own poster and calendar
					// thumbnail; the top-rated entry adds one more thumbnail.
					if count := strings.Count(body, `src="`+tt.want+`"`); count != 2 {
						t.Errorf("poster count = %d, want 2", count)
					}
					thumb := strings.Replace(tt.want, "/w500/", "/w185/", 1)
					if count := strings.Count(body, `src="`+thumb+`"`); count != 3 {
						t.Errorf("thumbnail count = %d, want 3", count)
					}
				} else if strings.Contains(body, `src="https://image.tmdb.org/`) {
					t.Error("missing or invalid path produced a poster URL")
				}
				if strings.Contains(body, "evil.example") || strings.Contains(body, "onerror=") || strings.Contains(body, "ZgotmplZ") {
					t.Error("unsafe poster metadata reached the page")
				}
			}
		})
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
			requestLogging(logger, h).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/challenges/2026", nil))
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

func TestHomeCurrentYearAndNextMovie(t *testing.T) {
	for _, scenario := range []struct {
		name, setup, want, absent string
	}{
		{"missing year", "", "The lineup is still in the making.", `class="feature-layout"`},
		{"empty year", "INSERT INTO challenges (id, year) VALUES (3, ?)", "The lineup is still in the making.", `class="feature-layout"`},
		{"scheduled before unscheduled", "", `class="feature-layout" id="movie-5"`, "You’re all caught up."},
		{"skip watched", "UPDATE challenge_movies SET watched_at = CURRENT_TIMESTAMP WHERE id = 5", `class="feature-layout" id="movie-6"`, "You’re all caught up."},
		{"unscheduled next", "UPDATE challenge_movies SET watched_at = CURRENT_TIMESTAMP WHERE id IN (5,6)", `class="feature-layout" id="movie-4"`, "You’re all caught up."},
		{"all watched", "UPDATE challenge_movies SET watched_at = CURRENT_TIMESTAMP WHERE challenge_id = 3", "You’re all caught up.", `class="feature-layout"`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			db := schemaFixture(t)
			year := time.Now().Year()
			execSchema(t, db, "UPDATE challenges SET year = year + 100")
			execSchema(t, db, "UPDATE challenges SET year = ? WHERE id = 1", year-1)
			execSchema(t, db, "UPDATE challenges SET year = ? WHERE id = 2", year+1)
			if scenario.name == "empty year" {
				execSchema(t, db, scenario.setup, year)
			} else if scenario.name != "missing year" {
				execSchema(t, db, "INSERT INTO challenges (id, year) VALUES (3, ?)", year)
				execSchema(t, db, `INSERT INTO movies (id, tmdb_id, title, overview) VALUES (2, 456, '<b>Current movie</b>', '<em>Current overview</em>');
INSERT INTO challenge_movies (id, challenge_id, movie_id, position) VALUES (4,3,2,NULL), (5,3,2,2), (6,3,2,7)`)
				if scenario.setup != "" {
					execSchema(t, db, scenario.setup)
				}
			}
			h := challengeHTTPFixture(t, db)
			for _, signedIn := range []bool{false, true} {
				var cookie *http.Cookie
				if signedIn {
					cookie = loginCookie(t, h)
				}
				w := authRequest(h, "GET", "/", "", cookie)
				body := w.Body.String()
				for _, want := range []string{fmt.Sprintf("%d movie challenge", year), scenario.want, fmt.Sprintf(`href="/challenges/%d"`, year-1), fmt.Sprintf(`href="/challenges/%d"`, year+1)} {
					if w.Code != 200 || !strings.Contains(body, want) {
						t.Fatalf("home missing %q: %d %s", want, w.Code, body)
					}
				}
				for _, absent := range []string{scenario.absent, "Test movie", "<b>Current movie</b>", "<em>Current overview</em>"} {
					if strings.Contains(body, absent) {
						t.Fatalf("home unexpectedly contains %q", absent)
					}
				}
				if strings.Contains(scenario.want, "feature-layout") && !strings.Contains(body, "&lt;b&gt;Current movie&lt;/b&gt;") {
					t.Fatal("missing escaped next movie")
				}
				if scenario.name != "missing year" && scenario.name != "empty year" {
					for _, id := range []int{4, 5, 6} {
						if count := strings.Count(body, fmt.Sprintf(`id="movie-%d"`, id)); count != 1 {
							t.Errorf("entry %d rendered %d times, want once across feature and carousel", id, count)
						}
						form := fmt.Sprintf(`action="/challenges/%d/movies/%d/rating"`, year, id)
						wantForms := 0
						if signedIn {
							wantForms = 1
						}
						if count := strings.Count(body, form); count != wantForms {
							t.Errorf("entry %d has %d rating forms, want %d", id, count, wantForms)
						}
					}
					wantCards := 2
					if scenario.name == "all watched" {
						wantCards = 3
					}
					if count := strings.Count(body, `<article id="movie-`); count != wantCards {
						t.Errorf("carousel contains %d entries, want %d", count, wantCards)
					}
				}
			}
			var count int
			if err := db.QueryRow("SELECT count(*) FROM challenges").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if scenario.name == "missing year" && count != 2 {
				t.Fatal("home created a challenge")
			}
		})
	}
}

func TestChallengeSingleFeaturedMovie(t *testing.T) {
	db := schemaFixture(t)
	h := challengeHTTPFixture(t, db)
	for _, cookie := range []*http.Cookie{nil, loginCookie(t, h)} {
		w := authRequest(h, "GET", "/challenges/2027", "", cookie)
		body := w.Body.String()
		if w.Code != http.StatusOK || !strings.Contains(body, `id="movie-3"`) || !strings.Contains(body, "Your only pick is featured above.") {
			t.Fatal("single entry must remain accessible in the feature")
		}
		if strings.Contains(body, `id="challenge-carousel"`) || strings.Contains(body, "No movies selected for this year yet.") {
			t.Fatal("single featured entry should not create an empty carousel or missing-movies message")
		}
	}
}

func TestCarouselPosterCards(t *testing.T) {
	db := schemaFixture(t)
	execSchema(t, db, `INSERT INTO movies(id,tmdb_id,title,overview,poster_path) VALUES(2,456,'<b>Carousel movie</b>','Carousel-only description','/carousel.jpg'); UPDATE challenge_movies SET movie_id=2 WHERE id=2`)
	h := challengeHTTPFixture(t, db)
	for _, cookie := range []*http.Cookie{nil, loginCookie(t, h)} {
		body := authRequest(h, "GET", "/challenges/2026", "", cookie).Body.String()
		start := strings.Index(body, `<article id="movie-2"`)
		if start < 0 {
			t.Fatal("carousel entry missing")
		}
		end := strings.Index(body[start:], "</article>")
		if end < 0 {
			t.Fatal("incomplete carousel card")
		}
		card := body[start : start+end]
		if !strings.Contains(card, `aria-label="&lt;b&gt;Carousel movie&lt;/b&gt;"`) || !strings.Contains(card, `src="https://image.tmdb.org/t/p/w500/carousel.jpg"`) {
			t.Fatal("poster or escaped accessible name missing")
		}
		if strings.Contains(card, "Carousel-only description") || strings.Contains(card, "<h3") {
			t.Fatal("carousel must retain its poster-only presentation")
		}
		if strings.Contains(card, `action="/challenges/2026/movies/2/rating"`) != (cookie != nil) {
			t.Fatal("wrong per-entry rating controls")
		}
	}
}

func TestChallengeNights(t *testing.T) {
	var scheduled, unscheduled challengeMovieView
	scheduled.ID, scheduled.Position = 7, sql.NullInt64{Int64: 3, Valid: true}
	unscheduled.ID = 8
	nights := challengePage{Year: 2026, Movies: []challengeMovieView{unscheduled, scheduled}}.Nights()
	if len(nights) != 31 || nights[0].Day != 1 || nights[30].Day != 31 {
		t.Fatalf("nights = %d, want October 1–31", len(nights))
	}
	if nights[0].Weekday != "Thu" || nights[30].Weekday != "Sat" {
		t.Fatalf("weekdays = %s…%s, want the challenge year's calendar", nights[0].Weekday, nights[30].Weekday)
	}
	for _, night := range nights {
		if want := night.Day == 3; (night.Movie != nil) != want || (want && night.Movie.ID != 7) {
			t.Fatalf("night %d has wrong movie", night.Day)
		}
	}
}

func TestChallengeHouseholdSummary(t *testing.T) {
	db := schemaFixture(t)
	// The member has watched entry 2 but has not rated it; entry 1 keeps both votes.
	execSchema(t, db, `UPDATE challenge_movies SET watched_at = CURRENT_TIMESTAMP WHERE id IN (1, 2); DELETE FROM ratings WHERE user_id = 2 AND challenge_movie_id = 2`)
	h := challengeHTTPFixture(t, db)
	for _, cookie := range []*http.Cookie{nil, loginCookie(t, h)} {
		body := authRequest(h, "GET", "/challenges/2026", "", cookie).Body.String()
		// Year totals average only submitted ratings; 2027 votes stay out of 2026.
		for _, want := range []string{`<b>3.0</b><span>★ · 2 ratings`, `class="top-pick" href="#movie-1"`, `Member</span><span class="critic-stats">1 rated · <b>5.0</b>`, `Owner</span><span class="critic-stats">1 rated · <b>1.0</b>`} {
			if !strings.Contains(body, want) {
				t.Fatalf("summary missing %q", want)
			}
		}
		if got := strings.Contains(body, `Your turn.`) && strings.Contains(body, `<li><a href="#movie-2">`); got != (cookie != nil) {
			t.Fatalf("unrated reminder shown = %v for signed in = %v", got, cookie != nil)
		}
	}
}

func TestChallengeCalendarDates(t *testing.T) {
	for _, tt := range []struct {
		now  time.Time
		days int
	}{
		{time.Date(2026, time.September, 23, 23, 30, 0, 0, time.Local), 8},
		{time.Date(2026, time.September, 30, 0, 0, 0, 0, time.Local), 1},
		{time.Date(2026, time.October, 1, 0, 0, 0, 0, time.Local), 0},
		{time.Date(2026, time.December, 31, 0, 0, 0, 0, time.Local), 0},
		{time.Date(2026, time.March, 1, 0, 0, 0, 0, time.Local), 214},
	} {
		if got := daysUntilOctober(tt.now); got != tt.days {
			t.Errorf("daysUntilOctober(%s) = %d, want %d", tt.now.Format(time.DateOnly), got, tt.days)
		}
	}
	// October 1 fell on a Wednesday in 2025 and a Thursday in 2026.
	for year, column := range map[int64]int{2025: 4, 2026: 5} {
		if got := (challengePage{Year: year}).FirstColumn(); got != column {
			t.Errorf("%d first column = %d, want %d", year, got, column)
		}
	}
	entry := challengeMovieView{}
	entry.Position = sql.NullInt64{Int64: 5, Valid: true}
	if got := (challengePage{Year: 2026}).NightLabel(entry); got != "Monday, October 5" {
		t.Errorf("night label = %q", got)
	}
}
