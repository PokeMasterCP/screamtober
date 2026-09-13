package main

import (
	"bytes"
	"html/template"
	"log/slog"
	"net/http"
)

// Buffer before committing the status so template failures cannot return partial HTML.
func renderPage(w http.ResponseWriter, r *http.Request, pages *template.Template, name string, status int, data any) {
	var body bytes.Buffer
	if err := pages.ExecuteTemplate(&body, name, data); err != nil {
		setRequestEvent(r, slog.LevelError, "render page failed", "template", name, "error", err)
		http.Error(w, "Unable to load page. Please try again later.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// no-referrer makes some browsers send Origin: null, which fails CSRF checks
	// on HTTP private-IP origins where Sec-Fetch-Site is also omitted.
	w.Header().Set("Referrer-Policy", "strict-origin")
	w.WriteHeader(status)
	_, _ = body.WriteTo(w)
}
