package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRoutes(t *testing.T) {
	_, handler := authFixture(t, testToken, false)
	for _, tt := range []struct {
		name, method, path string
		status             int
	}{
		{"home", http.MethodGet, "/", http.StatusOK},
		{"unknown page", http.MethodGet, "/missing", http.StatusNotFound},
		{"removed auth status", http.MethodGet, "/auth/status", http.StatusNotFound},
		{"unsupported method", http.MethodPost, "/", http.StatusMethodNotAllowed},
	} {
		t.Run(tt.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(tt.method, tt.path, nil))
			if response.Code != tt.status {
				t.Fatalf("status = %d, want %d", response.Code, tt.status)
			}
			if tt.name == "home" {
				if got := response.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
					t.Errorf("unexpected Content-Type: %q", got)
				}
				if !strings.Contains(response.Body.String(), "<h1>Screamtober</h1>") {
					t.Error("response does not contain the rendered page heading")
				}
			}
		})
	}
}
