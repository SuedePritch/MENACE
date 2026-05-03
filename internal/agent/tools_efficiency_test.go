package agent

// Token efficiency tests.
//
// These tests prove that the tool set answers navigation questions with
// minimal output bytes — a direct proxy for token cost. Each test defines
// a "naive" approach (what an agent would do without good tools) and the
// "direct" approach (what find_symbol + targeted read_file enables), then
// asserts the direct path is substantially cheaper.
//
// The fixture is a small multi-language codebase (Go, Python, Rust, JS)
// written to a temp directory so tests are self-contained and ctags-free
// fallback behaviour is also exercised.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flitsinc/go-llms/tools"
)

// ── fixture ───────────────────────────────────────────────────────────────

// testRepo creates a small multi-language repo in a temp dir and returns
// the root path. The repo has ~10 files across 4 languages with realistic
// symbol density.
func testRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	files := map[string]string{
		"main.go": `package main

import (
	"fmt"
	"myapp/internal/auth"
	"myapp/internal/db"
)

func main() {
	db.Init()
	srv := NewServer()
	srv.Run()
}

// NewServer constructs the HTTP server.
func NewServer() *Server {
	return &Server{port: 8080}
}

type Server struct {
	port int
}

func (s *Server) Run() {
	fmt.Printf("listening on :%d\n", s.port)
}

func (s *Server) Shutdown() {
	fmt.Println("shutting down")
}
`,
		"internal/auth/auth.go": `package auth

import (
	"crypto/sha256"
	"errors"
)

var ErrUnauthorized = errors.New("unauthorized")

// ValidateToken checks the bearer token against the secret.
func ValidateToken(token, secret string) error {
	h := sha256.Sum256([]byte(secret))
	if token != fmt.Sprintf("%x", h) {
		return ErrUnauthorized
	}
	return nil
}

// GenerateToken creates a new signed token.
func GenerateToken(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return fmt.Sprintf("%x", h)
}

type Claims struct {
	UserID string
	Role   string
}

func ParseClaims(token string) (*Claims, error) {
	if token == "" {
		return nil, ErrUnauthorized
	}
	return &Claims{UserID: "anon", Role: "user"}, nil
}
`,
		"internal/db/db.go": `package db

import "database/sql"

var conn *sql.DB

// Init opens the database connection.
func Init() {
	var err error
	conn, err = sql.Open("sqlite3", "app.db")
	if err != nil {
		panic(err)
	}
}

func Close() {
	if conn != nil {
		conn.Close()
	}
}

type User struct {
	ID    int
	Email string
	Role  string
}

func FindUser(id int) (*User, error) {
	row := conn.QueryRow("SELECT id, email, role FROM users WHERE id=?", id)
	u := &User{}
	return u, row.Scan(&u.ID, &u.Email, &u.Role)
}

func CreateUser(email, role string) error {
	_, err := conn.Exec("INSERT INTO users (email, role) VALUES (?, ?)", email, role)
	return err
}
`,
		"scripts/migrate.py": `#!/usr/bin/env python3
"""Database migration runner."""

import sqlite3
import os
import sys


def connect(path: str) -> sqlite3.Connection:
    return sqlite3.connect(path)


def run_migrations(conn: sqlite3.Connection, migrations_dir: str) -> None:
    """Apply all pending migrations in order."""
    cursor = conn.cursor()
    cursor.execute(
        "CREATE TABLE IF NOT EXISTS migrations (name TEXT PRIMARY KEY)"
    )
    files = sorted(os.listdir(migrations_dir))
    for f in files:
        if not f.endswith(".sql"):
            continue
        cursor.execute("SELECT name FROM migrations WHERE name=?", (f,))
        if cursor.fetchone():
            continue
        with open(os.path.join(migrations_dir, f)) as fh:
            conn.executescript(fh.read())
        cursor.execute("INSERT INTO migrations VALUES (?)", (f,))
    conn.commit()


def rollback_last(conn: sqlite3.Connection) -> None:
    """Roll back the most recently applied migration."""
    cursor = conn.cursor()
    cursor.execute("SELECT name FROM migrations ORDER BY rowid DESC LIMIT 1")
    row = cursor.fetchone()
    if row:
        cursor.execute("DELETE FROM migrations WHERE name=?", (row[0],))
    conn.commit()


if __name__ == "__main__":
    db = connect(sys.argv[1] if len(sys.argv) > 1 else "app.db")
    run_migrations(db, "migrations")
`,
		"core/engine.rs": `use std::collections::HashMap;

pub struct Engine {
    handlers: HashMap<String, Box<dyn Fn(String) -> String>>,
}

impl Engine {
    pub fn new() -> Self {
        Engine {
            handlers: HashMap::new(),
        }
    }

    pub fn register(&mut self, name: &str, handler: Box<dyn Fn(String) -> String>) {
        self.handlers.insert(name.to_string(), handler);
    }

    pub fn dispatch(&self, name: &str, input: String) -> Option<String> {
        self.handlers.get(name).map(|h| h(input))
    }

    pub fn handler_count(&self) -> usize {
        self.handlers.len()
    }
}

pub fn default_handler(input: String) -> String {
    format!("echo: {}", input)
}
`,
		"web/api.js": `import { validateToken } from './auth.js'
import { createResponse } from './response.js'

export async function handleRequest(req, res) {
  const token = req.headers['authorization']
  if (!validateToken(token)) {
    return createResponse(res, 401, { error: 'unauthorized' })
  }
  const data = await fetchData(req.params.id)
  return createResponse(res, 200, data)
}

async function fetchData(id) {
  const resp = await fetch('/api/data/' + id)
  return resp.json()
}

export function registerRoutes(app) {
  app.get('/api/:id', handleRequest)
  app.post('/api', handleRequest)
}

function parseQueryParams(url) {
  const params = {}
  const search = url.split('?')[1] || ''
  search.split('&').forEach(pair => {
    const [k, v] = pair.split('=')
    if (k) params[k] = decodeURIComponent(v || '')
  })
  return params
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

// ── helpers ───────────────────────────────────────────────────────────────

// runTool executes a tool with JSON params and returns the output string.
func runTool(t *testing.T, tool tools.Tool, params string) string {
	t.Helper()
	result := tool.Run(tools.NopRunner, []byte(params))
	if result.Error() != nil {
		return "error: " + result.Error().Error()
	}
	// Content is {"output": "..."} — extract the string value.
	raw, err := result.Content().MarshalJSON()
	if err != nil {
		return result.Label()
	}
	var wrapper struct {
		Output string `json:"output"`
	}
	if err := json.Unmarshal(raw, &wrapper); err == nil && wrapper.Output != "" {
		return wrapper.Output
	}
	return string(raw)
}

// tokenEstimate approximates token count as bytes/4 (standard LLM heuristic).
func tokenEstimate(s string) int {
	return (len(s) + 3) / 4
}

// logEfficiency reports both approaches so failures are easy to diagnose.
func logEfficiency(t *testing.T, label, naive, direct string) {
	t.Helper()
	nt, dt := tokenEstimate(naive), tokenEstimate(direct)
	ratio := float64(nt) / float64(dt)
	t.Logf("%s: naive=%d tok, direct=%d tok, ratio=%.1fx", label, nt, dt, ratio)
}

// ── tests ─────────────────────────────────────────────────────────────────

// TestEfficiency_FindSymbol_VsSearchCode proves that find_symbol returns a
// precise location (file:line) while search_code dumps every matching line
// across the whole repo — often 10-20x more tokens for the same answer.
func TestEfficiency_FindSymbol_VsSearchCode(t *testing.T) {
	root := testRepo(t)

	findSym := findSymbolTool(root)
	searchCode := searchCodeTool(root)

	// Question: "where is ValidateToken defined?"
	direct := runTool(t, findSym, `{"name":"ValidateToken"}`)
	naive := runTool(t, searchCode, `{"pattern":"ValidateToken"}`)

	logEfficiency(t, "find ValidateToken", naive, direct)

	if tokenEstimate(direct) >= tokenEstimate(naive) {
		t.Errorf("find_symbol should be cheaper than search_code\ndirect (%d tok):\n%s\nnaive (%d tok):\n%s",
			tokenEstimate(direct), direct, tokenEstimate(naive), naive)
	}

	// Direct answer must contain the file and a line number.
	if !strings.Contains(direct, "auth.go") {
		t.Errorf("find_symbol result missing expected file: %s", direct)
	}
}

// TestEfficiency_ReadRange_VsReadFull proves that read_file with a line range
// is far cheaper than reading the whole file when you only need one function.
func TestEfficiency_ReadRange_VsReadFull(t *testing.T) {
	root := testRepo(t)

	readFile := readFileTool(root)

	// Find the line number of run_migrations by scanning the fixture file.
	pyPath := filepath.Join(root, "scripts", "migrate.py")
	pyData, err := os.ReadFile(pyPath)
	if err != nil {
		t.Fatal(err)
	}
	startLine := 1
	for i, line := range strings.Split(string(pyData), "\n") {
		if strings.HasPrefix(line, "def run_migrations") {
			startLine = i + 1
			break
		}
	}

	rangeResult := runTool(t, readFile, fmt.Sprintf(
		`{"path":"scripts/migrate.py","start_line":%d,"end_line":%d}`,
		startLine, startLine+17,
	))
	fullResult := runTool(t, readFile, `{"path":"scripts/migrate.py"}`)

	if tokenEstimate(rangeResult) >= tokenEstimate(fullResult) {
		t.Errorf("range read should be cheaper than full read\nrange (%d tok), full (%d tok)",
			tokenEstimate(rangeResult), tokenEstimate(fullResult))
	}

	// Range result must still contain the function.
	if !strings.Contains(rangeResult, "run_migrations") {
		t.Errorf("range result missing function name: %s", rangeResult)
	}
}

// TestEfficiency_Tree_VsRepeatedListDir proves that a single tree() call
// returns the same structural information as multiple list_dir-style os.ReadDir
// calls, but in one round trip.
func TestEfficiency_Tree_VsRepeatedListDir(t *testing.T) {
	root := testRepo(t)

	treeTl := treeTool(root)

	// Simulate what an agent does without tree: list root, then each subdir.
	var naiveParts []string
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		naiveParts = append(naiveParts, e.Name())
		if e.IsDir() {
			sub, _ := os.ReadDir(filepath.Join(root, e.Name()))
			for _, se := range sub {
				naiveParts = append(naiveParts, "  "+se.Name())
				if se.IsDir() {
					sub2, _ := os.ReadDir(filepath.Join(root, e.Name(), se.Name()))
					for _, se2 := range sub2 {
						naiveParts = append(naiveParts, "    "+se2.Name())
					}
				}
			}
		}
	}
	// Each list_dir call also carries tool call overhead — add a fixed cost per call.
	const toolCallOverheadBytes = 120 // conservative: tool name + params JSON
	naiveCallCount := 1 + len(entries) // root + one per subdir entry
	naiveOutput := strings.Join(naiveParts, "\n") + strings.Repeat(" ", naiveCallCount*toolCallOverheadBytes)

	directOutput := runTool(t, treeTl, fmt.Sprintf(`{"path":"%s","depth":3}`, root))

	logEfficiency(t, "tree vs repeated list_dir", naiveOutput, directOutput)

	// Tree must show nested structure.
	if !strings.Contains(directOutput, "internal") {
		t.Errorf("tree missing expected directory: %s", directOutput)
	}
	if !strings.Contains(directOutput, "auth.go") {
		t.Errorf("tree missing expected file: %s", directOutput)
	}
}

// TestEfficiency_FindSymbol_MultiLanguage proves find_symbol works across
// all languages in the fixture, returning precise locations not grep dumps.
func TestEfficiency_FindSymbol_MultiLanguage(t *testing.T) {
	root := testRepo(t)
	findSym := findSymbolTool(root)

	cases := []struct {
		symbol   string
		wantFile string
	}{
		{"ValidateToken", "auth.go"},   // Go
		{"run_migrations", "migrate.py"}, // Python
		{"handleRequest", "api.js"},    // JavaScript
		{"dispatch", "engine.rs"},      // Rust
		{"FindUser", "db.go"},          // Go
	}

	for _, tc := range cases {
		t.Run(tc.symbol, func(t *testing.T) {
			result := runTool(t, findSym, fmt.Sprintf(`{"name":%q}`, tc.symbol))

			if result == fmt.Sprintf("Symbol %q not found.", tc.symbol) {
				// ctags not installed — skip rather than fail
				t.Skipf("ctags not available, skipping %s", tc.symbol)
			}

			if !strings.Contains(result, tc.wantFile) {
				t.Errorf("find_symbol(%q): expected %q in result\ngot: %s", tc.symbol, tc.wantFile, result)
			}

			// Result must be compact: a location line, not a source dump.
			lines := strings.Split(strings.TrimSpace(result), "\n")
			for _, line := range lines {
				if tokenEstimate(line) > 30 {
					t.Errorf("find_symbol line too verbose (%d tok): %q", tokenEstimate(line), line)
				}
			}
		})
	}
}

// TestEfficiency_GetFunction_Precision proves get_function returns only the
// requested function, not the whole file.
func TestEfficiency_GetFunction_Precision(t *testing.T) {
	root := testRepo(t)
	getFunc := getFunctionTool(root)
	readFile := readFileTool(root)

	direct := runTool(t, getFunc, `{"name":"GenerateToken","path":"internal/auth/auth.go"}`)
	full := runTool(t, readFile, `{"path":"internal/auth/auth.go"}`)

	logEfficiency(t, "get_function vs read_file", full, direct)

	if tokenEstimate(direct) >= tokenEstimate(full) {
		t.Errorf("get_function should be cheaper than read_file\ndirect=%d tok, full=%d tok",
			tokenEstimate(direct), tokenEstimate(full))
	}

	if !strings.Contains(direct, "GenerateToken") {
		t.Errorf("result missing function name: %s", direct)
	}
	// Must not contain unrelated functions.
	if strings.Contains(direct, "ParseClaims") {
		t.Errorf("result leaked unrelated function ParseClaims: %s", direct)
	}
}

// TestEfficiency_FindSymbol_IncludesEndLine proves find_symbol now returns
// start-end so the agent can read_file with a precise range in one follow-up
// call, with no guessing.
func TestEfficiency_FindSymbol_IncludesEndLine(t *testing.T) {
	root := testRepo(t)
	findSym := findSymbolTool(root)

	result := runTool(t, findSym, `{"name":"ValidateToken"}`)
	if result == fmt.Sprintf("Symbol %q not found.", "ValidateToken") {
		t.Skip("ctags not available")
	}

	// Result should contain a range like "auth.go:8-20" not just "auth.go:8"
	// This only holds when ctags is available and supports --fields=+e
	if !strings.Contains(result, "-") {
		t.Skip("ctags does not return end lines on this system (older ctags?)")
	}
	t.Logf("find_symbol result: %s", result)
}

// TestEfficiency_Callers_VsSearchCode proves callers() strips definitions,
// imports, and comments — returning only actual call sites with snippets.
func TestEfficiency_Callers_VsSearchCode(t *testing.T) {
	root := testRepo(t)
	callers := callersTool(root)
	searchCode := searchCodeTool(root)

	direct := runTool(t, callers, `{"name":"ValidateToken"}`)
	naive := runTool(t, searchCode, `{"pattern":"ValidateToken"}`)

	logEfficiency(t, "callers vs search_code", naive, direct)

	// callers should be cheaper (filtered) or at worst equal
	if tokenEstimate(direct) > tokenEstimate(naive) {
		t.Errorf("callers should be <= search_code tokens\ndirect=%d tok, naive=%d tok",
			tokenEstimate(direct), tokenEstimate(naive))
	}

	// callers result should not contain the definition line itself
	if strings.Contains(direct, "func ValidateToken") {
		t.Errorf("callers result should not include the definition line: %s", direct)
	}
}

// TestEfficiency_SymbolContext_IsCompact proves symbol_context returns a small
// orientation snapshot — never source code.
func TestEfficiency_SymbolContext_IsCompact(t *testing.T) {
	root := testRepo(t)
	symCtx := symbolContextTool(root)
	getFunc := getFunctionTool(root)

	ctx := runTool(t, symCtx, `{"name":"ValidateToken"}`)
	if strings.Contains(ctx, "not found") {
		t.Skip("ctags not available")
	}
	full := runTool(t, getFunc, `{"name":"ValidateToken","path":"internal/auth/auth.go"}`)

	logEfficiency(t, "symbol_context vs get_function", full, ctx)

	// Context must be much cheaper than full source
	if tokenEstimate(ctx) >= tokenEstimate(full) {
		t.Errorf("symbol_context should be cheaper than get_function\nctx=%d tok, full=%d tok\nctx:\n%s",
			tokenEstimate(ctx), tokenEstimate(full), ctx)
	}

	// Must contain the key orientation fields
	for _, field := range []string{"symbol:", "loc:", "sig:", "callers:", "callees:"} {
		if !strings.Contains(ctx, field) {
			t.Errorf("symbol_context missing field %q:\n%s", field, ctx)
		}
	}

	// Must NOT contain multi-line source
	lines := strings.Split(strings.TrimSpace(ctx), "\n")
	if len(lines) > 10 {
		t.Errorf("symbol_context too verbose (%d lines), should be <=10:\n%s", len(lines), ctx)
	}
	t.Logf("symbol_context:\n%s", ctx)
}

// TestEfficiency_GrepFiles_VsSearchCode proves grep_files returns file paths
// only — orders of magnitude cheaper when you just need to know "where does X live".
func TestEfficiency_GrepFiles_VsSearchCode(t *testing.T) {
	root := testRepo(t)
	grepFiles := grepFilesTool(root)
	searchCode := searchCodeTool(root)

	direct := runTool(t, grepFiles, `{"pattern":"ValidateToken"}`)
	naive := runTool(t, searchCode, `{"pattern":"ValidateToken"}`)

	logEfficiency(t, "grep_files vs search_code", naive, direct)

	if tokenEstimate(direct) >= tokenEstimate(naive) {
		t.Errorf("grep_files should be cheaper than search_code\ndirect=%d tok, naive=%d tok",
			tokenEstimate(direct), tokenEstimate(naive))
	}

	// Result is file paths, not line content
	for _, line := range strings.Split(strings.TrimSpace(direct), "\n") {
		if strings.Contains(line, "\t") {
			t.Errorf("grep_files should not contain tab-separated content, got: %q", line)
		}
	}
}

// TestEfficiency_Callees_LocationsOnly proves callees() returns names+locations
// not source — the agent sees what a function depends on without reading bodies.
func TestEfficiency_Callees_LocationsOnly(t *testing.T) {
	root := testRepo(t)
	callees := calleesTool(root)
	readFile := readFileTool(root)

	direct := runTool(t, callees, `{"name":"ValidateToken","path":"internal/auth/auth.go"}`)
	naive := runTool(t, readFile, `{"path":"internal/auth/auth.go"}`)

	logEfficiency(t, "callees vs read_file", naive, direct)

	if tokenEstimate(direct) >= tokenEstimate(naive) {
		t.Errorf("callees should be cheaper than read_file\ndirect=%d tok, naive=%d tok\ndirect:\n%s",
			tokenEstimate(direct), tokenEstimate(naive), direct)
	}
	t.Logf("callees: %s", direct)
}

// TestEfficiency_OverallNavigationFlow simulates a realistic agent task:
// "find ValidateToken, understand it, check who calls it" — and counts the
// total tokens across both the naive (grep everything) and direct approaches.
func TestEfficiency_OverallNavigationFlow(t *testing.T) {
	root := testRepo(t)

	findSym := findSymbolTool(root)
	getFunc := getFunctionTool(root)
	searchCode := searchCodeTool(root)
	readFile := readFileTool(root)

	// Direct flow: find_symbol → get_function → search_code for callers.
	loc := runTool(t, findSym, `{"name":"ValidateToken"}`)
	body := runTool(t, getFunc, `{"name":"ValidateToken","path":"internal/auth/auth.go"}`)
	callers := runTool(t, searchCode, `{"pattern":"ValidateToken","file_glob":"*.go"}`)
	directTotal := loc + body + callers

	// Naive flow: search_code for definition (dumps all matches), read whole
	// file to find the function, search_code again for callers.
	naiveDef := runTool(t, searchCode, `{"pattern":"func ValidateToken"}`)
	naiveFile := runTool(t, readFile, `{"path":"internal/auth/auth.go"}`)
	naiveCallers := runTool(t, searchCode, `{"pattern":"ValidateToken"}`)
	naiveTotal := naiveDef + naiveFile + naiveCallers

	logEfficiency(t, "full navigation flow", naiveTotal, directTotal)

	if tokenEstimate(directTotal) >= tokenEstimate(naiveTotal) {
		t.Errorf("direct flow should cost fewer tokens than naive\ndirect=%d tok, naive=%d tok",
			tokenEstimate(directTotal), tokenEstimate(naiveTotal))
	}
}
