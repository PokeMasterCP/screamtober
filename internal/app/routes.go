package app

import (
	"database/sql"
	"embed"
	"html/template"
	"net/http"
	"os"
	"time"

	"github.com/pokemastercp/screamtober/internal/tmdb"
)

//go:embed templates/*.html
var templateFiles embed.FS

// now reports the current time in the household's time zone for calendar dates.
func newHandler(auth *auth, db *sql.DB, now func() time.Time) (http.Handler, error) {
	return newHandlerWithMovieSearch(auth, db, tmdb.New(os.Getenv("TMDB_API_KEY")), now)
}

func newHandlerWithMovieSearch(auth *auth, db *sql.DB, movies movieSearcher, now func() time.Time) (http.Handler, error) {
	pages, err := template.ParseFS(templateFiles, "templates/*.html")
	if err != nil {
		return nil, err
	}

	auth.pages = pages
	mux := http.NewServeMux()
	admin := &adminHandler{db: db, queries: newQueries(db), pages: pages, now: now}
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
	challenges := &challengeHandler{db: db, queries: newQueries(db), auth: auth, pages: pages, now: now}
	mux.HandleFunc("GET /{$}", challenges.home)
	mux.HandleFunc("GET /challenges/{year}", challenges.byYear)
	mux.Handle("POST /challenges/{year}/movies/{id}/rating", auth.requireAuth(http.HandlerFunc(challenges.rate)))
	mux.Handle("POST /challenges/{year}/movies/{id}/rating/delete", auth.requireAuth(http.HandlerFunc(challenges.removeRating)))
	// Unmatched requests, including unsupported methods on known paths, get the
	// 404 page. Logging omits the route for this catch-all pattern.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		eventNotFound(r, "not_found")
		renderNotFound(w, r, pages)
	})
	// Bundled assets are shared by product and admin pages.
	root := http.NewServeMux()
	registerAssetRoutes(root)
	root.Handle("/", auth.restrictAdminSession(mux))
	return protectCrossOrigin(root), nil
}
