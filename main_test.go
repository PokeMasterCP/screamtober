package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBundledFontsAcrossSessions(t *testing.T) {
	for _, session := range []string{"visitor", "personal", "admin"} {
		t.Run(session, func(t *testing.T) {
			_, handler := authFixture(t, testToken, true)
			var cookie *http.Cookie
			if session == "personal" {
				cookie = loginCookie(t, handler)
			} else if session == "admin" {
				login := authRequest(handler, "POST", "/login", testAdminToken, nil)
				cookie = activeSessionCookie(t, login, adminSessionCookie)
			}
			for _, name := range []string{"Barlow-Regular.ttf", "Barlow-SemiBold.ttf", "BarlowCondensed-Bold.ttf", "BarlowCondensed-ExtraBold.ttf"} {
				w := authRequest(handler, "GET", "/assets/fonts/"+name, "", cookie)
				if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "font/ttf" || !bytes.HasPrefix(w.Body.Bytes(), []byte{0, 1, 0, 0}) {
					t.Fatalf("font %s: status %d, type %q", name, w.Code, w.Header().Get("Content-Type"))
				}
			}
			license := authRequest(handler, "GET", "/assets/fonts/OFL.txt", "", cookie)
			if license.Code != http.StatusOK || !strings.Contains(license.Body.String(), "SIL OPEN FONT LICENSE") {
				t.Fatal("bundled font license is unavailable")
			}
			if session == "admin" {
				w := authRequest(handler, "GET", "/", "", cookie)
				if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin/users" {
					t.Fatal("public fonts must not bypass the admin/product session boundary")
				}
			}
		})
	}
}

func TestNotFoundPage(t *testing.T) {
	for _, session := range []string{"visitor", "personal", "admin"} {
		t.Run(session, func(t *testing.T) {
			_, handler := authFixture(t, testToken, true)
			var cookie *http.Cookie
			if session == "personal" {
				cookie = loginCookie(t, handler)
			} else if session == "admin" {
				login := authRequest(handler, "POST", "/login", testAdminToken, nil)
				cookie = activeSessionCookie(t, login, adminSessionCookie)
			}
			for _, tt := range []struct{ method, path string }{
				{"GET", "/admin/missing"},
				{"GET", "/challenges/2025"},
				{"GET", "/challenges/nonsense"},
				{"POST", "/login/extra"},
				{"PUT", "/login"},
			} {
				w := authRequest(handler, tt.method, tt.path, "", cookie)
				if session == "admin" && !strings.HasPrefix(tt.path, "/admin/") {
					// The admin/product boundary applies before routing.
					if w.Code == http.StatusNotFound {
						t.Fatalf("%s %s: admin session reached a product 404", tt.method, tt.path)
					}
					continue
				}
				body := w.Body.String()
				if w.Code != http.StatusNotFound || w.Header().Get("Content-Type") != "text/html; charset=utf-8" ||
					!strings.Contains(body, "<h1>Page not found</h1>") || !strings.Contains(body, `href="/"`) {
					t.Fatalf("%s %s: status %d, type %q, body %q", tt.method, tt.path, w.Code, w.Header().Get("Content-Type"), body)
				}
			}
		})
	}
}

func TestRoutes(t *testing.T) {
	_, handler := authFixture(t, testToken, false)
	for _, tt := range []struct {
		name, method, path string
		status             int
	}{
		{"home", http.MethodGet, "/", http.StatusOK},
		{"login", http.MethodGet, "/login", http.StatusOK},
		{"credits", http.MethodGet, "/credits", http.StatusOK},
		{"unknown page", http.MethodGet, "/missing", http.StatusNotFound},
		{"removed auth status", http.MethodGet, "/auth/status", http.StatusNotFound},
		{"unsupported method", http.MethodPost, "/", http.StatusNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(tt.method, tt.path, nil))
			if response.Code != tt.status {
				t.Fatalf("status = %d, want %d", response.Code, tt.status)
			}
			if tt.name == "home" {
				body := response.Body.String()
				if got := response.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
					t.Errorf("unexpected Content-Type: %q", got)
				}
				if !strings.Contains(body, `href="/login"`) || !strings.Contains(body, `href="/credits"`) {
					t.Error("public navigation missing")
				}
			}
			if tt.name == "login" && strings.Contains(response.Body.String(), `href="/credits"`) {
				t.Error("login page contains the public footer navigation")
			}
			if tt.name == "login" {
				body := response.Body.String()
				if !strings.Contains(body, `action="/login"`) || !strings.Contains(body, `name="token"`) {
					t.Error("login form missing")
				}
			}
			if tt.name == "credits" {
				body := response.Body.String()
				if !strings.Contains(body, "Movie data") || !strings.Contains(body, "This product uses the TMDB API") {
					t.Error("credits page does not contain the expected attribution")
				}
			}
		})
	}
}
