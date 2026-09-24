package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pokemastercp/screamtober/internal/store"
)

const sessionCookie = "screamtober_session"
const adminSessionCookie = "screamtober_admin_session"

// Personal sessions are stored in the database so they survive restarts. They
// stay active while used, up to a fixed maximum, and renew at most daily to
// avoid a database write on every request.
const sessionIdleTimeout = 30 * 24 * time.Hour
const sessionMaxLifetime = 90 * 24 * time.Hour
const sessionRenewInterval = 24 * time.Hour
const sessionsPerUser = 10

// Admin sessions are short and kept in memory, so a restart signs admin out.
const adminSessionLifetime = time.Hour
const maxAdminSessions = 128

type auth struct {
	adminTokenHash [32]byte
	insecureCookie bool
	db             *sql.DB
	queries        *store.Queries
	pages          *template.Template
	mu             sync.Mutex
	adminSessions  map[[32]byte]time.Time
}

func newAuth(token string, insecureCookie bool, db *sql.DB) (*auth, error) {
	if token == "" {
		return nil, fmt.Errorf("ADMIN_TOKEN is required")
	}
	if len(token) < 32 {
		return nil, fmt.Errorf("ADMIN_TOKEN must contain at least 32 bytes")
	}
	return &auth{
		adminTokenHash: sha256.Sum256([]byte(token)), insecureCookie: insecureCookie,
		db: db, queries: store.New(db), adminSessions: make(map[[32]byte]time.Time),
	}, nil
}

func newLoginToken() string {
	var token [32]byte
	rand.Read(token[:])
	return base64.RawURLEncoding.EncodeToString(token[:])
}

func cookieKey(r *http.Request, name string) ([32]byte, bool) {
	cookie, err := r.Cookie(name)
	if err != nil || cookie.Value == "" || len(cookie.Value) > 128 {
		return [32]byte{}, false
	}
	return sha256.Sum256([]byte(cookie.Value)), true
}

func (a *auth) currentUser(r *http.Request) (*store.User, error) {
	if a.adminSignedIn(r) {
		return nil, nil
	}
	key, ok := cookieKey(r, sessionCookie)
	if !ok {
		addEventAttrs(r, "auth", "visitor")
		return nil, nil
	}
	now := time.Now()
	// Checking the issuing token on every authenticated request makes replacement
	// and disabling effective even for sessions issued before the change.
	session, err := a.queries.GetUserSession(r.Context(), store.GetUserSessionParams{SessionHash: key[:], Now: now.Unix()})
	if errors.Is(err, sql.ErrNoRows) {
		addEventAttrs(r, "auth", "visitor")
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	expiry := min(now.Add(sessionIdleTimeout).Unix(), session.MaxExpiresAt)
	gain := expiry - session.ExpiresAt
	if gain >= int64(sessionRenewInterval.Seconds()) || (gain > 0 && expiry == session.MaxExpiresAt) {
		if err := a.queries.RenewUserSession(r.Context(), store.RenewUserSessionParams{ExpiresAt: expiry, SessionHash: key[:]}); err != nil {
			return nil, err
		}
	}
	addEventAttrs(r, "auth", "personal", "user_id", session.User.ID)
	return &session.User, nil
}

func (a *auth) adminSignedIn(r *http.Request) bool {
	key, ok := cookieKey(r, adminSessionCookie)
	if !ok {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	expires, ok := a.adminSessions[key]
	if ok && !time.Now().Before(expires) {
		delete(a.adminSessions, key)
		return false
	}
	if ok {
		addEventAttrs(r, "auth", "admin")
	}
	return ok
}

type authenticatedUserKey struct{}

// The admin cookie is sent on every path so this boundary also covers direct
// navigation and other tabs in the same browser, not just links in the panel.
func (a *auth) restrictAdminSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.adminSignedIn(r) && !strings.HasPrefix(r.URL.Path, "/admin/") {
			eventRejected(r, "admin_session_active")
			w.Header().Set("Cache-Control", "no-store")
			if r.Method == http.MethodGet || r.Method == http.MethodHead {
				http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
			} else {
				http.Error(w, "Sign out of admin before using the product.", http.StatusForbidden)
			}
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *auth) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if a.adminSignedIn(r) {
			eventRejected(r, "admin_session_active")
			http.Error(w, "Sign out of admin before using the product.", http.StatusForbidden)
			return
		}
		user, err := a.currentUser(r)
		if err != nil {
			authFailure(w, r, "check user session", err)
			return
		}
		if user == nil {
			eventRejected(r, "sign_in_required")
			http.Error(w, "Sign in required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authenticatedUserKey{}, *user)))
	})
}

func (a *auth) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !a.adminSignedIn(r) {
			eventRejected(r, "admin_required")
			if r.Method == http.MethodGet || r.Method == http.MethodHead {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
			} else {
				http.Error(w, "Administrator sign-in required", http.StatusForbidden)
			}
			return
		}
		next.ServeHTTP(w, r)
	})
}

func loginToken(w http.ResponseWriter, r *http.Request) (string, bool) {
	w.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		eventRejected(r, "invalid_form")
		http.Error(w, "Invalid login form", http.StatusBadRequest)
		return "", false
	}
	return r.PostForm.Get("token"), true
}

func (a *auth) login(w http.ResponseWriter, r *http.Request) {
	startEvent(r, "login")
	if a.adminSignedIn(r) {
		eventRejected(r, "admin_session_active")
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "Sign out of admin before signing in again.", http.StatusForbidden)
		return
	}
	token, ok := loginToken(w, r)
	if !ok {
		return
	}
	hash := sha256.Sum256([]byte(token))
	if subtle.ConstantTimeCompare(hash[:], a.adminTokenHash[:]) == 1 {
		a.startAdminSession(w, r)
		return
	}
	user, err := a.queries.GetActiveUserByTokenHash(r.Context(), hash[:])
	if errors.Is(err, sql.ErrNoRows) {
		// The redirect alone would log at info; failed sign-ins deserve attention.
		recordEvent(r, slog.LevelWarn, "rejected", "reason", "invalid_token")
		http.Redirect(w, r, "/login?error=invalid", http.StatusSeeOther)
		return
	}
	if err != nil {
		authFailure(w, r, "look up login token", err)
		return
	}
	value := newLoginToken()
	key := sha256.Sum256([]byte(value))
	now := time.Now()
	// The cookie lasts for the maximum lifetime; the server enforces idle expiry.
	expiry := now.Add(sessionMaxLifetime)
	err = withTransaction(r.Context(), a.db, func(q *store.Queries) error {
		if err := q.DeleteExpiredUserSessions(r.Context(), now.Unix()); err != nil {
			return err
		}
		if old, ok := cookieKey(r, sessionCookie); ok {
			if err := q.DeleteUserSession(r.Context(), old[:]); err != nil {
				return err
			}
		}
		if err := q.CreateUserSession(r.Context(), store.CreateUserSessionParams{
			SessionHash: key[:], UserID: user.ID, TokenHash: hash[:],
			ExpiresAt: now.Add(sessionIdleTimeout).Unix(), MaxExpiresAt: expiry.Unix(),
		}); err != nil {
			return err
		}
		return q.TrimUserSessions(r.Context(), store.TrimUserSessionsParams{UserID: user.ID, Keep: sessionsPerUser})
	})
	if err != nil {
		authFailure(w, r, "create user session", err)
		return
	}
	a.mu.Lock()
	if old, ok := cookieKey(r, adminSessionCookie); ok {
		delete(a.adminSessions, old)
	}
	a.mu.Unlock()
	a.clearCookie(w, adminSessionCookie)
	a.setCookie(w, sessionCookie, "/", value, sessionMaxLifetime, expiry)
	eventSucceeded(r, "session", "personal", "user_id", user.ID)
	a.loginSuccess(w, r, false)
}

func (a *auth) startAdminSession(w http.ResponseWriter, r *http.Request) {
	if old, ok := cookieKey(r, sessionCookie); ok {
		if err := a.queries.DeleteUserSession(r.Context(), old[:]); err != nil {
			authFailure(w, r, "end user session", err)
			return
		}
	}
	value := newLoginToken()
	now := time.Now()
	expiry := now.Add(adminSessionLifetime)
	a.mu.Lock()
	for key, expires := range a.adminSessions {
		if !now.Before(expires) {
			delete(a.adminSessions, key)
		}
	}
	if old, ok := cookieKey(r, adminSessionCookie); ok {
		delete(a.adminSessions, old)
	}
	if len(a.adminSessions) >= maxAdminSessions {
		a.mu.Unlock()
		eventRejected(r, "session_limit")
		http.Error(w, "Too many active sessions; try again later", http.StatusServiceUnavailable)
		return
	}
	a.adminSessions[sha256.Sum256([]byte(value))] = expiry
	a.mu.Unlock()
	a.clearCookie(w, sessionCookie)
	a.setCookie(w, adminSessionCookie, "/", value, adminSessionLifetime, expiry)
	eventSucceeded(r, "session", "admin")
	a.loginSuccess(w, r, true)
}

func (a *auth) setCookie(w http.ResponseWriter, name, path, value string, lifetime time.Duration, expiry time.Time) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: path, HttpOnly: true, Secure: !a.insecureCookie, SameSite: http.SameSiteLaxMode, MaxAge: int(lifetime.Seconds()), Expires: expiry})
}

func authFailure(w http.ResponseWriter, r *http.Request, step string, err error) {
	eventFailed(r, step, err)
	http.Error(w, "Unable to sign in. Please try again later.", http.StatusInternalServerError)
}

// endUserSession revokes the browser's personal session, reporting any failure.
func (a *auth) endUserSession(w http.ResponseWriter, r *http.Request) bool {
	key, ok := cookieKey(r, sessionCookie)
	if !ok {
		return true
	}
	if err := a.queries.DeleteUserSession(r.Context(), key[:]); err != nil {
		eventFailed(r, "end user session", err)
		http.Error(w, "Unable to sign out. Please try again later.", http.StatusInternalServerError)
		return false
	}
	return true
}

func (a *auth) logout(w http.ResponseWriter, r *http.Request) {
	startEvent(r, "logout")
	w.Header().Set("Cache-Control", "no-store")
	if !a.endUserSession(w, r) {
		return
	}
	eventSucceeded(r)
	a.clearCookie(w, sessionCookie)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *auth) adminLogout(w http.ResponseWriter, r *http.Request) {
	startEvent(r, "admin.logout")
	w.Header().Set("Cache-Control", "no-store")
	if key, ok := cookieKey(r, adminSessionCookie); ok {
		a.mu.Lock()
		delete(a.adminSessions, key)
		a.mu.Unlock()
	}
	if !a.endUserSession(w, r) {
		return
	}
	eventSucceeded(r)
	a.clearCookie(w, adminSessionCookie)
	a.clearCookie(w, sessionCookie)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *auth) clearCookie(w http.ResponseWriter, name string) {
	a.setCookie(w, name, "/", "", -time.Second, time.Unix(1, 0))
}

// A confirmation response completes sign-in without an automatic second request.
func (a *auth) loginSuccess(w http.ResponseWriter, r *http.Request, admin bool) {
	w.Header().Set("Cache-Control", "no-store")
	renderPage(w, r, a.pages, "login_success.html", http.StatusOK, admin)
}
