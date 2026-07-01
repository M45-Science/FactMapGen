package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func newTestAuthStore(t *testing.T) (*authStore, string) {
	t.Helper()
	auth, password, err := openAuthStore(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("openAuthStore: %v", err)
	}
	t.Cleanup(func() { _ = auth.close() })
	return auth, password
}

func TestAuthStoreCreatesInitialAdminAndSession(t *testing.T) {
	auth, password := newTestAuthStore(t)
	if password == "" {
		t.Fatal("initial admin password was empty")
	}
	user, err := auth.authenticate("admin", password)
	if err != nil {
		t.Fatalf("authenticate initial admin: %v", err)
	}
	if user.Username != "admin" || !user.IsAdmin {
		t.Fatalf("initial admin = %#v, want admin account", user)
	}
	if _, err := auth.authenticate("admin", "wrong-password"); err != errInvalidCredentials {
		t.Fatalf("wrong password err = %v, want errInvalidCredentials", err)
	}
	token, _, err := auth.createSession(user.ID)
	if err != nil {
		t.Fatalf("createSession: %v", err)
	}
	sessionUser, err := auth.userForSession(token)
	if err != nil {
		t.Fatalf("userForSession: %v", err)
	}
	if sessionUser.ID != user.ID {
		t.Fatalf("session user id = %d, want %d", sessionUser.ID, user.ID)
	}
}

func TestAuthStoreUserLifecycleKeepsLastAdmin(t *testing.T) {
	auth, _ := newTestAuthStore(t)
	users, err := auth.listUsers()
	if err != nil {
		t.Fatalf("listUsers: %v", err)
	}
	admin := users[0]
	if err := auth.deleteUser(admin.ID, admin.ID); err != errDeleteSelf {
		t.Fatalf("delete self err = %v, want errDeleteSelf", err)
	}
	regular, err := auth.createUser("editor", "password123", false)
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
	if regular.IsAdmin {
		t.Fatal("regular user was admin")
	}
	makeAdmin := true
	if _, err := auth.updateUser(regular.ID, userWriteRequest{IsAdmin: &makeAdmin}, admin.ID); err != nil {
		t.Fatalf("promote user: %v", err)
	}
	if err := auth.deleteUser(admin.ID, regular.ID); err != nil {
		t.Fatalf("delete original admin with second admin available: %v", err)
	}
}

func TestAuthHandlersProtectUsersAndWriteAudit(t *testing.T) {
	auth, password := newTestAuthStore(t)
	srv := &server{auth: auth}

	unauth := httptest.NewRecorder()
	srv.handleUsers(unauth, httptest.NewRequest(http.MethodGet, "/api/users", nil))
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauth users status = %d, want 401", unauth.Code)
	}

	loginReq := httptest.NewRequest(http.MethodPost, "/api/session", strings.NewReader(`{"username":"admin","password":"`+password+`"}`))
	login := httptest.NewRecorder()
	srv.handleSession(login, loginReq)
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", login.Code, login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("login did not set a cookie")
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/users", strings.NewReader(`{"username":"auditor","password":"password123","isAdmin":false}`))
	createReq.AddCookie(cookies[0])
	create := httptest.NewRecorder()
	srv.handleUsers(create, createReq)
	if create.Code != http.StatusCreated {
		t.Fatalf("create user status = %d body=%s", create.Code, create.Body.String())
	}

	auditReq := httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	auditReq.AddCookie(cookies[0])
	audit := httptest.NewRecorder()
	srv.handleAudit(audit, auditReq)
	if audit.Code != http.StatusOK {
		t.Fatalf("audit status = %d body=%s", audit.Code, audit.Body.String())
	}
	var body struct {
		Audit []auditEntry `json:"audit"`
	}
	if err := json.Unmarshal(audit.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode audit: %v", err)
	}
	if len(body.Audit) == 0 || body.Audit[0].Action != "create" || body.Audit[0].TargetType != "user" {
		t.Fatalf("audit entries = %#v, want user create entry", body.Audit)
	}
}
