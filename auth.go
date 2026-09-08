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
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pokemastercp/screamtober/internal/store"
)

const sessionCookie = "screamtober_session"
const adminSessionCookie = "screamtober_admin_session"
const sessionLifetime = 12 * time.Hour
const adminSessionLifetime = time.Hour
const maxSessions = 128

type userSession struct {
	userID    int64
	tokenHash [32]byte
	expiresAt time.Time
}

type auth struct {
	adminTokenHash [32]byte
	insecureCookie bool
	queries        *store.Queries
	mu             sync.Mutex
	sessions       map[[32]byte]userSession
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
		queries: store.New(db), sessions: make(map[[32]byte]userSession),
		adminSessions: make(map[[32]byte]time.Time),
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
		return nil, nil
	}
	a.mu.Lock()
	session, ok := a.sessions[key]
	if ok && !time.Now().Before(session.expiresAt) {
		delete(a.sessions, key)
		ok = false
	}
	a.mu.Unlock()
	if !ok {
		return nil, nil
	}
	// Checking the credential on every authenticated request makes replacement
	// and disabling effective even for sessions issued before the change.
	user, err := a.queries.GetActiveUserByTokenHash(r.Context(), session.tokenHash[:])
	if errors.Is(err, sql.ErrNoRows) {
		a.mu.Lock()
		delete(a.sessions, key)
		a.mu.Unlock()
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
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
	return ok
}

type authenticatedUserKey struct{}

// The admin cookie is sent on every path so this boundary also covers direct
// navigation and other tabs in the same browser, not just links in the panel.
func (a *auth) restrictAdminSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.adminSignedIn(r) && !strings.HasPrefix(r.URL.Path, "/admin/") {
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
			http.Error(w, "Sign out of admin before using the product.", http.StatusForbidden)
			return
		}
		user, err := a.currentUser(r)
		if err != nil {
			authFailure(w, r, "check user session", err)
			return
		}
		if user == nil {
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

func loginToken(w http.ResponseWriter, r *http.Request, event string) (string, bool) {
	w.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		setRequestEvent(r, slog.LevelWarn, event, "reason", "invalid_form")
		http.Error(w, "Invalid login form", http.StatusBadRequest)
		return "", false
	}
	return r.PostForm.Get("token"), true
}

func (a *auth) login(w http.ResponseWriter, r *http.Request) {
	if a.adminSignedIn(r) {
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "Sign out of admin before signing in again.", http.StatusForbidden)
		return
	}
	token, ok := loginToken(w, r, "login failed")
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
		setRequestEvent(r, slog.LevelWarn, "login failed", "reason", "invalid_token")
		http.Redirect(w, r, "/login?error=invalid", http.StatusSeeOther)
		return
	}
	if err != nil {
		authFailure(w, r, "look up login token", err)
		return
	}
	value := newLoginToken()
	now := time.Now()
	expiry := now.Add(sessionLifetime)
	a.mu.Lock()
	for key, session := range a.sessions {
		if !now.Before(session.expiresAt) {
			delete(a.sessions, key)
		}
	}
	if old, ok := cookieKey(r, sessionCookie); ok {
		delete(a.sessions, old)
	}
	if len(a.sessions) >= maxSessions {
		a.mu.Unlock()
		sessionLimit(w, r, "login failed")
		return
	}
	a.sessions[sha256.Sum256([]byte(value))] = userSession{userID: user.ID, tokenHash: hash, expiresAt: expiry}
	if old, ok := cookieKey(r, adminSessionCookie); ok {
		delete(a.adminSessions, old)
	}
	a.mu.Unlock()
	a.clearAdminCookies(w)
	a.setCookie(w, sessionCookie, "/", value, sessionLifetime, expiry)
	setRequestEvent(r, slog.LevelInfo, "login successful", "user_id", user.ID)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *auth) startAdminSession(w http.ResponseWriter, r *http.Request) {
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
	if len(a.adminSessions) >= maxSessions {
		a.mu.Unlock()
		sessionLimit(w, r, "admin login failed")
		return
	}
	a.adminSessions[sha256.Sum256([]byte(value))] = expiry
	if old, ok := cookieKey(r, sessionCookie); ok {
		delete(a.sessions, old)
	}
	a.mu.Unlock()
	a.setCookie(w, sessionCookie, "/", "", -time.Second, time.Unix(1, 0))
	// Expire the former /admin-scoped cookie when upgrading an existing browser.
	a.setCookie(w, adminSessionCookie, "/admin", "", -time.Second, time.Unix(1, 0))
	a.setCookie(w, adminSessionCookie, "/", value, adminSessionLifetime, expiry)
	setRequestEvent(r, slog.LevelInfo, "admin login successful")
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (a *auth) setCookie(w http.ResponseWriter, name, path, value string, lifetime time.Duration, expiry time.Time) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: path, HttpOnly: true, Secure: !a.insecureCookie, SameSite: http.SameSiteLaxMode, MaxAge: int(lifetime.Seconds()), Expires: expiry})
}

func sessionLimit(w http.ResponseWriter, r *http.Request, event string) {
	setRequestEvent(r, slog.LevelWarn, event, "reason", "session_limit")
	http.Error(w, "Too many active sessions; try again later", http.StatusServiceUnavailable)
}

func authFailure(w http.ResponseWriter, r *http.Request, operation string, err error) {
	setRequestEvent(r, slog.LevelError, "authentication failed", "operation", operation, "error", err)
	http.Error(w, "Unable to sign in. Please try again later.", http.StatusInternalServerError)
}

func (a *auth) revokeUserSessions(userID int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for key, session := range a.sessions {
		if session.userID == userID {
			delete(a.sessions, key)
		}
	}
}

func (a *auth) logout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if key, ok := cookieKey(r, sessionCookie); ok {
		a.mu.Lock()
		delete(a.sessions, key)
		a.mu.Unlock()
	}
	a.setCookie(w, sessionCookie, "/", "", -time.Second, time.Unix(1, 0))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *auth) adminLogout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if key, ok := cookieKey(r, adminSessionCookie); ok {
		a.mu.Lock()
		delete(a.adminSessions, key)
		a.mu.Unlock()
	}
	if key, ok := cookieKey(r, sessionCookie); ok {
		a.mu.Lock()
		delete(a.sessions, key)
		a.mu.Unlock()
	}
	a.clearAdminCookies(w)
	a.setCookie(w, sessionCookie, "/", "", -time.Second, time.Unix(1, 0))
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *auth) clearAdminCookies(w http.ResponseWriter) {
	a.setCookie(w, adminSessionCookie, "/", "", -time.Second, time.Unix(1, 0))
	a.setCookie(w, adminSessionCookie, "/admin", "", -time.Second, time.Unix(1, 0))
}
