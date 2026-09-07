package main

import (
	"bytes"
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"time"
)

//go:embed templates/*.html
var templateFiles embed.FS

func newHandler() (http.Handler, error) {
	pages, err := template.ParseFS(templateFiles, "templates/*.html")
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		data := struct{ Title string }{Title: "Screamtober"}
		var body bytes.Buffer
		if err := pages.ExecuteTemplate(&body, "home.html", data); err != nil {
			slog.ErrorContext(r.Context(), "render home", "error", err)
			http.Error(w, "Unable to load page", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = body.WriteTo(w)
	})
	return mux, nil
}

func main() {
	logger, err := newLogger(os.Stdout, os.Getenv("LOG_LEVEL"))
	if err != nil {
		fallback, _ := newLogger(os.Stdout, "info")
		fallback.Error("invalid logging configuration", "error", err)
		os.Exit(1)
	}
	slog.SetDefault(logger)
	handler, err := newHandler()
	if err != nil {
		logger.Error("load templates", "error", err)
		os.Exit(1)
	}
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	server := &http.Server{
		Addr:              addr,
		Handler:           requestLogging(logger, handler),
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	logger.Info("starting server", "addr", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
