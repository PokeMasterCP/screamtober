package main

import (
	"bytes"
	"context"
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"
)

//go:embed templates/*.html
var templateFiles embed.FS

func newHandler(auth *auth) (http.Handler, error) {
	pages, err := template.ParseFS(templateFiles, "templates/*.html")
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		data := struct{ InvalidToken bool }{InvalidToken: r.URL.Query().Get("error") == "invalid"}
		if err := pages.ExecuteTemplate(w, "login.html", data); err != nil {
			slog.ErrorContext(r.Context(), "render login", "error", err)
		}
	})
	mux.HandleFunc("POST /login", auth.login)
	mux.HandleFunc("POST /logout", auth.logout)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		data := struct {
			Title    string
			SignedIn bool
		}{Title: "Screamtober", SignedIn: auth.signedIn(r)}
		var body bytes.Buffer
		if err := pages.ExecuteTemplate(&body, "home.html", data); err != nil {
			slog.ErrorContext(r.Context(), "render home", "error", err)
			http.Error(w, "Unable to load page", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = body.WriteTo(w)
	})
	return http.NewCrossOriginProtection().Handler(mux), nil
}

func main() {
	logger, err := newLogger(os.Stdout, os.Getenv("LOG_LEVEL"))
	if err != nil {
		fallback, _ := newLogger(os.Stdout, "info")
		fallback.Error("invalid logging configuration", "error", err)
		os.Exit(1)
	}
	slog.SetDefault(logger)
	insecureCookie := false
	if value := os.Getenv("AUTH_INSECURE_COOKIE"); value != "" {
		insecureCookie, err = strconv.ParseBool(value)
		if err != nil {
			logger.Error("AUTH_INSECURE_COOKIE must be a boolean")
			os.Exit(1)
		}
	}
	auth, err := newAuth(os.Getenv("AUTH_TOKEN"), insecureCookie)
	if err != nil {
		logger.Error("invalid authentication configuration", "error", err)
		os.Exit(1)
	}
	handler, err := newHandler(auth)
	if err != nil {
		logger.Error("load templates", "error", err)
		os.Exit(1)
	}
	path, err := databasePath(os.Getenv("DATABASE_PATH"), os.Getenv("RAILWAY_VOLUME_MOUNT_PATH"), os.Getenv("RAILWAY_PROJECT_ID") != "")
	if err != nil {
		logger.Error("invalid database configuration", "error", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	db, err := openDatabase(ctx, path)
	cancel()
	if err != nil {
		logger.Error("initialize database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	logger.Info("database initialized", "path", path)
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
		db.Close()
		os.Exit(1)
	}
}
