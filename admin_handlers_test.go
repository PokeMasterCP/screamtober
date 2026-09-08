package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pokemastercp/screamtober/internal/store"
)

func setTestUserToken(t *testing.T, db *sql.DB, userID int64, token string) {
	t.Helper()
	hash := sha256.Sum256([]byte(token))
	if err := store.New(db).SetUserToken(context.Background(), store.SetUserTokenParams{UserID: userID, TokenHash: hash[:]}); err != nil {
		t.Fatal(err)
	}
}

func portalFixture(t *testing.T, db *sql.DB) (*auth, http.Handler) {
	t.Helper()
	if db == nil {
		var err error
		db, err = openDatabase(context.Background(), filepath.Join(t.TempDir(), "portal.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
	}
	a, err := newAuth(testAdminToken, false, db)
	if err != nil {
		t.Fatal(err)
	}
	h, err := newHandler(a, db)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET /test/protected", a.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := r.Context().Value(authenticatedUserKey{}).(store.User)
		_, _ = w.Write([]byte(user.DisplayName))
	})))
	mux.Handle("/", h)
	return a, mux
}

func portalRequest(h http.Handler, method, path string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func adminCookie(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	w := authRequest(h, http.MethodPost, "/login", testAdminToken, nil)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin/users" {
		t.Fatalf("admin login = %d %s", w.Code, w.Body.String())
	}
	return activeSessionCookie(t, w, adminSessionCookie)
}

func personalCookie(t *testing.T, h http.Handler, token string) *http.Cookie {
	t.Helper()
	w := authRequest(h, http.MethodPost, "/login", token, nil)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("personal login = %d %s", w.Code, w.Body.String())
	}
	return activeSessionCookie(t, w, sessionCookie)
}

func issuedToken(t *testing.T, w *httptest.ResponseRecorder, status int) string {
	t.Helper()
	if w.Code != status {
		t.Fatalf("issue token = %d %s", w.Code, w.Body.String())
	}
	match := regexp.MustCompile(`id="new-token"[^>]*value="([^"]+)"`).FindStringSubmatch(w.Body.String())
	if len(match) != 2 {
		t.Fatal("missing one-time token")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(match[1])
	if err != nil || len(decoded) != 32 {
		t.Fatal("token does not contain 256 random bits")
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("credential response missing privacy headers")
	}
	return match[1]
}

func TestAdminOnboardingAndPermissionSeparation(t *testing.T) {
	a, h := portalFixture(t, nil)
	admin := adminCookie(t, h)
	if admin.Name != adminSessionCookie || admin.Path != "/" || !admin.Secure || !admin.HttpOnly || admin.SameSite != http.SameSiteLaxMode || admin.MaxAge != 3600 {
		t.Fatal("unexpected admin cookie attributes")
	}
	created := portalRequest(h, "POST", "/admin/users", url.Values{"display_name": {"Owner"}, "role": {"owner"}}, admin)
	token := issuedToken(t, created, http.StatusCreated)
	hash := sha256.Sum256([]byte(token))
	user, err := a.queries.GetActiveUserByTokenHash(context.Background(), hash[:])
	if err != nil || user.Role != "owner" {
		t.Fatalf("stored token lookup = %+v %v", user, err)
	}
	for _, path := range []string{"/", "/challenges/2026", "/login"} {
		if w := portalRequest(h, "GET", path, nil, admin); w.Code != 303 || w.Header().Get("Location") != "/admin/users" {
			t.Fatalf("admin escaped into %s: %d", path, w.Code)
		}
	}
	for _, path := range []string{"/login", "/logout"} {
		if w := authRequest(h, "POST", path, token, admin); w.Code != 403 || len(w.Result().Cookies()) != 0 {
			t.Fatal("admin switched sessions without signing out")
		}
	}
	if w := portalRequest(h, "GET", "/test/protected", nil, admin); w.Code != 403 {
		t.Fatal("admin became a rating identity")
	}
	if w := portalRequest(h, "GET", "/admin/users", nil, admin); w.Code != 200 || strings.Contains(w.Body.String(), token) {
		t.Fatal("portal failed or redisplayed token")
	}
	if w := portalRequest(h, "POST", "/admin/logout", nil, admin); w.Code != 303 || w.Header().Get("Location") != "/login" {
		t.Fatal("admin logout did not return to unified login")
	}
	personal := personalCookie(t, h, token)
	if personal.Name == admin.Name {
		t.Fatal("session namespaces overlap")
	}
	if w := portalRequest(h, "GET", "/test/protected", nil, personal); w.Code != 200 || w.Body.String() != "Owner" {
		t.Fatal("personal identity missing")
	}
	if w := portalRequest(h, "GET", "/admin/users", nil, personal); w.Code != 303 || w.Header().Get("Location") != "/login" {
		t.Fatal("owner's personal session grants admin access")
	}

	// Entering admin mode must revoke the existing personal session server-side.
	promotion := authRequest(h, "POST", "/login", testAdminToken, personal)
	admin = activeSessionCookie(t, promotion, adminSessionCookie)
	if promotion.Header().Get("Location") != "/admin/users" {
		t.Fatal("admin token did not select admin mode")
	}
	if w := portalRequest(h, "GET", "/test/protected", nil, personal); w.Code != 401 {
		t.Fatal("previous personal session survived admin sign-in")
	}
	if w := portalRequest(h, "GET", "/test/protected", nil, personal, admin); w.Code != 403 {
		t.Fatal("mixed cookies granted product access")
	}
	a.adminSessions[sha256.Sum256([]byte(admin.Value))] = time.Now().Add(-time.Second)
	if w := portalRequest(h, "GET", "/admin/users", nil, admin); w.Code != 303 || w.Header().Get("Location") != "/login" {
		t.Fatal("expired admin session accepted")
	}
	if w := portalRequest(h, "GET", "/test/protected", nil, personal); w.Code != 401 {
		t.Fatal("admin expiration revived the personal session")
	}
	if w := portalRequest(h, "GET", "/admin/login", nil); w.Code != 303 || w.Header().Get("Location") != "/login" {
		t.Fatal("legacy login link does not redirect")
	}
	if w := authRequest(h, "POST", "/admin/login", token, nil); w.Code != 405 {
		t.Fatal("legacy login endpoint still accepts credentials")
	}
}

func TestAdminRoutesRequireAdminAndSameOrigin(t *testing.T) {
	db := schemaFixture(t)
	setTestUserToken(t, db, 2, testToken)
	_, h := portalFixture(t, db)
	personal := personalCookie(t, h, testToken)
	admin := adminCookie(t, h)
	paths := []string{"/admin/users", "/admin/users/2/rename", "/admin/users/2/token", "/admin/users/2/disable"}
	for _, path := range paths {
		for _, cookies := range [][]*http.Cookie{nil, {personal}} {
			w := portalRequest(h, "POST", path, url.Values{"display_name": {"Changed"}, "role": {"member"}}, cookies...)
			if w.Code != 403 {
				t.Fatalf("unauthorized %s = %d", path, w.Code)
			}
		}
		if path != "/admin/users" {
			if w := portalRequest(h, "GET", path, nil, admin); w.Code != 405 {
				t.Fatalf("GET mutation %s = %d", path, w.Code)
			}
		}
	}
	for _, path := range append(paths, "/login", "/admin/logout") {
		for _, header := range []string{"Origin", "Sec-Fetch-Site"} {
			r := httptest.NewRequest("POST", path, strings.NewReader(url.Values{"display_name": {"Changed"}, "role": {"member"}, "token": {testAdminToken}}.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if header == "Origin" {
				r.Header.Set(header, "https://other.example")
			} else {
				r.Header.Set(header, "cross-site")
			}
			r.AddCookie(admin)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 403 {
				t.Fatalf("cross-origin %s = %d", path, w.Code)
			}
		}
	}
	user, err := store.New(db).GetUser(context.Background(), 2)
	if err != nil || user.DisplayName != "Member" || user.DisabledAt.Valid {
		t.Fatal("rejected requests changed account data")
	}
	if w := portalRequest(h, "GET", "/test/protected", nil, personal); w.Code != 200 {
		t.Fatal("rejected request revoked personal access")
	}
	if w := portalRequest(h, "GET", "/admin/users", nil, admin); w.Code != 200 {
		t.Fatal("rejected logout revoked admin access")
	}
}

func TestReplaceDisableAndReenable(t *testing.T) {
	db := schemaFixture(t)
	setTestUserToken(t, db, 2, testToken)
	setTestUserToken(t, db, 1, "different-owner-personal-token-0123456789")
	_, h := portalFixture(t, db)
	admin := adminCookie(t, h)
	old := personalCookie(t, h, testToken)
	other := personalCookie(t, h, "different-owner-personal-token-0123456789")
	w := portalRequest(h, "POST", "/admin/users/2/token", nil, admin)
	replacement := issuedToken(t, w, 200)
	if replacement == testToken {
		t.Fatal("token was not replaced")
	}
	if w := portalRequest(h, "GET", "/test/protected", nil, old); w.Code != 401 {
		t.Fatal("replaced token's session remains valid")
	}
	if w := authRequest(h, "POST", "/login", testToken, nil); len(w.Result().Cookies()) != 0 {
		t.Fatal("old token remains valid")
	}
	current := personalCookie(t, h, replacement)
	if w := portalRequest(h, "POST", "/admin/users/2/disable", nil, admin); w.Code != 303 {
		t.Fatalf("disable = %d", w.Code)
	}
	if w := portalRequest(h, "GET", "/test/protected", nil, current); w.Code != 401 {
		t.Fatal("disabled session remains valid")
	}
	if w := authRequest(h, "POST", "/login", replacement, nil); len(w.Result().Cookies()) != 0 {
		t.Fatal("disabled token remains valid")
	}
	users, err := store.New(db).ListManagedUsers(context.Background())
	if err != nil || !users[1].DisabledAt.Valid || users[1].HasToken {
		t.Fatal("disable did not remove credential and mark the profile")
	}
	reenabled := issuedToken(t, portalRequest(h, "POST", "/admin/users/2/token", nil, admin), 200)
	active := personalCookie(t, h, reenabled)
	if w := portalRequest(h, "GET", "/test/protected", nil, active); w.Code != 200 {
		t.Fatal("re-enable failed")
	}
	if w := portalRequest(h, "GET", "/test/protected", nil, other); w.Code != 200 {
		t.Fatal("another person's session was revoked")
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM ratings").Scan(&count); err != nil || count != 4 {
		t.Fatal("account actions lost ratings")
	}
	// Renaming changes the displayed identity without changing its ratings or token.
	if w := portalRequest(h, "POST", "/admin/users/2/rename", url.Values{"display_name": {"Renamed"}}, admin); w.Code != 303 {
		t.Fatal("rename failed")
	}
	if w := portalRequest(h, "GET", "/test/protected", nil, active); w.Body.String() != "Renamed" {
		t.Fatal("session retained stale user data")
	}
}

func TestPortalValidationAndLogging(t *testing.T) {
	a, h := portalFixture(t, nil)
	admin := adminCookie(t, h)
	var logs bytes.Buffer
	logger, err := newLogger(&logs, "debug")
	if err != nil {
		t.Fatal(err)
	}
	logged := requestLogging(logger, h)
	for _, role := range []string{"owner", "member", "member", "member"} {
		w := portalRequest(logged, "POST", "/admin/users", url.Values{"display_name": {"<b>Person</b>"}, "role": {role}}, admin)
		token := issuedToken(t, w, 201)
		if !strings.Contains(w.Body.String(), "&lt;b&gt;Person&lt;/b&gt;") {
			t.Fatal("name was not escaped")
		}
		if strings.Contains(logs.String(), token) || strings.Contains(logs.String(), admin.Value) || strings.Contains(logs.String(), testAdminToken) {
			t.Fatal("credential leaked into logs")
		}
		if !strings.Contains(logs.String(), `"message":"user created"`) {
			t.Fatal("missing user creation event")
		}
	}
	for _, tt := range []struct {
		name, role string
		status     int
	}{
		{"Another", "owner", 409}, {"Another", "member", 409}, {"", "member", 400}, {strings.Repeat("x", 81), "member", 400}, {"Name", "admin", 400},
	} {
		w := portalRequest(h, "POST", "/admin/users", url.Values{"display_name": {tt.name}, "role": {tt.role}}, admin)
		if w.Code != tt.status || !strings.Contains(w.Body.String(), `role="alert"`) {
			t.Fatalf("invalid create = %d %s", w.Code, w.Body.String())
		}
	}
	users, err := a.queries.ListManagedUsers(context.Background())
	if err != nil || len(users) != 4 {
		t.Fatal("failed create left an extra profile")
	}
	for _, user := range users {
		if !user.HasToken {
			t.Fatal("profile was created without its credential")
		}
	}
	for _, path := range []string{"/admin/users/999/token", "/admin/users/bad/disable", "/admin/users/999/rename"} {
		w := portalRequest(h, "POST", path, url.Values{"display_name": {"Name"}}, admin)
		if w.Code != 404 {
			t.Fatalf("missing user %s = %d", path, w.Code)
		}
	}
}

func TestCredentialCheckedAfterExternalRevocation(t *testing.T) {
	db := schemaFixture(t)
	setTestUserToken(t, db, 2, testToken)
	_, h := portalFixture(t, db)
	cookie := personalCookie(t, h, testToken)
	// Bypass in-memory invalidation to verify the database check itself.
	setTestUserToken(t, db, 2, "replacement-personal-token-for-test")
	if w := portalRequest(h, "GET", "/test/protected", nil, cookie); w.Code != 401 {
		t.Fatal("session did not recheck its credential")
	}
}

func TestPortalTransactionsRollbackOnCredentialFailure(t *testing.T) {
	db := schemaFixture(t)
	setTestUserToken(t, db, 2, testToken)
	_, h := portalFixture(t, db)
	admin := adminCookie(t, h)
	execSchema(t, db, `ALTER TABLE user_tokens RENAME TO unavailable_user_tokens`)
	for _, path := range []string{"/admin/users", "/admin/users/2/disable", "/admin/users/2/token"} {
		w := portalRequest(h, "POST", path, url.Values{"display_name": {"New member"}, "role": {"member"}}, admin)
		if w.Code != 500 || w.Body.String() != "Unable to manage users. Please try again later.\n" {
			t.Fatalf("failed transaction %s = %d %s", path, w.Code, w.Body.String())
		}
	}
	users, err := store.New(db).ListUsers(context.Background())
	if err != nil || len(users) != 2 || users[1].DisabledAt.Valid {
		t.Fatal("credential failure left a partial account mutation")
	}
	execSchema(t, db, `ALTER TABLE unavailable_user_tokens RENAME TO user_tokens`)
	personalCookie(t, h, testToken)
}

func TestAuthenticationDatabaseFailure(t *testing.T) {
	db := schemaFixture(t)
	setTestUserToken(t, db, 2, testToken)
	_, h := portalFixture(t, db)
	cookie := personalCookie(t, h, testToken)
	db.Close()
	for _, request := range []struct{ method, path string }{{"POST", "/login"}, {"GET", "/test/protected"}} {
		w := authRequest(h, request.method, request.path, testToken, cookie)
		if w.Code != 500 || strings.Contains(w.Body.String(), "database") || strings.Contains(w.Body.String(), testToken) {
			t.Fatalf("database error response = %d %s", w.Code, w.Body.String())
		}
	}
}

func activeSessionCookie(t *testing.T, w *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	var active *http.Cookie
	for _, cookie := range w.Result().Cookies() {
		if cookie.MaxAge <= 0 {
			continue
		}
		if active != nil || cookie.Name != name {
			t.Fatal("login created unexpected active sessions")
		}
		active = cookie
	}
	if active == nil {
		t.Fatal("login did not issue the expected session")
	}
	return active
}

func TestUnifiedSignInBrowserCookies(t *testing.T) {
	db := schemaFixture(t)
	setTestUserToken(t, db, 2, testToken)
	a, h := portalFixture(t, db)
	a.insecureCookie = true
	server := httptest.NewServer(h)
	defer server.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	post := func(path, token, destination string) {
		t.Helper()
		response, err := client.PostForm(server.URL+path, url.Values{"token": {token}})
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 200 || response.Request.URL.Path != destination {
			t.Fatalf("%s ended at %s (%d)", path, response.Request.URL.Path, response.StatusCode)
		}
	}
	endpoint, _ := url.Parse(server.URL + "/login")
	post("/login", testToken, "/")
	if cookies := jar.Cookies(endpoint); len(cookies) != 1 || cookies[0].Name != sessionCookie {
		t.Fatal("personal cookie state incorrect")
	}
	post("/login", testAdminToken, "/admin/users")
	if cookies := jar.Cookies(endpoint); len(cookies) != 1 || cookies[0].Name != adminSessionCookie {
		t.Fatal("admin cookie must replace personal cookie and cover product paths")
	}
	response, err := client.Get(server.URL + "/challenges/2026")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.Request.URL.Path != "/admin/users" {
		t.Fatal("browser bypassed admin-only navigation")
	}
	response, err = client.PostForm(server.URL+"/login", url.Values{"token": {testToken}})
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("browser switched out of admin without logout")
	}
	post("/admin/logout", "", "/login")
	if len(jar.Cookies(endpoint)) != 0 {
		t.Fatal("logout left a session cookie behind")
	}
	post("/login", testToken, "/")
}
