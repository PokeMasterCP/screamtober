package main

import (
	"html/template"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRenderPageDoesNotCommitPartialTemplate(t *testing.T) {
	pages := template.Must(template.New("broken").Parse(`private prefix {{.Missing}}`))
	w := httptest.NewRecorder()
	renderPage(w, httptest.NewRequest("GET", "/login", nil), pages, "broken", 201, struct{}{})
	if w.Code != 500 || strings.Contains(w.Body.String(), "private prefix") {
		t.Fatalf("partial template response: %d %s", w.Code, w.Body.String())
	}
}
