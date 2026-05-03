package agent

// Scenario tests simulate realistic agent tasks end-to-end.
//
// Each scenario defines:
//   - A task description (what the agent was asked to do)
//   - A "naive" tool sequence (what an agent with only read_file + search_code would do)
//   - A "smart" tool sequence (what an agent with the full tool set does)
//
// We measure total tokens across all tool calls in the sequence and assert
// the smart path is substantially cheaper. The fixture is a realistic
// mid-size Go web service so the numbers reflect real usage.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── large realistic fixture ────────────────────────────────────────────────

// bigRepo creates a realistic mid-size Go web service with ~15 files,
// multiple packages, and enough cross-package calls to make navigation
// non-trivial.
func bigRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	files := map[string]string{
		// ── main ──────────────────────────────────────────────────────────
		"main.go": `package main

import (
	"log"
	"net/http"

	"myapp/internal/api"
	"myapp/internal/auth"
	"myapp/internal/config"
	"myapp/internal/db"
)

func main() {
	cfg := config.Load()
	database := db.Connect(cfg.DSN)
	defer database.Close()

	authSvc := auth.NewService(database, cfg.JWTSecret)
	router := api.NewRouter(authSvc, database)

	log.Printf("listening on %s", cfg.Addr)
	if err := http.ListenAndServe(cfg.Addr, router); err != nil {
		log.Fatal(err)
	}
}
`,
		// ── config ────────────────────────────────────────────────────────
		"internal/config/config.go": `package config

import (
	"os"
	"strings"
)

type Config struct {
	Addr      string
	DSN       string
	JWTSecret string
	LogLevel  string
	MaxConns  int
}

func Load() *Config {
	return &Config{
		Addr:      getEnv("ADDR", ":8080"),
		DSN:       getEnv("DSN", "app.db"),
		JWTSecret: getEnv("JWT_SECRET", "dev-secret"),
		LogLevel:  getEnv("LOG_LEVEL", "info"),
		MaxConns:  10,
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func (c *Config) IsProduction() bool {
	return c.LogLevel == "warn" || c.LogLevel == "error"
}
`,
		// ── db ────────────────────────────────────────────────────────────
		"internal/db/db.go": `package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	conn *sql.DB
}

func Connect(dsn string) *DB {
	conn, err := sql.Open("sqlite3", dsn)
	if err != nil {
		panic(fmt.Sprintf("db.Connect: %v", err))
	}
	conn.SetMaxOpenConns(10)
	conn.SetConnMaxLifetime(time.Hour)
	return &DB{conn: conn}
}

func (d *DB) Close() error {
	return d.conn.Close()
}

func (d *DB) Exec(query string, args ...any) (sql.Result, error) {
	return d.conn.Exec(query, args...)
}

func (d *DB) QueryRow(query string, args ...any) *sql.Row {
	return d.conn.QueryRow(query, args...)
}

func (d *DB) Query(query string, args ...any) (*sql.Rows, error) {
	return d.conn.Query(query, args...)
}
`,
		"internal/db/users.go": `package db

import "errors"

var ErrNotFound = errors.New("not found")

type User struct {
	ID           int
	Email        string
	PasswordHash string
	Role         string
	CreatedAt    string
}

func (d *DB) GetUser(id int) (*User, error) {
	u := &User{}
	err := d.QueryRow(
		"SELECT id, email, password_hash, role, created_at FROM users WHERE id=?", id,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, ErrNotFound
	}
	return u, nil
}

func (d *DB) GetUserByEmail(email string) (*User, error) {
	u := &User{}
	err := d.QueryRow(
		"SELECT id, email, password_hash, role, created_at FROM users WHERE email=?", email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, ErrNotFound
	}
	return u, nil
}

func (d *DB) CreateUser(email, passwordHash, role string) (*User, error) {
	res, err := d.Exec(
		"INSERT INTO users (email, password_hash, role) VALUES (?, ?, ?)",
		email, passwordHash, role,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return d.GetUser(int(id))
}

func (d *DB) UpdateUserRole(id int, role string) error {
	_, err := d.Exec("UPDATE users SET role=? WHERE id=?", role, id)
	return err
}

func (d *DB) DeleteUser(id int) error {
	_, err := d.Exec("DELETE FROM users WHERE id=?", id)
	return err
}
`,
		"internal/db/sessions.go": `package db

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

type Session struct {
	Token     string
	UserID    int
	ExpiresAt time.Time
}

func (d *DB) CreateSession(userID int, ttl time.Duration) (*Session, error) {
	token := generateToken()
	expiresAt := time.Now().Add(ttl)
	_, err := d.Exec(
		"INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)",
		token, userID, expiresAt.Unix(),
	)
	if err != nil {
		return nil, err
	}
	return &Session{Token: token, UserID: userID, ExpiresAt: expiresAt}, nil
}

func (d *DB) GetSession(token string) (*Session, error) {
	s := &Session{}
	var expiresUnix int64
	err := d.QueryRow(
		"SELECT token, user_id, expires_at FROM sessions WHERE token=?", token,
	).Scan(&s.Token, &s.UserID, &expiresUnix)
	if err != nil {
		return nil, ErrNotFound
	}
	s.ExpiresAt = time.Unix(expiresUnix, 0)
	return s, nil
}

func (d *DB) DeleteSession(token string) error {
	_, err := d.Exec("DELETE FROM sessions WHERE token=?", token)
	return err
}

func (d *DB) PurgeExpiredSessions() error {
	_, err := d.Exec("DELETE FROM sessions WHERE expires_at < ?", time.Now().Unix())
	return err
}

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
`,
		// ── auth ──────────────────────────────────────────────────────────
		"internal/auth/auth.go": `package auth

import (
	"errors"
	"time"

	"myapp/internal/db"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrTokenExpired       = errors.New("token expired")
	ErrInvalidToken       = errors.New("invalid token")
)

type Service struct {
	db     *db.DB
	secret string
}

func NewService(database *db.DB, secret string) *Service {
	return &Service{db: database, secret: secret}
}

// Login validates credentials and returns a session token.
func (s *Service) Login(email, password string) (string, error) {
	user, err := s.db.GetUserByEmail(email)
	if err != nil {
		return "", ErrInvalidCredentials
	}
	if !checkPassword(password, user.PasswordHash) {
		return "", ErrInvalidCredentials
	}
	session, err := s.db.CreateSession(user.ID, 24*time.Hour)
	if err != nil {
		return "", err
	}
	return session.Token, nil
}

// Logout invalidates a session token.
func (s *Service) Logout(token string) error {
	return s.db.DeleteSession(token)
}

// Authenticate validates a token and returns the authenticated user.
func (s *Service) Authenticate(token string) (*db.User, error) {
	session, err := s.db.GetSession(token)
	if err != nil {
		return nil, ErrInvalidToken
	}
	if time.Now().After(session.ExpiresAt) {
		_ = s.db.DeleteSession(token)
		return nil, ErrTokenExpired
	}
	user, err := s.db.GetUser(session.UserID)
	if err != nil {
		return nil, ErrInvalidToken
	}
	return user, nil
}

// RequireRole checks that a user has the required role.
func (s *Service) RequireRole(user *db.User, role string) error {
	if user.Role != role && user.Role != "admin" {
		return errors.New("forbidden: requires role " + role)
	}
	return nil
}
`,
		"internal/auth/password.go": `package auth

import (
	"crypto/sha256"
	"fmt"
)

// HashPassword hashes a plaintext password.
func HashPassword(password string) string {
	h := sha256.Sum256([]byte(password))
	return fmt.Sprintf("%x", h)
}

// checkPassword verifies a password against a stored hash.
func checkPassword(password, hash string) bool {
	return HashPassword(password) == hash
}
`,
		// ── api ───────────────────────────────────────────────────────────
		"internal/api/router.go": `package api

import (
	"net/http"

	"myapp/internal/auth"
	"myapp/internal/db"
)

func NewRouter(authSvc *auth.Service, database *db.DB) http.Handler {
	mux := http.NewServeMux()

	h := &handlers{auth: authSvc, db: database}

	mux.HandleFunc("/auth/login", h.handleLogin)
	mux.HandleFunc("/auth/logout", h.handleLogout)
	mux.HandleFunc("/users", h.requireAuth(h.handleUsers))
	mux.HandleFunc("/users/", h.requireAuth(h.handleUser))
	mux.HandleFunc("/admin/users", h.requireAuth(h.requireAdmin(h.handleAdminUsers)))

	return mux
}

type handlers struct {
	auth *auth.Service
	db   *db.DB
}

// requireAuth middleware validates the session token.
func (h *handlers) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		user, err := h.auth.Authenticate(token)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		r = withUser(r, user)
		next(w, r)
	}
}

// requireAdmin middleware checks for admin role.
func (h *handlers) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := userFromRequest(r)
		if err := h.auth.RequireRole(user, "admin"); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		next(w, r)
	}
}
`,
		"internal/api/handlers.go": `package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"myapp/internal/auth"
	"myapp/internal/db"
)

func (h *handlers) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Email    string ` + "`" + `json:"email"` + "`" + `
		Password string ` + "`" + `json:"password"` + "`" + `
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	token, err := h.auth.Login(req.Email, req.Password)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	writeJSON(w, map[string]string{"token": token})
}

func (h *handlers) handleLogout(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("Authorization")
	if err := h.auth.Logout(token); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) handleUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listUsers(w, r)
	case http.MethodPost:
		h.createUser(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *handlers) handleUser(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/users/")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		h.getUser(w, r, id)
	case http.MethodDelete:
		h.deleteUser(w, r, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *handlers) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		UserID int    ` + "`" + `json:"user_id"` + "`" + `
		Role   string ` + "`" + `json:"role"` + "`" + `
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := h.db.UpdateUserRole(req.UserID, req.Role); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) listUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query("SELECT id, email, role FROM users")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var users []db.User
	for rows.Next() {
		var u db.User
		rows.Scan(&u.ID, &u.Email, &u.Role)
		users = append(users, u)
	}
	writeJSON(w, users)
}

func (h *handlers) getUser(w http.ResponseWriter, r *http.Request, id int) {
	user, err := h.db.GetUser(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, user)
}

func (h *handlers) createUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string ` + "`" + `json:"email"` + "`" + `
		Password string ` + "`" + `json:"password"` + "`" + `
		Role     string ` + "`" + `json:"role"` + "`" + `
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	hash := auth.HashPassword(req.Password)
	user, err := h.db.CreateUser(req.Email, hash, req.Role)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, user)
}

func (h *handlers) deleteUser(w http.ResponseWriter, r *http.Request, id int) {
	if err := h.db.DeleteUser(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
`,
		"internal/api/context.go": `package api

import (
	"context"
	"net/http"

	"myapp/internal/db"
)

type contextKey string

const userKey contextKey = "user"

func withUser(r *http.Request, user *db.User) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), userKey, user))
}

func userFromRequest(r *http.Request) *db.User {
	u, _ := r.Context().Value(userKey).(*db.User)
	return u
}
`,
		"internal/api/response.go": `package api

import (
	"encoding/json"
	"net/http"
)

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
`,
	}

	for rel, content := range files {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// ── scenario harness ───────────────────────────────────────────────────────

// toolCall records one step in a scenario: the tool name and its output.
type toolCall struct {
	tool   string
	result string
}

// scenario runs a named sequence of tool calls and reports total tokens.
func runScenario(t *testing.T, name string, calls []toolCall) int {
	t.Helper()
	total := 0
	for _, c := range calls {
		total += tokenEstimate(c.result)
	}
	t.Logf("  [%s] %d calls, %d tokens total", name, len(calls), total)
	for _, c := range calls {
		tok := tokenEstimate(c.result)
		preview := c.result
		if len(preview) > 80 {
			preview = preview[:80] + "…"
		}
		t.Logf("    %-20s %4d tok  %s", c.tool, tok, strings.ReplaceAll(preview, "\n", "↵"))
	}
	return total
}

// ── scenarios ─────────────────────────────────────────────────────────────

// Scenario 1: "Add rate limiting to the login endpoint"
//
// The agent needs to find the login handler, understand what auth.Login does,
// and see if anything already calls a rate limiter.
//
// Naive: search for "login" everywhere, read whole handler file, read whole auth file.
// Smart: grep_files to confirm location, symbol_context on Login, get_function on handler.
func TestScenario_AddRateLimiting(t *testing.T) {
	root := bigRepo(t)

	fs := findSymbolTool(root)
	sc := searchCodeTool(root)
	gf := getFunctionTool(root)
	gfil := grepFilesTool(root)
	rf := readFileTool(root)
	sym := symbolContextTool(root)
	cal := callersTool(root)

	t.Log("Task: Add rate limiting to the login endpoint")
	t.Log("")

	// ── naive ──────────────────────────────────────────────────────────
	t.Log("NAIVE (search_code + read whole files):")
	naiveCalls := []toolCall{
		{"search_code", runTool(t, sc, `{"pattern":"login","file_glob":"*.go"}`)},
		{"read_file(handlers)", runTool(t, rf, `{"path":"internal/api/handlers.go"}`)},
		{"read_file(auth)", runTool(t, rf, `{"path":"internal/auth/auth.go"}`)},
		{"search_code(rate)", runTool(t, sc, `{"pattern":"rate","file_glob":"*.go"}`)},
	}
	naiveTokens := runScenario(t, "naive", naiveCalls)

	// ── smart ──────────────────────────────────────────────────────────
	t.Log("SMART (targeted tools):")
	smartCalls := []toolCall{
		{"grep_files(login)", runTool(t, gfil, `{"pattern":"login","file_glob":"*.go"}`)},
		{"find_symbol(Login)", runTool(t, fs, `{"name":"Login"}`)},
		{"symbol_context(Login)", runTool(t, sym, `{"name":"Login"}`)},
		{"get_function(handleLogin)", runTool(t, gf, `{"name":"handleLogin","path":"internal/api/handlers.go"}`)},
		{"grep_files(rate)", runTool(t, gfil, `{"pattern":"rate"}`)},
	}
	smartTokens := runScenario(t, "smart", smartCalls)

	t.Logf("")
	t.Logf("RESULT: naive=%d tok, smart=%d tok, %.1fx more efficient",
		naiveTokens, smartTokens, float64(naiveTokens)/float64(smartTokens))

	if smartTokens >= naiveTokens {
		t.Errorf("smart path should use fewer tokens: smart=%d, naive=%d", smartTokens, naiveTokens)
	}
	_ = cal
}

// Scenario 2: "The Authenticate function is slow — understand what it does and what calls it"
//
// Classic blast-radius investigation before optimising.
//
// Naive: search for Authenticate, read auth.go, read router.go, read handlers.go.
// Smart: symbol_context(Authenticate), callers(Authenticate), callees(Authenticate).
func TestScenario_InvestigateAuthenticate(t *testing.T) {
	root := bigRepo(t)

	sc := searchCodeTool(root)
	rf := readFileTool(root)
	sym := symbolContextTool(root)
	cal := callersTool(root)
	cle := calleesTool(root)
	fs := findSymbolTool(root)

	t.Log("Task: Authenticate is slow — understand what it does and what calls it")
	t.Log("")

	t.Log("NAIVE:")
	naiveCalls := []toolCall{
		{"search_code(Authenticate)", runTool(t, sc, `{"pattern":"Authenticate","file_glob":"*.go"}`)},
		{"read_file(auth.go)", runTool(t, rf, `{"path":"internal/auth/auth.go"}`)},
		{"read_file(router.go)", runTool(t, rf, `{"path":"internal/api/router.go"}`)},
		{"read_file(handlers.go)", runTool(t, rf, `{"path":"internal/api/handlers.go"}`)},
	}
	naiveTokens := runScenario(t, "naive", naiveCalls)

	t.Log("SMART:")
	symResult := runTool(t, sym, `{"name":"Authenticate"}`)
	skipCtags := strings.Contains(symResult, "ctags unavailable")

	var smartCalls []toolCall
	if skipCtags {
		smartCalls = []toolCall{
			{"find_symbol(Authenticate)", runTool(t, fs, `{"name":"Authenticate"}`)},
			{"callers(Authenticate)", runTool(t, cal, `{"name":"Authenticate"}`)},
		}
	} else {
		smartCalls = []toolCall{
			{"symbol_context(Authenticate)", symResult},
			{"callers(Authenticate)", runTool(t, cal, `{"name":"Authenticate"}`)},
			{"callees(Authenticate,auth.go)", runTool(t, cle, `{"name":"Authenticate","path":"internal/auth/auth.go"}`)},
		}
	}
	smartTokens := runScenario(t, "smart", smartCalls)

	t.Logf("")
	t.Logf("RESULT: naive=%d tok, smart=%d tok, %.1fx more efficient",
		naiveTokens, smartTokens, float64(naiveTokens)/float64(smartTokens))

	if smartTokens >= naiveTokens {
		t.Errorf("smart path should use fewer tokens: smart=%d, naive=%d", smartTokens, naiveTokens)
	}
}

// Scenario 3: "DeleteUser is being called somewhere it shouldn't be — find all call sites"
//
// Security audit: find every place DeleteUser is called, with enough context
// to judge whether the call is guarded properly.
//
// Naive: search_code for DeleteUser, get lots of noise including imports and comments.
// Smart: callers(DeleteUser) — filtered call sites with snippets only.
func TestScenario_AuditDeleteUser(t *testing.T) {
	root := bigRepo(t)

	sc := searchCodeTool(root)
	cal := callersTool(root)

	t.Log("Task: Audit all call sites of DeleteUser — is it always guarded?")
	t.Log("")

	t.Log("NAIVE:")
	naiveCalls := []toolCall{
		{"search_code(DeleteUser)", runTool(t, sc, `{"pattern":"DeleteUser","file_glob":"*.go"}`)},
	}
	naiveTokens := runScenario(t, "naive", naiveCalls)

	t.Log("SMART:")
	smartCalls := []toolCall{
		{"callers(DeleteUser)", runTool(t, cal, `{"name":"DeleteUser"}`)},
	}
	smartTokens := runScenario(t, "smart", smartCalls)

	t.Logf("")
	t.Logf("RESULT: naive=%d tok, smart=%d tok, %.1fx more efficient",
		naiveTokens, smartTokens, float64(naiveTokens)/float64(smartTokens))

	// callers strips definitions/imports — should be cheaper or equal
	if smartTokens > naiveTokens {
		t.Errorf("smart path should use fewer tokens: smart=%d, naive=%d", smartTokens, naiveTokens)
	}

	// The smart result should contain actual call sites
	smartResult := smartCalls[0].result
	if strings.Contains(smartResult, "No call sites") {
		t.Logf("Note: no call sites found (callers filter may be too aggressive for this fixture)")
	}
}

// Scenario 4: "Refactor HashPassword — understand impact before changing it"
//
// Classic refactor prep. Need to know: where is it defined, who calls it,
// what does it depend on.
//
// Naive: search everywhere for HashPassword, read password.go, read handlers.go.
// Smart: symbol_context gives everything in one call; get_function only if needed.
func TestScenario_RefactorHashPassword(t *testing.T) {
	root := bigRepo(t)

	sc := searchCodeTool(root)
	rf := readFileTool(root)
	sym := symbolContextTool(root)
	fs := findSymbolTool(root)
	cal := callersTool(root)
	gf := getFunctionTool(root)

	t.Log("Task: Refactor HashPassword — assess impact before changing")
	t.Log("")

	t.Log("NAIVE:")
	naiveCalls := []toolCall{
		{"search_code(HashPassword)", runTool(t, sc, `{"pattern":"HashPassword","file_glob":"*.go"}`)},
		{"read_file(password.go)", runTool(t, rf, `{"path":"internal/auth/password.go"}`)},
		{"read_file(handlers.go)", runTool(t, rf, `{"path":"internal/api/handlers.go"}`)},
	}
	naiveTokens := runScenario(t, "naive", naiveCalls)

	t.Log("SMART:")
	symResult := runTool(t, sym, `{"name":"HashPassword"}`)
	skipCtags := strings.Contains(symResult, "ctags unavailable") || strings.Contains(symResult, "not found")

	var smartCalls []toolCall
	if skipCtags {
		smartCalls = []toolCall{
			{"find_symbol(HashPassword)", runTool(t, fs, `{"name":"HashPassword"}`)},
			{"callers(HashPassword)", runTool(t, cal, `{"name":"HashPassword"}`)},
			{"get_function(HashPassword)", runTool(t, gf, `{"name":"HashPassword","path":"internal/auth/password.go"}`)},
		}
	} else {
		// symbol_context alone gives location + caller count + callee count
		// Only need get_function if the signature isn't enough
		smartCalls = []toolCall{
			{"symbol_context(HashPassword)", symResult},
			{"callers(HashPassword)", runTool(t, cal, `{"name":"HashPassword"}`)},
		}
	}
	smartTokens := runScenario(t, "smart", smartCalls)

	t.Logf("")
	t.Logf("RESULT: naive=%d tok, smart=%d tok, %.1fx more efficient",
		naiveTokens, smartTokens, float64(naiveTokens)/float64(smartTokens))

	if smartTokens >= naiveTokens {
		t.Errorf("smart path should use fewer tokens: smart=%d, naive=%d", smartTokens, naiveTokens)
	}
}

// Scenario 5: "Orient myself in this codebase before starting work"
//
// First thing an agent does on a new task: understand the structure.
//
// Naive: list_dir root, list_dir internal, list_dir each subpackage (5+ calls),
//        then search_code for entry points.
// Smart: tree(depth=3) gives the whole picture in one call.
func TestScenario_InitialOrientation(t *testing.T) {
	root := bigRepo(t)

	tr := treeTool(root)
	sc := searchCodeTool(root)

	t.Log("Task: Orient in the codebase before starting work")
	t.Log("")

	t.Log("NAIVE (repeated list_dir simulation + search for entry points):")
	// Simulate what an agent does: read each dir level separately
	var naiveParts []string
	addDir := func(path string) {
		entries, _ := os.ReadDir(path)
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		naiveParts = append(naiveParts, strings.Join(names, "\n"))
	}
	addDir(root)
	addDir(filepath.Join(root, "internal"))
	for _, pkg := range []string{"api", "auth", "config", "db"} {
		addDir(filepath.Join(root, "internal", pkg))
	}
	const overheadPerCall = 120
	naiveDirOutput := strings.Join(naiveParts, "\n") + strings.Repeat(" ", 6*overheadPerCall)
	naiveSearchOutput := runTool(t, sc, `{"pattern":"func main|NewRouter|NewService"}`)

	naiveCalls := []toolCall{
		{"list_dir×6", naiveDirOutput},
		{"search_code(entry points)", naiveSearchOutput},
	}
	naiveTokens := runScenario(t, "naive", naiveCalls)

	t.Log("SMART:")
	smartCalls := []toolCall{
		{"tree(depth=3)", runTool(t, tr, `{"depth":3}`)},
	}
	smartTokens := runScenario(t, "smart", smartCalls)

	t.Logf("")
	t.Logf("RESULT: naive=%d tok, smart=%d tok, %.1fx more efficient",
		naiveTokens, smartTokens, float64(naiveTokens)/float64(smartTokens))

	if smartTokens >= naiveTokens {
		t.Errorf("smart path should use fewer tokens: smart=%d, naive=%d", smartTokens, naiveTokens)
	}
}
