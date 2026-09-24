package main

import (
	"context"
	"database/sql"
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/pokemastercp/screamtober/internal/tmdb"
)

//go:embed templates/*.html
var templateFiles embed.FS

func newHandler(auth *auth, db *sql.DB) (http.Handler, error) {
	return newHandlerWithMovieSearch(auth, db, tmdb.New(os.Getenv("TMDB_API_KEY")))
}

func newHandlerWithMovieSearch(auth *auth, db *sql.DB, movies movieSearcher) (http.Handler, error) {
	pages, err := template.ParseFS(templateFiles, "templates/*.html")
	if err != nil {
		return nil, err
	}

	auth.pages = pages
	mux := http.NewServeMux()
	admin := &adminHandler{db: db, queries: newQueries(db), pages: pages}
	mux.Handle("GET /admin/calendar", auth.requireAdmin(http.HandlerFunc(admin.calendar)))
	mux.Handle("POST /admin/calendar", auth.requireAdmin(http.HandlerFunc(admin.saveCalendar)))
	search := &movieSearchHandler{admin: admin, movies: movies}
	mux.Handle("POST /admin/movies", auth.requireAdmin(http.HandlerFunc(search.add)))
	mux.Handle("GET /admin/movies/search", auth.requireAdmin(http.HandlerFunc(search.search)))
	mux.Handle("POST /admin/challenges/{year}/movies/{id}/service", auth.requireAdmin(http.HandlerFunc(search.updateService)))
	mux.Handle("POST /admin/challenges/{year}/movies/{id}/delete", auth.requireAdmin(http.HandlerFunc(search.delete)))
	mux.HandleFunc("GET /admin/login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})
	mux.HandleFunc("POST /admin/logout", auth.adminLogout)
	mux.Handle("GET /admin/users", auth.requireAdmin(http.HandlerFunc(admin.users)))
	mux.Handle("POST /admin/users", auth.requireAdmin(http.HandlerFunc(admin.createUser)))
	mux.Handle("POST /admin/users/{id}/rename", auth.requireAdmin(http.HandlerFunc(admin.renameUser)))
	mux.Handle("POST /admin/users/{id}/token", auth.requireAdmin(http.HandlerFunc(admin.replaceToken)))
	mux.Handle("POST /admin/users/{id}/disable", auth.requireAdmin(http.HandlerFunc(admin.disableUser)))
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		data := struct{ InvalidToken bool }{InvalidToken: r.URL.Query().Get("error") == "invalid"}
		renderPage(w, r, pages, "login.html", http.StatusOK, data)
	})
	mux.HandleFunc("POST /login", auth.login)
	mux.HandleFunc("POST /logout", auth.logout)
	mux.HandleFunc("GET /credits", func(w http.ResponseWriter, r *http.Request) {
		renderPage(w, r, pages, "credits.html", http.StatusOK, nil)
	})
	challenges := &challengeHandler{db: db, queries: newQueries(db), auth: auth, pages: pages}
	mux.HandleFunc("GET /{$}", challenges.home)
	mux.HandleFunc("GET /challenges/{year}", challenges.byYear)
	mux.Handle("POST /challenges/{year}/movies/{id}/rating", auth.requireAuth(http.HandlerFunc(challenges.rate)))
	// Bundled assets are shared by product and admin pages.
	root := http.NewServeMux()
	registerAssetRoutes(root)
	root.Handle("/", auth.restrictAdminSession(mux))
	return protectCrossOrigin(root), nil
}

func main() {
	logger, err := newLogger(os.Stdout, os.Getenv("LOG_LEVEL"))
	if err != nil {
		fallback, _ := newLogger(os.Stdout, "info")
		fallback.Error("invalid logging configuration", "error", err)
		os.Exit(1)
	}
	slog.SetDefault(logger)
	tunnel, err := parseCloudflareTunnel(os.Getenv("CLOUDFLARE_TUNNEL"))
	if err != nil {
		logger.Error("invalid deployment configuration", "error", err)
		os.Exit(1)
	}
	insecureCookie := false
	if value := os.Getenv("AUTH_INSECURE_COOKIE"); value != "" {
		insecureCookie, err = strconv.ParseBool(value)
		if err != nil {
			logger.Error("AUTH_INSECURE_COOKIE must be a boolean")
			os.Exit(1)
		}
	}
	path, err := databasePath(os.Getenv("DATABASE_DIR"))
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
	auth, err := newAuth(os.Getenv("ADMIN_TOKEN"), insecureCookie, db)
	if err != nil {
		logger.Error("invalid authentication configuration", "error", err)
		db.Close()
		os.Exit(1)
	}
	handler, err := newHandler(auth, db)
	if err != nil {
		logger.Error("load templates", "error", err)
		db.Close()
		os.Exit(1)
	}
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	server := &http.Server{
		Addr:              addr,
		Handler:           trustedClientIP(tunnel, requestLogging(logger, handler)),
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
