package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestOriginMatchesHost(t *testing.T) {
	req := func(origin, host string) *http.Request {
		r := httptest.NewRequest("POST", "/", nil)
		r.Host = host
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		return r
	}
	for _, tt := range []struct {
		origin, host string
		want         bool
	}{
		{"http://192.168.1.50:8080", "192.168.1.50:8080", true},
		{"http://192.168.1.50", "192.168.1.50", true},
		{"http://192.168.1.50:80", "192.168.1.50", true},
		{"http://192.168.1.50", "192.168.1.50:80", true},
		{"http://[::ffff:192.168.1.50]:8080", "192.168.1.50:8080", true},
		{"http://[fd12::1]:8080", "[fd12::1]:8080", true},
		{"http://127.0.0.1:8080", "127.0.0.1:8080", true},
		{"HTTP://192.168.1.50:8080", "192.168.1.50:8080", true},
		{"http://192.168.1.50:8080", "192.168.1.50", false},
		{"http://10.0.0.2:8080", "192.168.1.50:8080", false},
		{"https://evil.example", "192.168.1.50:8080", false},
		{"null", "192.168.1.50:8080", false},
		{"", "192.168.1.50:8080", false},
		{"http://192.168.1.50:8080", "", false},
	} {
		if got := originMatchesHost(req(tt.origin, tt.host)); got != tt.want {
			t.Errorf("origin %q host %q: got %v, want %v", tt.origin, tt.host, got, tt.want)
		}
	}
}

func TestPrivateIPSameOriginPosts(t *testing.T) {
	_, handler := authFixture(t, testToken, true)
	post := func(origin, host, secFetchSite string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://"+host+"/login", strings.NewReader(url.Values{"token": {testToken}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if secFetchSite != "" {
			r.Header.Set("Sec-Fetch-Site", secFetchSite)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := post("http://192.168.1.50:8080", "192.168.1.50:8080", ""); w.Code != http.StatusOK {
		t.Fatalf("matching private-IP origin: %d %s", w.Code, w.Body.String())
	}
	if w := post("http://[::ffff:192.168.1.50]:8080", "192.168.1.50:8080", ""); w.Code != http.StatusOK {
		t.Fatalf("IPv4-mapped origin: %d %s", w.Code, w.Body.String())
	}
	if w := post("http://192.168.1.50:8080", "192.168.1.50:8080", "same-origin"); w.Code != http.StatusOK {
		t.Fatalf("private-IP with Sec-Fetch-Site: %d %s", w.Code, w.Body.String())
	}
	if w := post("null", "192.168.1.50:8080", ""); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "Origin does not match Host") {
		t.Fatalf("null origin: %d %s", w.Code, w.Body.String())
	}
	if w := post("https://evil.example", "192.168.1.50:8080", ""); w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin private-IP: %d", w.Code)
	}
	if w := post("", "192.168.1.50:8080", "cross-site"); w.Code != http.StatusForbidden {
		t.Fatalf("cross-site private-IP: %d", w.Code)
	}
}

func TestHTMLReferrerPolicy(t *testing.T) {
	_, handler := authFixture(t, testToken, true)
	login := authRequest(handler, "GET", "/login", "", nil)
	if login.Code != http.StatusOK || login.Header().Get("Referrer-Policy") != "strict-origin" {
		t.Fatalf("login Referrer-Policy = %q, status %d", login.Header().Get("Referrer-Policy"), login.Code)
	}
	admin := authRequest(handler, "POST", "/login", testAdminToken, nil)
	cookie := activeSessionCookie(t, admin, adminSessionCookie)
	users := authRequest(handler, "GET", "/admin/users", "", cookie)
	if users.Code != http.StatusOK || users.Header().Get("Cache-Control") != "no-store" || users.Header().Get("Referrer-Policy") != "strict-origin" {
		t.Fatalf("admin Referrer-Policy = %q, status %d", users.Header().Get("Referrer-Policy"), users.Code)
	}
}
