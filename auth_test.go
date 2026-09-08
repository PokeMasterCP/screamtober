package main

import (
	"bytes"
	"context"
	"crypto/sha256"
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
		name, token, message, level, reason string
	}{
		{"success", testToken, "login successful", "info", ""},
		{"invalid token", "invalid-token", "login failed", "warn", "invalid_token"},
		{"invalid form", strings.Repeat("x", 4097), "login failed", "warn", "invalid_form"},
		{"session limit", testToken, "login failed", "error", "session_limit"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			logger, err := newLogger(&output, "info")
			if err != nil {
				t.Fatal(err)
			}
			a, h := authFixture(t, testToken, false)
			if tt.reason == "session_limit" {
				for i := 0; i < 128; i++ {
					a.sessions[[32]byte{byte(i)}] = userSession{expiresAt: time.Now().Add(time.Hour)}
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
			if event["message"] != tt.message || event["level"] != tt.level || event["time"] == nil {
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
	return a, mux
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
	if w.Code != http.StatusSeeOther {
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
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.MaxAge != 43200 || cookie.Value == testToken {
		t.Fatalf("unexpected session cookie attributes: secure=%v httponly=%v samesite=%v path=%s maxage=%d", cookie.Secure, cookie.HttpOnly, cookie.SameSite, cookie.Path, cookie.MaxAge)
	}
	w := authRequest(h, "GET", "/test/protected", "", cookie)
	if w.Code != 200 || w.Body.String() != "protected content" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("session did not grant access")
	}
	w = authRequest(h, "GET", "/", "", cookie)
	if !strings.Contains(w.Body.String(), "<h2>Signed in</h2>") || !strings.Contains(w.Body.String(), `action="/logout"`) {
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

func TestSessionExpiryRestartAndForgery(t *testing.T) {
	a, h := authFixture(t, testToken, true)
	cookie := loginCookie(t, h)
	if cookie.Secure {
		t.Fatal("local HTTP override ignored")
	}
	_, restarted := authFixture(t, testToken, true)
	if w := authRequest(restarted, "GET", "/test/protected", "", cookie); w.Code != 401 {
		t.Fatal("session survived restart")
	}
	forged := &http.Cookie{Name: sessionCookie, Value: "made-up-session"}
	if w := authRequest(h, "GET", "/test/protected", "", forged); w.Code != 401 {
		t.Fatal("forged cookie accepted")
	}
	a.sessions[sha256.Sum256([]byte(cookie.Value))] = userSession{expiresAt: time.Now().Add(-time.Second)}
	if w := authRequest(h, "GET", "/test/protected", "", cookie); w.Code != 401 {
		t.Fatal("expired session accepted")
	}
	if len(a.sessions) != 0 {
		t.Fatal("expired session not removed")
	}
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
