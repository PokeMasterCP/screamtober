package main

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/pokemastercp/screamtober/internal/store"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

type adminHandler struct {
	db      *sql.DB
	queries *store.Queries
	auth    *auth
	pages   *template.Template
}

type adminUsersPage struct {
	Users       []store.ListManagedUsersRow
	Error       string
	Name        string
	Role        string
	HasOwner    bool
	MemberCount int
}

type issuedTokenPage struct {
	Name  string
	Token string
}

func (h *adminHandler) users(w http.ResponseWriter, r *http.Request) {
	h.showUsers(w, r, http.StatusOK, adminUsersPage{Role: "member"})
}

func (h *adminHandler) showUsers(w http.ResponseWriter, r *http.Request, status int, data adminUsersPage) {
	users, err := h.queries.ListManagedUsers(r.Context())
	if err != nil {
		h.fail(w, r, "list users", err)
		return
	}
	data.Users = users
	for _, user := range users {
		if user.Role == "owner" {
			data.HasOwner = true
		} else {
			data.MemberCount++
		}
	}
	if len(users) == 0 && data.Error == "" {
		data.Role = "owner"
	}
	h.render(w, r, "admin_users.html", status, data)
}

func (h *adminHandler) render(w http.ResponseWriter, r *http.Request, name string, status int, data any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	renderPage(w, r, h.pages, name, status, data)
}

func (h *adminHandler) parseForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		h.showUsers(w, r, http.StatusBadRequest, adminUsersPage{Error: "The form could not be read. Please try again."})
		return false
	}
	return true
}

func validDisplayName(name string) bool {
	return name != "" && utf8.ValidString(name) && utf8.RuneCountInString(name) <= 80
}

func (h *adminHandler) createUser(w http.ResponseWriter, r *http.Request) {
	if !h.parseForm(w, r) {
		return
	}
	name, role := strings.TrimSpace(r.PostForm.Get("display_name")), r.PostForm.Get("role")
	if !validDisplayName(name) || (role != "owner" && role != "member") {
		h.showUsers(w, r, http.StatusBadRequest, adminUsersPage{Error: "Enter a name of 1–80 characters and choose a household role.", Name: name, Role: role})
		return
	}
	token := newLoginToken()
	hash := sha256.Sum256([]byte(token))
	var user store.User
	err := withTransaction(r.Context(), h.db, func(q *store.Queries) error {
		var err error
		user, err = q.CreateUser(r.Context(), store.CreateUserParams{DisplayName: name, Role: role})
		if err != nil {
			return err
		}
		return q.SetUserToken(r.Context(), store.SetUserTokenParams{UserID: user.ID, TokenHash: hash[:]})
	})
	if err != nil {
		var sqliteErr *sqlite.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code()&0xff == sqlite3.SQLITE_CONSTRAINT {
			h.showUsers(w, r, http.StatusConflict, adminUsersPage{Error: "The household can have one owner and three members, including disabled profiles.", Name: name, Role: role})
		} else {
			h.fail(w, r, "create user", err)
		}
		return
	}
	setRequestEvent(r, slog.LevelInfo, "user created", "user_id", user.ID)
	h.render(w, r, "admin_token.html", http.StatusCreated, issuedTokenPage{Name: user.DisplayName, Token: token})
}

func (h *adminHandler) renameUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok || !h.parseForm(w, r) {
		return
	}
	name := strings.TrimSpace(r.PostForm.Get("display_name"))
	if !validDisplayName(name) {
		h.showUsers(w, r, http.StatusBadRequest, adminUsersPage{Error: "Enter a name of 1–80 characters."})
		return
	}
	_, err := h.queries.RenameUser(r.Context(), store.RenameUserParams{ID: id, DisplayName: name})
	if err != nil {
		h.writeFailure(w, r, "rename user", err)
		return
	}
	setRequestEvent(r, slog.LevelInfo, "user renamed", "user_id", id)
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (h *adminHandler) replaceToken(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	token := newLoginToken()
	hash := sha256.Sum256([]byte(token))
	var user store.User
	err := withTransaction(r.Context(), h.db, func(q *store.Queries) error {
		var err error
		user, err = q.EnableUser(r.Context(), id)
		if err != nil {
			return err
		}
		return q.SetUserToken(r.Context(), store.SetUserTokenParams{UserID: id, TokenHash: hash[:]})
	})
	if err != nil {
		h.writeFailure(w, r, "replace user token", err)
		return
	}
	h.auth.revokeUserSessions(id)
	setRequestEvent(r, slog.LevelInfo, "user token replaced", "user_id", id)
	h.render(w, r, "admin_token.html", http.StatusOK, issuedTokenPage{Name: user.DisplayName, Token: token})
}

func (h *adminHandler) disableUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := withTransaction(r.Context(), h.db, func(q *store.Queries) error {
		if _, err := q.DisableUser(r.Context(), id); err != nil {
			return err
		}
		return q.DeleteUserToken(r.Context(), id)
	})
	if err != nil {
		h.writeFailure(w, r, "disable user", err)
		return
	}
	h.auth.revokeUserSessions(id)
	setRequestEvent(r, slog.LevelInfo, "user disabled", "user_id", id)
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (h *adminHandler) writeFailure(w http.ResponseWriter, r *http.Request, operation string, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	h.fail(w, r, operation, err)
}

func (h *adminHandler) fail(w http.ResponseWriter, r *http.Request, operation string, err error) {
	setRequestEvent(r, slog.LevelError, "user management failed", "operation", operation, "error", err)
	http.Error(w, "Unable to manage users. Please try again later.", http.StatusInternalServerError)
}
