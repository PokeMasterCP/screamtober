package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestLogging(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		level  string
		body   string
	}{
		{"implicit OK", 0, "info", "hello"},
		{"empty OK", 0, "info", ""},
		{"redirect", 302, "info", ""},
		{"not found", 404, "warn", "missing"},
		{"server error", 500, "error", "failed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			logger, err := newLogger(&output, "info")
			if err != nil {
				t.Fatal(err)
			}
			handler := requestLogging(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.status != 0 {
					w.WriteHeader(tt.status)
					w.WriteHeader(201) // The first final status must win.
				}
				if tt.body != "" {
					_, _ = w.Write([]byte(tt.body))
				}
			}))
			r := httptest.NewRequest("POST", "/movies?token=query-secret", strings.NewReader("body-secret"))
			r.RemoteAddr = "[2001:db8::1]:4321"
			r.Header.Set("Authorization", "Bearer auth-secret")
			r.Header.Set("Cookie", "session=cookie-secret")
			r.Header.Set("X-Request-ID", "untrusted-id")
			r.Header.Set("CF-Connecting-IP", "192.0.2.1")
			r.Header.Set("X-Forwarded-For", "192.0.2.2")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			var entry map[string]any
			if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
				t.Fatal(err)
			}
			if entry["ip"] != "2001:db8::1" || entry["method"] != "POST" || entry["path"] != "/movies" {
				t.Fatalf("unexpected request fields: %v", entry)
			}
			if entry["status"] != float64(w.Code) || entry["level"] != tt.level || entry["response_bytes"] != float64(len(tt.body)) || entry["aborted"] != false {
				t.Fatalf("unexpected response fields: %v", entry)
			}
			if w.Body.String() != tt.body {
				t.Fatalf("response body changed: %q", w.Body.String())
			}
			if id := w.Header().Get("X-Request-ID"); id == "" || id == "untrusted-id" || entry["request_id"] != id {
				t.Fatalf("invalid request ID: %v", entry)
			}
			if duration, ok := entry["duration_ms"].(float64); !ok || duration < 0 || entry["time"] == nil {
				t.Fatalf("missing timing information: %v", entry)
			}
			for _, secret := range []string{"query-secret", "body-secret", "auth-secret", "cookie-secret", "untrusted-id"} {
				if strings.Contains(output.String(), secret) {
					t.Fatalf("log includes %s", secret)
				}
			}
		})
	}
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

func TestRequestEventAccumulation(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startEvent(r, "user.create", "role", "member")
		eventSucceeded(r, "user_id", 7, "role", "owner")
		startEvent(r, "ignored")
		// A committed change stays successful when its response fails.
		eventFailed(r, "render page", errors.New("template failed"))
		w.WriteHeader(http.StatusInternalServerError)
	})
	_, event := loggedRequest(t, h, httptest.NewRequest("POST", "/", nil))
	for key, want := range map[string]any{
		"message": "http request", "level": "error", "event": "user.create", "outcome": "success",
		"user_id": float64(7), "role": "owner", "step": "render page", "error": "template failed",
	} {
		if event[key] != want {
			t.Fatalf("%s = %v, want %v: %v", key, event[key], want, event)
		}
	}
	// Handlers used without logging middleware record nothing.
	eventSucceeded(httptest.NewRequest("GET", "/", nil))
}

func TestRequestEventRejections(t *testing.T) {
	db := schemaFixture(t)
	h := challengeHTTPFixture(t, db)
	personal, admin := loginCookie(t, h), adminCookie(t, h)
	for _, tt := range []struct {
		name, method, path, body string
		cookie                   *http.Cookie
		status                   int
		level                    string
		want                     map[string]any
	}{
		{"visitor rating", "POST", "/challenges/2026/movies/1/rating", "score=4", nil, 401, "warn",
			map[string]any{"reason": "sign_in_required", "auth": "visitor", "route": "POST /challenges/{year}/movies/{id}/rating"}},
		{"visitor admin page", "GET", "/admin/users", "", nil, 303, "info", map[string]any{"reason": "admin_required"}},
		{"admin product page", "GET", "/", "", admin, 303, "info", map[string]any{"reason": "admin_session_active", "auth": "admin"}},
		{"cross-site form", "POST", "/logout", "", nil, 403, "warn", map[string]any{"reason": "cross_origin"}},
		{"invalid score", "POST", "/challenges/2026/movies/1/rating", "score=6", personal, 400, "warn",
			map[string]any{"event": "rating.save", "reason": "invalid_score", "auth": "personal", "user_id": float64(2), "year": float64(2026), "entry_id": float64(1)}},
		{"missing entry", "POST", "/challenges/2026/movies/99/rating", "score=4", personal, 404, "warn",
			map[string]any{"event": "rating.save", "reason": "not_found", "entry_id": float64(99)}},
		{"household full", "POST", "/admin/users", "display_name=Another&role=owner", admin, 409, "warn",
			map[string]any{"event": "user.create", "reason": "household_full", "auth": "admin"}},
		{"missing profile", "POST", "/admin/users/999/disable", "", admin, 404, "warn",
			map[string]any{"event": "user.disable", "reason": "not_found", "user_id": float64(999)}},
		{"stale calendar", "POST", "/admin/calendar?year=2026", "revision=stale", admin, 409, "warn",
			map[string]any{"event": "calendar.save", "reason": "stale_revision", "year": float64(2026)}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if tt.name == "cross-site form" {
				r.Header.Set("Sec-Fetch-Site", "cross-site")
				r.Header.Set("Origin", "https://attacker.example")
			}
			if tt.cookie != nil {
				r.AddCookie(tt.cookie)
			}
			w, event := loggedRequest(t, h, r)
			if w.Code != tt.status || event["level"] != tt.level || event["outcome"] != "rejected" {
				t.Fatalf("status = %d, event = %v", w.Code, event)
			}
			for key, want := range tt.want {
				if event[key] != want {
					t.Fatalf("%s = %v, want %v: %v", key, event[key], want, event)
				}
			}
		})
	}
}

func TestRequestLoggingAbortedHandler(t *testing.T) {
	var output bytes.Buffer
	logger, _ := newLogger(&output, "info")
	handler := requestLogging(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	func() {
		defer func() {
			if got := recover(); got != http.ErrAbortHandler {
				t.Fatalf("panic not preserved: %v", got)
			}
		}()
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	}()
	var entry map[string]any
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry["aborted"] != true || entry["status"] != float64(0) || entry["level"] != "error" {
		t.Fatalf("unexpected aborted log: %v", entry)
	}
}

func TestLoggedResponseFlush(t *testing.T) {
	recorder := httptest.NewRecorder()
	w := &loggedResponse{ResponseWriter: recorder}
	if err := http.NewResponseController(w).Flush(); err != nil {
		t.Fatal(err)
	}
	w.WriteHeader(http.StatusInternalServerError)
	if !recorder.Flushed || w.status != http.StatusOK || recorder.Code != http.StatusOK {
		t.Fatal("flush did not preserve implicit OK status")
	}
}

func TestTrustedClientIP(t *testing.T) {
	for _, tt := range []struct {
		name, config, peer string
		headers            []string
		want               string
	}{
		{"disabled", "", "10.0.0.2:1234", []string{"192.0.2.1"}, "10.0.0.2"},
		{"trusted IPv4", "true", "10.0.0.2:1234", []string{"192.0.2.1"}, "192.0.2.1"},
		{"trusted IPv6", "true", "[fd00::2]:1234", []string{"2001:db8::1"}, "2001:db8::1"},
		{"disabled explicitly", "false", "10.0.0.3:1234", []string{"192.0.2.1"}, "10.0.0.3"},
		{"missing", "true", "10.0.0.2:1234", nil, "10.0.0.2"},
		{"invalid", "true", "10.0.0.2:1234", []string{"invalid"}, "10.0.0.2"},
		{"list", "true", "10.0.0.2:1234", []string{"192.0.2.1, 192.0.2.2"}, "10.0.0.2"},
		{"duplicate", "true", "10.0.0.2:1234", []string{"192.0.2.1", "192.0.2.2"}, "10.0.0.2"},
		{"port", "true", "10.0.0.2:1234", []string{"192.0.2.1:80"}, "10.0.0.2"},
		{"zone", "true", "10.0.0.2:1234", []string{"fe80::1%eth0"}, "10.0.0.2"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tunnel, err := parseCloudflareTunnel(tt.config)
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			logger, _ := newLogger(&output, "info")
			h := trustedClientIP(tunnel, requestLogging(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})))
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tt.peer
			for _, value := range tt.headers {
				r.Header.Add("CF-Connecting-IP", value)
			}
			r.Header.Set("X-Forwarded-For", "192.0.2.99")
			h.ServeHTTP(httptest.NewRecorder(), r)
			var event map[string]any
			if err := json.Unmarshal(output.Bytes(), &event); err != nil {
				t.Fatal(err)
			}
			if event["ip"] != tt.want {
				t.Fatalf("ip = %v, want %s", event["ip"], tt.want)
			}
		})
	}
}

func TestParseCloudflareTunnel(t *testing.T) {
	for _, value := range []string{"garbage", "enabled", "true,false"} {
		if _, err := parseCloudflareTunnel(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
	for _, tt := range []struct {
		value string
		want  bool
	}{{"", false}, {"false", false}, {"true", true}} {
		got, err := parseCloudflareTunnel(tt.value)
		if err != nil || got != tt.want {
			t.Fatalf("%q = %v, %v", tt.value, got, err)
		}
	}
}
