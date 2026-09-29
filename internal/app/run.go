package app

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Run configures and serves the application, returning the process exit status.
func Run() int {
	logger, err := newLogger(os.Stdout, os.Getenv("LOG_LEVEL"))
	if err != nil {
		fallback, _ := newLogger(os.Stdout, "info")
		fallback.Error("invalid logging configuration", "error", err)
		return 1
	}
	slog.SetDefault(logger)
	tunnel, err := parseCloudflareTunnel(os.Getenv("CLOUDFLARE_TUNNEL"))
	if err != nil {
		logger.Error("invalid deployment configuration", "error", err)
		return 1
	}
	insecureCookie := false
	if value := os.Getenv("AUTH_INSECURE_COOKIE"); value != "" {
		insecureCookie, err = strconv.ParseBool(value)
		if err != nil {
			logger.Error("AUTH_INSECURE_COOKIE must be a boolean")
			return 1
		}
	}
	path, err := databasePath(os.Getenv("DATABASE_DIR"))
	if err != nil {
		logger.Error("invalid database configuration", "error", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	started := time.Now()
	db, schema, err := openDatabase(ctx, path)
	cancel()
	if err != nil {
		logger.Error("initialize database", "error", err)
		return 1
	}
	defer db.Close()
	logger.Info("database initialized", "path", path, "schema_version", schema.version,
		"migrations_applied", schema.applied, "duration_ms", float64(time.Since(started))/float64(time.Millisecond))
	auth, err := newAuth(os.Getenv("ADMIN_TOKEN"), insecureCookie, db)
	if err != nil {
		logger.Error("invalid authentication configuration", "error", err)
		return 1
	}
	handler, err := newHandler(auth, db)
	if err != nil {
		logger.Error("load templates", "error", err)
		return 1
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
	// Report settings, never secrets. tmdb.New trims the key the same way.
	logger.Info("starting server", "addr", addr, "cloudflare_tunnel", tunnel, "insecure_cookie", insecureCookie,
		"tmdb_configured", strings.TrimSpace(os.Getenv("TMDB_API_KEY")) != "")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server stopped", "error", err)
		return 1
	}
	return 0
}
