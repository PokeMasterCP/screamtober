package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const sessionCookie = "screamtober_session"
const sessionLifetime = 12 * time.Hour

// This temporary shared identity only distinguishes signed-in visitors. It does
// not assign an owner role or replace the eventual per-user authorization model.
type auth struct {
	tokenHash      [32]byte
	insecureCookie bool
	mu             sync.Mutex
	sessions       map[[32]byte]time.Time
}

func newAuth(token string, insecureCookie bool) (*auth, error) {
	if token == "" {
		return nil, fmt.Errorf("AUTH_TOKEN is required")
	}
	if len(token) < 32 {
		return nil, fmt.Errorf("AUTH_TOKEN must contain at least 32 bytes")
	}
	return &auth{tokenHash: sha256.Sum256([]byte(token)), insecureCookie: insecureCookie, sessions: make(map[[32]byte]time.Time)}, nil
}

func (a *auth) signedIn(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || len(cookie.Value) > 128 {
		return false
	}
	key := sha256.Sum256([]byte(cookie.Value))
	a.mu.Lock()
	defer a.mu.Unlock()
	expiry, ok := a.sessions[key]
	if !ok {
		return false
	}
	if !time.Now().Before(expiry) {
		delete(a.sessions, key)
		return false
	}
	return true
}

func (a *auth) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !a.signedIn(r) {
			http.Error(w, "Sign in required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *auth) login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		requestLogger(r).WarnContext(r.Context(), "user login failed", "reason", "invalid_form")
		http.Error(w, "Invalid login form", http.StatusBadRequest)
		return
	}
	supplied := sha256.Sum256([]byte(r.PostForm.Get("token")))
	if subtle.ConstantTimeCompare(supplied[:], a.tokenHash[:]) != 1 {
		requestLogger(r).WarnContext(r.Context(), "user login failed", "reason", "invalid_token")
		http.Redirect(w, r, "/login?error=invalid", http.StatusSeeOther)
		return
	}
	value := rand.Text()
	now := time.Now()
	expiry := now.Add(sessionLifetime)
	a.mu.Lock()
	for key, expires := range a.sessions {
		if !now.Before(expires) {
			delete(a.sessions, key)
		}
	}
	if old, err := r.Cookie(sessionCookie); err == nil {
		delete(a.sessions, sha256.Sum256([]byte(old.Value)))
	}
	// Bound memory even when the shared credential is used repeatedly.
	if len(a.sessions) >= 128 {
		a.mu.Unlock()
		requestLogger(r).WarnContext(r.Context(), "user login failed", "reason", "session_limit")
		http.Error(w, "Too many active sessions; try again later", http.StatusServiceUnavailable)
		return
	}
	a.sessions[sha256.Sum256([]byte(value))] = expiry
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: value, Path: "/", HttpOnly: true, Secure: !a.insecureCookie, SameSite: http.SameSiteLaxMode, MaxAge: int(sessionLifetime.Seconds()), Expires: expiry})
	requestLogger(r).InfoContext(r.Context(), "user logged in", "username", "shared-user")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *auth) logout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		a.mu.Lock()
		delete(a.sessions, sha256.Sum256([]byte(cookie.Value)))
		a.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", HttpOnly: true, Secure: !a.insecureCookie, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
