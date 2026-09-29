package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pokemastercp/screamtober/internal/store"
)

const testAdminToken = "admin-test-only-0123456789abcdef0123456789abcdef"

const testToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func setTestUserToken(t *testing.T, db *sql.DB, userID int64, token string) {
	t.Helper()
	hash := sha256.Sum256([]byte(token))
	if err := store.New(db).SetUserToken(context.Background(), store.SetUserTokenParams{UserID: userID, TokenHash: hash[:]}); err != nil {
		t.Fatal(err)
	}
}

func portalFixture(t *testing.T, db *sql.DB) (*auth, http.Handler) {
	t.Helper()
	if db == nil {
		var err error
		db, _, err = openDatabase(context.Background(), filepath.Join(t.TempDir(), "portal.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
	}
	a, err := newAuth(testAdminToken, false, db)
	if err != nil {
		t.Fatal(err)
	}
	h, err := newHandler(a, db)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET /test/protected", a.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := r.Context().Value(authenticatedUserKey{}).(store.User)
		_, _ = w.Write([]byte(user.DisplayName))
	})))
	mux.Handle("/", h)
	return a, mux
}

func portalRequest(h http.Handler, method, path string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func adminCookie(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	w := authRequest(h, http.MethodPost, "/login", testAdminToken, nil)
	if w.Code != http.StatusOK || w.Header().Get("Location") != "" {
		t.Fatalf("admin login = %d %s", w.Code, w.Body.String())
	}
	return activeSessionCookie(t, w, adminSessionCookie)
}

func personalCookie(t *testing.T, h http.Handler, token string) *http.Cookie {
	t.Helper()
	w := authRequest(h, http.MethodPost, "/login", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("personal login = %d %s", w.Code, w.Body.String())
	}
	return activeSessionCookie(t, w, sessionCookie)
}

func activeSessionCookie(t *testing.T, w *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	var active *http.Cookie
	for _, cookie := range w.Result().Cookies() {
		if cookie.MaxAge <= 0 {
			continue
		}
		if active != nil || cookie.Name != name {
			t.Fatal("login created unexpected active sessions")
		}
		active = cookie
	}
	if active == nil {
		t.Fatal("login did not issue the expected session")
	}
	return active
}

func authFixture(t *testing.T, token string, insecure bool) (*auth, http.Handler) {
	t.Helper()
	db, _, err := openDatabase(context.Background(), filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	user, err := store.New(db).CreateUser(context.Background(), store.CreateUserParams{DisplayName: "Test user", Role: "member"})
	if err != nil {
		t.Fatal(err)
	}
	setTestUserToken(t, db, user.ID, token)
	a, err := newAuth(testAdminToken, insecure, db)
	if err != nil {
		t.Fatal(err)
	}
	return a, authRoutes(t, a, db)
}

func authRequest(h http.Handler, method, path, token string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(url.Values{"token": {token}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func loginCookie(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	w := authRequest(h, "POST", "/login", testToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("login status: %d", w.Code)
	}
	return activeSessionCookie(t, w, sessionCookie)
}

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

// loggedRequest serves r with request logging and returns its single completion event.
func loggedRequest(t *testing.T, h http.Handler, r *http.Request) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var output bytes.Buffer
	logger, err := newLogger(&output, "info")
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	requestLogging(logger, h).ServeHTTP(w, r)
	var event map[string]any
	// Unmarshal also rejects multiple records for one request.
	if err := json.Unmarshal(output.Bytes(), &event); err != nil {
		t.Fatalf("expected one completion event: %v\n%s", err, output.String())
	}
	return w, event
}

func formRequest(method, path, body string, cookies ...*http.Cookie) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range cookies {
		r.AddCookie(cookie)
	}
	return r
}

// assertEvent checks expected fields; a nil value requires the field to be absent.
func assertEvent(t *testing.T, event map[string]any, want map[string]any) {
	t.Helper()
	for key, value := range want {
		if event[key] != value {
			t.Fatalf("%s = %v, want %v: %v", key, event[key], value, event)
		}
	}
}

func schemaFixture(t *testing.T) *sql.DB {
	t.Helper()
	db, _, err := openDatabase(context.Background(), filepath.Join(t.TempDir(), "schema.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	execSchema(t, db, `
INSERT INTO users (id, display_name, role) VALUES
    (1, 'Owner', 'owner'), (2, 'Member', 'member');
INSERT INTO movies (id, tmdb_id, title) VALUES (1, 123, 'Test movie');
INSERT INTO challenges (id, year) VALUES (1, 2026), (2, 2027);
INSERT INTO challenge_movies (id, challenge_id, movie_id, position) VALUES
    (1, 1, 1, 1), (2, 1, 1, 2), (3, 2, 1, 1);
INSERT INTO ratings (user_id, challenge_movie_id, score) VALUES
    (1, 1, 1), (2, 1, 5), (2, 2, 2), (2, 3, 3);`)
	return db
}

func execSchema(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func authRoutes(t *testing.T, a *auth, db *sql.DB) http.Handler {
	t.Helper()
	h, err := newHandler(a, db)
	if err != nil {
		t.Fatal(err)
	}
	// Exercise authorization using a test-only route, not a production endpoint.
	mux := http.NewServeMux()
	mux.Handle("GET /test/protected", a.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("protected content"))
	})))
	mux.Handle("/", h)
	return mux
}
