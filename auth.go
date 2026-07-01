package main

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	sessionCookieName = "factmapgen_session"
	sessionDuration   = 7 * 24 * time.Hour
	passwordIters     = 210000
)

type authStore struct {
	db *sql.DB
}

type authUser struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	IsAdmin   bool   `json:"isAdmin"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type publicSession struct {
	Authenticated bool      `json:"authenticated"`
	User          *authUser `json:"user,omitempty"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type userWriteRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	IsAdmin  *bool  `json:"isAdmin"`
}

type auditEntry struct {
	ID            int64  `json:"id"`
	ActorUserID   int64  `json:"actorUserId"`
	ActorUsername string `json:"actorUsername"`
	Action        string `json:"action"`
	TargetType    string `json:"targetType"`
	TargetID      string `json:"targetId"`
	Detail        string `json:"detail"`
	CreatedAt     string `json:"createdAt"`
}

var (
	errAuthRequired       = errors.New("authentication required")
	errAdminRequired      = errors.New("admin privileges required")
	errInvalidCredentials = errors.New("invalid username or password")
	errInvalidUsername    = errors.New("usernames must be 1-64 characters and use letters, numbers, dots, underscores, or hyphens")
	errInvalidPassword    = errors.New("passwords must be at least 8 characters")
	errLastAdmin          = errors.New("cannot remove the last admin account")
	errDeleteSelf         = errors.New("cannot delete the account you are logged in as")
)

func openAuthStore(path string) (*authStore, string, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, "", err
	}
	store := &authStore{db: db}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, "", err
	}
	password, err := store.ensureInitialAdmin()
	if err != nil {
		_ = db.Close()
		return nil, "", err
	}
	return store, password, nil
}

func (a *authStore) close() error {
	if a == nil || a.db == nil {
		return nil
	}
	return a.db.Close()
}

func (a *authStore) migrate() error {
	statements := []string{
		`PRAGMA foreign_keys = ON`,
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			is_admin INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			token_hash TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at TEXT NOT NULL,
			expires_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS audit_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			actor_user_id INTEGER,
			actor_username TEXT NOT NULL,
			action TEXT NOT NULL,
			target_type TEXT NOT NULL,
			target_id TEXT NOT NULL,
			detail TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_log(created_at DESC)`,
	}
	for _, stmt := range statements {
		if _, err := a.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (a *authStore) ensureInitialAdmin() (string, error) {
	var count int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return "", err
	}
	if count > 0 {
		return "", nil
	}
	password, err := randomToken(18)
	if err != nil {
		return "", err
	}
	if _, err := a.createUser("admin", password, true); err != nil {
		return "", err
	}
	return password, nil
}

func (a *authStore) authenticate(username, password string) (*authUser, error) {
	username = normalizeUsername(username)
	var user authUser
	var hash string
	var isAdmin int
	err := a.db.QueryRow(`SELECT id, username, password_hash, is_admin, created_at, updated_at FROM users WHERE username = ?`, username).Scan(
		&user.ID, &user.Username, &hash, &isAdmin, &user.CreatedAt, &user.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if !verifyPassword(password, hash) {
		return nil, errInvalidCredentials
	}
	user.IsAdmin = isAdmin != 0
	return &user, nil
}

func (a *authStore) createSession(userID int64) (string, time.Time, error) {
	token, err := randomToken(32)
	if err != nil {
		return "", time.Time{}, err
	}
	now := time.Now().UTC()
	expires := now.Add(sessionDuration)
	_, err = a.db.Exec(`INSERT INTO sessions (token_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`, hashToken(token), userID, now.Format(time.RFC3339), expires.Format(time.RFC3339))
	return token, expires, err
}

func (a *authStore) deleteSession(token string) error {
	if token == "" {
		return nil
	}
	_, err := a.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
	return err
}

func (a *authStore) userForSession(token string) (*authUser, error) {
	if token == "" {
		return nil, errAuthRequired
	}
	var user authUser
	var isAdmin int
	var expiresRaw string
	err := a.db.QueryRow(`SELECT u.id, u.username, u.is_admin, u.created_at, u.updated_at, s.expires_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ?`, hashToken(token)).Scan(&user.ID, &user.Username, &isAdmin, &user.CreatedAt, &user.UpdatedAt, &expiresRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errAuthRequired
	}
	if err != nil {
		return nil, err
	}
	expires, err := time.Parse(time.RFC3339, expiresRaw)
	if err != nil || time.Now().UTC().After(expires) {
		_ = a.deleteSession(token)
		return nil, errAuthRequired
	}
	user.IsAdmin = isAdmin != 0
	return &user, nil
}

func (a *authStore) listUsers() ([]authUser, error) {
	rows, err := a.db.Query(`SELECT id, username, is_admin, created_at, updated_at FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []authUser{}
	for rows.Next() {
		var user authUser
		var isAdmin int
		if err := rows.Scan(&user.ID, &user.Username, &isAdmin, &user.CreatedAt, &user.UpdatedAt); err != nil {
			return nil, err
		}
		user.IsAdmin = isAdmin != 0
		users = append(users, user)
	}
	return users, rows.Err()
}

func (a *authStore) createUser(username, password string, isAdmin bool) (*authUser, error) {
	username = normalizeUsername(username)
	if err := validateUsername(username); err != nil {
		return nil, err
	}
	if err := validatePassword(password); err != nil {
		return nil, err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := a.db.Exec(`INSERT INTO users (username, password_hash, is_admin, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, username, hash, boolInt(isAdmin), now, now)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return a.getUser(id)
}

func (a *authStore) getUser(id int64) (*authUser, error) {
	var user authUser
	var isAdmin int
	err := a.db.QueryRow(`SELECT id, username, is_admin, created_at, updated_at FROM users WHERE id = ?`, id).Scan(&user.ID, &user.Username, &isAdmin, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errProfileNotFound
	}
	if err != nil {
		return nil, err
	}
	user.IsAdmin = isAdmin != 0
	return &user, nil
}

func (a *authStore) updateUser(id int64, req userWriteRequest, actorID int64) (*authUser, error) {
	current, err := a.getUser(id)
	if err != nil {
		return nil, err
	}
	username := normalizeUsername(req.Username)
	if username == "" {
		username = current.Username
	}
	if err := validateUsername(username); err != nil {
		return nil, err
	}
	isAdmin := current.IsAdmin
	if req.IsAdmin != nil {
		isAdmin = *req.IsAdmin
	}
	if current.IsAdmin && !isAdmin {
		ok, err := a.canRemoveAdmin(id)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errLastAdmin
		}
	}
	if id == actorID && !isAdmin {
		return nil, errLastAdmin
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(req.Password) != "" {
		if err := validatePassword(req.Password); err != nil {
			return nil, err
		}
		hash, err := hashPassword(req.Password)
		if err != nil {
			return nil, err
		}
		if _, err := a.db.Exec(`UPDATE users SET username = ?, password_hash = ?, is_admin = ?, updated_at = ? WHERE id = ?`, username, hash, boolInt(isAdmin), now, id); err != nil {
			return nil, err
		}
	} else {
		if _, err := a.db.Exec(`UPDATE users SET username = ?, is_admin = ?, updated_at = ? WHERE id = ?`, username, boolInt(isAdmin), now, id); err != nil {
			return nil, err
		}
	}
	return a.getUser(id)
}

func (a *authStore) deleteUser(id, actorID int64) error {
	if id == actorID {
		return errDeleteSelf
	}
	user, err := a.getUser(id)
	if err != nil {
		return err
	}
	if user.IsAdmin {
		ok, err := a.canRemoveAdmin(id)
		if err != nil {
			return err
		}
		if !ok {
			return errLastAdmin
		}
	}
	_, err = a.db.Exec(`DELETE FROM users WHERE id = ?`, id)
	return err
}

func (a *authStore) canRemoveAdmin(id int64) (bool, error) {
	var count int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM users WHERE is_admin = 1 AND id <> ?`, id).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (a *authStore) logAudit(actor *authUser, action, targetType, targetID, detail string) {
	if a == nil || actor == nil {
		return
	}
	_, err := a.db.Exec(`INSERT INTO audit_log (actor_user_id, actor_username, action, target_type, target_id, detail, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		actor.ID, actor.Username, action, targetType, targetID, detail, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		log.Printf("write audit log: %v", err)
	}
}

func (a *authStore) listAudit(limit int) ([]auditEntry, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := a.db.Query(`SELECT id, COALESCE(actor_user_id, 0), actor_username, action, target_type, target_id, detail, created_at FROM audit_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []auditEntry{}
	for rows.Next() {
		var entry auditEntry
		if err := rows.Scan(&entry.ID, &entry.ActorUserID, &entry.ActorUsername, &entry.Action, &entry.TargetType, &entry.TargetID, &entry.Detail, &entry.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (s *server) handleSession(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		user, _ := s.currentUser(r)
		writeJSON(w, http.StatusOK, publicSession{Authenticated: user != nil, User: user})
	case http.MethodPost:
		var req loginRequest
		if err := decodeJSONRequest(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		user, err := s.auth.authenticate(req.Username, req.Password)
		if err != nil {
			writeAuthError(w, err)
			return
		}
		token, expires, err := s.auth.createSession(user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		setSessionCookie(w, token, expires)
		s.auth.logAudit(user, "login", "user", user.Username, "")
		writeJSON(w, http.StatusOK, publicSession{Authenticated: true, User: user})
	case http.MethodDelete:
		cookie, err := r.Cookie(sessionCookieName)
		if err == nil {
			_ = s.auth.deleteSession(cookie.Value)
		}
		clearSessionCookie(w)
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *server) handleUsers(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		users, err := s.auth.listUsers()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"users": users})
	case http.MethodPost:
		var req userWriteRequest
		if err := decodeJSONRequest(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		isAdmin := true
		if req.IsAdmin != nil {
			isAdmin = *req.IsAdmin
		}
		user, err := s.auth.createUser(req.Username, req.Password, isAdmin)
		if err != nil {
			writeAuthError(w, err)
			return
		}
		s.auth.logAudit(actor, "create", "user", user.Username, fmt.Sprintf("isAdmin=%v", user.IsAdmin))
		writeJSON(w, http.StatusCreated, user)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *server) handleUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/users/"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	switch r.Method {
	case http.MethodPut:
		var req userWriteRequest
		if err := decodeJSONRequest(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		user, err := s.auth.updateUser(id, req, actor.ID)
		if err != nil {
			writeAuthError(w, err)
			return
		}
		s.auth.logAudit(actor, "update", "user", user.Username, fmt.Sprintf("isAdmin=%v passwordChanged=%v", user.IsAdmin, strings.TrimSpace(req.Password) != ""))
		writeJSON(w, http.StatusOK, user)
	case http.MethodDelete:
		user, err := s.auth.getUser(id)
		if err != nil {
			writeAuthError(w, err)
			return
		}
		if err := s.auth.deleteUser(id, actor.ID); err != nil {
			writeAuthError(w, err)
			return
		}
		s.auth.logAudit(actor, "delete", "user", user.Username, "")
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *server) handleAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	entries, err := s.auth.listAudit(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"audit": entries})
}

func (s *server) currentUser(r *http.Request) (*authUser, error) {
	if s.auth == nil {
		return nil, errAuthRequired
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return nil, errAuthRequired
	}
	return s.auth.userForSession(cookie.Value)
}

func (s *server) requireUser(w http.ResponseWriter, r *http.Request) (*authUser, bool) {
	user, err := s.currentUser(r)
	if err != nil {
		writeAuthError(w, err)
		return nil, false
	}
	return user, true
}

func (s *server) requireAdmin(w http.ResponseWriter, r *http.Request) (*authUser, bool) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return nil, false
	}
	if !user.IsAdmin {
		writeAuthError(w, errAdminRequired)
		return nil, false
	}
	return user, true
}

func writeAuthError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, errAuthRequired):
		status = http.StatusUnauthorized
	case errors.Is(err, errAdminRequired):
		status = http.StatusForbidden
	case errors.Is(err, errInvalidCredentials):
		status = http.StatusUnauthorized
	case errors.Is(err, errInvalidUsername), errors.Is(err, errInvalidPassword), errors.Is(err, errLastAdmin), errors.Is(err, errDeleteSelf):
		status = http.StatusBadRequest
	case errors.Is(err, errProfileNotFound):
		status = http.StatusNotFound
	default:
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			status = http.StatusConflict
		}
	}
	writeError(w, status, err.Error())
}

func setSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordIters, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2_sha256$%d$%s$%s", passwordIters, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func verifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2_sha256" {
		return false
	}
	iters, err := strconv.Atoi(parts[1])
	if err != nil || iters <= 0 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iters, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

func randomToken(bytesLen int) (string, error) {
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawStdEncoding.EncodeToString(sum[:])
}

func normalizeUsername(username string) string {
	return strings.TrimSpace(strings.ToLower(username))
}

func validateUsername(username string) error {
	if username == "" || len(username) > 64 {
		return errInvalidUsername
	}
	for _, r := range username {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return errInvalidUsername
	}
	return nil
}

func validatePassword(password string) error {
	if len(password) < 8 {
		return errInvalidPassword
	}
	return nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func auditDetail(v any) string {
	body, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(body)
}
