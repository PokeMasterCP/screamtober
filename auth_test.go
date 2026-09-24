package main

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
	"time"

	"github.com/pokemastercp/screamtober/internal/store"
)

const testAdminToken = "admin-test-only-0123456789abcdef0123456789abcdef"

const testToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestLoginLogging(t *testing.T) {
	for _, tt := range []struct {
		name, token, outcome, level, reason string
	}{
		{"success", testToken, "success", "info", ""},
		{"invalid token", "invalid-token", "rejected", "warn", "invalid_token"},
		{"invalid form", strings.Repeat("x", 4097), "rejected", "warn", "invalid_form"},
		{"admin session limit", testAdminToken, "rejected", "error", "session_limit"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			logger, err := newLogger(&output, "info")
			if err != nil {
				t.Fatal(err)
			}
			a, h := authFixture(t, testToken, false)
			if tt.reason == "session_limit" {
				for i := 0; i < maxAdminSessions; i++ {
					a.adminSessions[[32]byte{byte(i)}] = time.Now().Add(time.Hour)
				}
			}
			r := httptest.NewRequest("POST", "/login", strings.NewReader(url.Values{"token": {tt.token}}.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("X-Request-ID", "untrusted-request-id")
			r.Header.Set("X-Forwarded-For", "192.0.2.1")
			r.Header.Set("CF-Connecting-IP", "192.0.2.2")
			r.RemoteAddr = "[2001:db8::1]:1234"
			w := httptest.NewRecorder()
			requestLogging(logger, h).ServeHTTP(w, r)
			lines := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte("\n"))
			if len(lines) != 1 {
				t.Fatalf("expected one combined login event, got %d", len(lines))
			}
			var event map[string]any
			if err := json.Unmarshal(lines[0], &event); err != nil {
				t.Fatal(err)
			}
			if event["message"] != "http request" || event["event"] != "login" || event["outcome"] != tt.outcome || event["level"] != tt.level || event["time"] == nil {
				t.Fatalf("unexpected event: %v", event)
			}
			if tt.reason == "" {
				if event["user_id"] != float64(1) {
					t.Fatal("successful identity missing")
				}
			} else {
				if event["reason"] != tt.reason || event["user_id"] != nil {
					t.Fatal("failed event must have a reason without claiming an authenticated identity")
				}
			}
			id := w.Header().Get("X-Request-ID")
			if id == "" || id == "untrusted-request-id" || event["request_id"] != id {
				t.Fatal("request correlation failed")
			}
			if event["ip"] != "2001:db8::1" {
				t.Fatal("IP must match the direct peer, ignoring forwarded headers")
			}
			if event["method"] != "POST" || event["path"] != "/login" || event["status"] != float64(w.Code) || event["response_bytes"] != float64(w.Body.Len()) || event["aborted"] != false {
				t.Fatalf("missing request or response details: %v", event)
			}
			if duration, ok := event["duration_ms"].(float64); !ok || duration < 0 {
				t.Fatal("missing duration")
			}

			secrets := []string{testToken, tt.token, "untrusted-request-id"}
			for _, cookie := range w.Result().Cookies() {
				if cookie.Value != "" {
					secrets = append(secrets, cookie.Value)
				}
			}
			for _, secret := range secrets {
				if strings.Contains(output.String(), secret) {
					t.Fatal("logs contain credentials or untrusted request ID")
				}
			}
		})
	}
}

func authFixture(t *testing.T, token string, insecure bool) (*auth, http.Handler) {
	t.Helper()
	db, err := openDatabase(context.Background(), filepath.Join(t.TempDir(), "auth.db"))
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

func TestAuthLifecycle(t *testing.T) {
	_, h := authFixture(t, testToken, false)
	for _, path := range []string{"/", "/login"} {
		if w := authRequest(h, "GET", path, "", nil); w.Code != 200 {
			t.Fatalf("public %s: %d", path, w.Code)
		}
	}
	if w := authRequest(h, "GET", "/test/protected", "", nil); w.Code != 401 {
		t.Fatalf("anonymous access: %d", w.Code)
	}
	if w := authRequest(h, "POST", "/login", "wrong", nil); w.Code != 303 || w.Header().Get("Location") != "/login?error=invalid" || len(w.Result().Cookies()) != 0 {
		t.Fatal("bad token accepted")
	}
	if w := authRequest(h, "GET", "/login?error=invalid", "", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "Invalid login token. Please try again.") {
		t.Fatal("invalid token feedback missing")
	}
	cookie := loginCookie(t, h)
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.MaxAge != 90*24*60*60 || cookie.Value == testToken {
		t.Fatalf("unexpected session cookie attributes: secure=%v httponly=%v samesite=%v path=%s maxage=%d", cookie.Secure, cookie.HttpOnly, cookie.SameSite, cookie.Path, cookie.MaxAge)
	}
	w := authRequest(h, "GET", "/test/protected", "", cookie)
	if w.Code != 200 || w.Body.String() != "protected content" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("session did not grant access")
	}
	w = authRequest(h, "GET", "/", "", cookie)
	if !strings.Contains(w.Body.String(), ">Sign out</button>") || !strings.Contains(w.Body.String(), `action="/logout"`) {
		t.Fatal("signed-in UI missing")
	}
	if !strings.Contains(w.Body.String(), "<title>Screamtober · Signed in</title>") {
		t.Fatal("signed-in page title missing")
	}
	if w := authRequest(h, "GET", "/logout", "", cookie); w.Code != 405 {
		t.Fatal("GET logout allowed")
	}
	if w := authRequest(h, "POST", "/logout", "", cookie); w.Code != 303 || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout did not clear cookie")
	}
	if w := authRequest(h, "GET", "/test/protected", "", cookie); w.Code != 401 {
		t.Fatal("logged-out session remains valid")
	}
	if w := authRequest(h, "GET", "/", "", cookie); !strings.Contains(w.Body.String(), `href="/login"`) || strings.Contains(w.Body.String(), "Signed in") {
		t.Fatal("anonymous UI missing")
	}
}

func TestSessionRestartExpiryAndForgery(t *testing.T) {
	a, h := authFixture(t, testToken, true)
	cookie := loginCookie(t, h)
	if cookie.Secure {
		t.Fatal("local HTTP override ignored")
	}
	restartedAuth, err := newAuth(testAdminToken, true, a.db)
	if err != nil {
		t.Fatal(err)
	}
	restarted := authRoutes(t, restartedAuth, a.db)
	if w := authRequest(restarted, "GET", "/test/protected", "", cookie); w.Code != 200 {
		t.Fatal("personal session did not survive restart")
	}
	var stored int
	if err := a.db.QueryRow(`SELECT count(*) FROM user_sessions WHERE session_hash = ?`, []byte(cookie.Value)).Scan(&stored); err != nil || stored != 0 {
		t.Fatal("raw session value stored")
	}
	forged := &http.Cookie{Name: sessionCookie, Value: "made-up-session"}
	if w := authRequest(h, "GET", "/test/protected", "", forged); w.Code != 401 {
		t.Fatal("forged cookie accepted")
	}
	setSessionExpiry(t, a, cookie, -time.Second, time.Hour)
	if w := authRequest(h, "GET", "/test/protected", "", cookie); w.Code != 401 {
		t.Fatal("expired session accepted")
	}
	if w := authRequest(h, "GET", "/test/protected", "", cookie); w.Code != 401 {
		t.Fatal("expired session was renewed")
	}
}

func TestSessionRenewal(t *testing.T) {
	a, h := authFixture(t, testToken, true)
	cookie := loginCookie(t, h)
	now := time.Now()
	if expires, _ := sessionExpiry(t, a, cookie); expires.Before(now.Add(sessionIdleTimeout - time.Minute)) {
		t.Fatalf("new session expires at %v", expires)
	}

	// Use within the idle timeout extends the session.
	setSessionExpiry(t, a, cookie, time.Hour, sessionMaxLifetime)
	if w := authRequest(h, "GET", "/", "", cookie); !strings.Contains(w.Body.String(), ">Sign out</button>") {
		t.Fatal("signed-in page did not recognize the session")
	}
	if expires, _ := sessionExpiry(t, a, cookie); expires.Before(now.Add(sessionIdleTimeout - time.Minute)) {
		t.Fatalf("used session was not renewed: %v", expires)
	}

	// Renewal never passes the maximum lifetime.
	setSessionExpiry(t, a, cookie, time.Hour, 2*time.Hour)
	if w := authRequest(h, "GET", "/test/protected", "", cookie); w.Code != 200 {
		t.Fatal("session rejected before its maximum lifetime")
	}
	if expires, maxExpires := sessionExpiry(t, a, cookie); !expires.Equal(maxExpires) {
		t.Fatalf("renewal passed the maximum lifetime: %v > %v", expires, maxExpires)
	}
	setSessionExpiry(t, a, cookie, -time.Second, -time.Second)
	if w := authRequest(h, "GET", "/test/protected", "", cookie); w.Code != 401 {
		t.Fatal("session outlived its maximum lifetime")
	}
}

func TestSessionsPerUserLimit(t *testing.T) {
	a, h := authFixture(t, testToken, true)
	first := loginCookie(t, h)
	var latest *http.Cookie
	for range sessionsPerUser {
		latest = loginCookie(t, h)
	}
	var count int
	if err := a.db.QueryRow(`SELECT count(*) FROM user_sessions`).Scan(&count); err != nil || count != sessionsPerUser {
		t.Fatalf("stored sessions = %d, error = %v", count, err)
	}
	if w := authRequest(h, "GET", "/test/protected", "", first); w.Code != 401 {
		t.Fatal("least recently used session was not replaced")
	}
	if w := authRequest(h, "GET", "/test/protected", "", latest); w.Code != 200 {
		t.Fatal("newest session rejected")
	}
}

func setSessionExpiry(t *testing.T, a *auth, cookie *http.Cookie, expires, maxExpires time.Duration) {
	t.Helper()
	hash := sha256.Sum256([]byte(cookie.Value))
	now := time.Now()
	result, err := a.db.Exec(`UPDATE user_sessions SET expires_at = ?, max_expires_at = ? WHERE session_hash = ?`, now.Add(expires).Unix(), now.Add(maxExpires).Unix(), hash[:])
	if n, _ := result.RowsAffected(); err != nil || n != 1 {
		t.Fatalf("update session expiry: %v", err)
	}
}

func sessionExpiry(t *testing.T, a *auth, cookie *http.Cookie) (time.Time, time.Time) {
	t.Helper()
	hash := sha256.Sum256([]byte(cookie.Value))
	var expires, maxExpires int64
	if err := a.db.QueryRow(`SELECT expires_at, max_expires_at FROM user_sessions WHERE session_hash = ?`, hash[:]).Scan(&expires, &maxExpires); err != nil {
		t.Fatal(err)
	}
	return time.Unix(expires, 0), time.Unix(maxExpires, 0)
}

func TestAuthCSRF(t *testing.T) {
	_, h := authFixture(t, testToken, false)
	cookie := loginCookie(t, h)
	for _, path := range []string{"/login", "/logout"} {
		for _, header := range []string{"Origin", "Sec-Fetch-Site"} {
			r := httptest.NewRequest("POST", path, strings.NewReader(url.Values{"token": {testToken}}.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if header == "Origin" {
				r.Header.Set(header, "https://other.example")
			} else {
				r.Header.Set(header, "cross-site")
			}
			r.AddCookie(cookie)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 403 {
				t.Fatalf("cross-origin %s via %s returned %d", path, header, w.Code)
			}
		}
	}
	if w := authRequest(h, "GET", "/test/protected", "", cookie); w.Code != 200 {
		t.Fatal("rejected logout invalidated session")
	}
}

func TestAuthConfigurationAndInput(t *testing.T) {
	if a, err := newAuth("", false, nil); err == nil || err.Error() != "ADMIN_TOKEN is required" || a != nil {
		t.Fatal("missing token must prevent authentication setup with a clear error")
	}
	if _, err := newAuth("short", false, nil); err == nil {
		t.Fatal("short token accepted")
	}
	_, h := authFixture(t, testToken, false)
	if w := authRequest(h, "POST", "/login", strings.Repeat("x", 4097), nil); w.Code != 400 {
		t.Fatal("oversized form accepted")
	}
	if w := authRequest(h, "POST", "/login?token="+testToken, "", nil); w.Header().Get("Location") != "/login?error=invalid" || len(w.Result().Cookies()) != 0 {
		t.Fatal("query token accepted")
	}
}

func TestSuccessfulLoginConfirmation(t *testing.T) {
	for _, tt := range []struct{ name, token, link string }{
		{"personal", testToken, `href="/"`},
		{"admin", testAdminToken, `href="/admin/users"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, h := authFixture(t, testToken, true)
			var output bytes.Buffer
			logger, _ := newLogger(&output, "info")
			w := authRequest(requestLogging(logger, h), "POST", "/login", tt.token, nil)
			if w.Code != http.StatusOK || w.Header().Get("Location") != "" {
				t.Fatalf("login = %d, location = %q", w.Code, w.Header().Get("Location"))
			}
			if !strings.Contains(w.Body.String(), tt.link) || !strings.Contains(w.Body.String(), "Signed in") {
				t.Fatalf("missing confirmation/navigation: %s", w.Body.String())
			}
			if hasMovieSearch := strings.Contains(w.Body.String(), `href="/admin/movies/search"`); hasMovieSearch != (tt.name == "admin") {
				t.Fatal("admin task navigation must appear only for administrator sign-in")
			}
			if w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Header().Get("Content-Type"), "text/html") {
				t.Fatalf("unexpected headers: %v", w.Header())
			}
			var event map[string]any
			if err := json.Unmarshal(output.Bytes(), &event); err != nil {
				t.Fatal(err)
			}
			if event["status"] != float64(200) || event["event"] != "login" || event["outcome"] != "success" || event["session"] != tt.name {
				t.Fatalf("unexpected event: %v", event)
			}
		})
	}
}
