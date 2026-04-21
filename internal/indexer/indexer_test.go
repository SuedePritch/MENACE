package indexer

import (
	"os"
	"path/filepath"
	"testing"
)

// ── Mock Indexer ────────────────────────────────────────────────────────────

type mockIndexer struct {
	exts    []string
	symbols []Symbol
	err     error
}

func (m *mockIndexer) Extensions() []string { return m.exts }

func (m *mockIndexer) IndexDir(dir string, workers int) (*Report, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &Report{Symbols: m.symbols, TokenEstimate: 100}, nil
}

func (m *mockIndexer) SymbolsInFile(filePath string) ([]Symbol, error) {
	if m.err != nil {
		return nil, m.err
	}
	var matched []Symbol
	for _, s := range m.symbols {
		if s.FilePath == filePath {
			matched = append(matched, s)
		}
	}
	return matched, nil
}

func (m *mockIndexer) FindSymbol(name string, filePath string) ([]Symbol, error) {
	if m.err != nil {
		return nil, m.err
	}
	var matched []Symbol
	for _, s := range m.symbols {
		if s.Name == name && (filePath == "" || s.FilePath == filePath) {
			matched = append(matched, s)
		}
	}
	return matched, nil
}

// ── Registry Tests ─────────────────────────────────────────────────────────

func TestRegisterValidIndexer(t *testing.T) {
	Reset()

	idx := &mockIndexer{
		exts: []string{".test"},
		symbols: []Symbol{
			{Name: "test", Kind: "function", FilePath: "test.test", StartLine: 1, EndLine: 3},
		},
	}

	err := Register(idx)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	got := ForFile("something.test")
	if got == nil {
		t.Fatal("expected indexer for .test, got nil")
	}

	statuses := Statuses()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if !statuses[0].Healthy {
		t.Fatalf("expected healthy, got error: %s", statuses[0].Error)
	}
}

func TestRegisterNoExtensions(t *testing.T) {
	Reset()

	idx := &mockIndexer{exts: []string{}}
	err := Register(idx)
	if err == nil {
		t.Fatal("expected error for empty extensions")
	}
}

func TestRegisterBadExtension(t *testing.T) {
	Reset()

	idx := &mockIndexer{exts: []string{"go"}} // missing dot
	err := Register(idx)
	if err == nil {
		t.Fatal("expected error for extension without dot")
	}
}

func TestRegisterBrokenIndexer(t *testing.T) {
	Reset()

	idx := &mockIndexer{
		exts: []string{".broken"},
		err:  os.ErrNotExist, // will fail smoke test
	}

	err := Register(idx)
	if err != nil {
		t.Fatalf("Register should succeed even for broken indexer, got: %v", err)
	}

	// Should be registered but unhealthy
	statuses := Statuses()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].Healthy {
		t.Fatal("expected unhealthy status for broken indexer")
	}

	// ForFile should NOT return unhealthy indexers
	got := ForFile("test.broken")
	if got != nil {
		t.Fatal("ForFile should return nil for unhealthy indexer")
	}
}

func TestForFileUnregistered(t *testing.T) {
	Reset()

	got := ForFile("test.xyz")
	if got != nil {
		t.Fatal("expected nil for unregistered extension")
	}
}

func TestRegistryOverride(t *testing.T) {
	Reset()

	idx1 := &mockIndexer{
		exts:    []string{".ts"},
		symbols: []Symbol{{Name: "first", Kind: "function"}},
	}
	idx2 := &mockIndexer{
		exts:    []string{".ts"},
		symbols: []Symbol{{Name: "second", Kind: "function"}},
	}

	Register(idx1)
	Register(idx2)

	// Second registration should override
	got := ForFile("app.ts")
	if got == nil {
		t.Fatal("expected indexer")
	}
	syms, _ := got.FindSymbol("second", "")
	if len(syms) != 1 {
		t.Fatal("expected second indexer to override first")
	}
}

func TestAllDeduplicates(t *testing.T) {
	Reset()

	idx := &mockIndexer{
		exts:    []string{".ts", ".tsx", ".js", ".jsx"},
		symbols: []Symbol{},
	}
	Register(idx)

	all := All()
	if len(all) != 1 {
		t.Fatalf("expected 1 unique indexer, got %d", len(all))
	}
}

// ── Interface Contract Tests ───────────────────────────────────────────────

func TestIndexerInterfaceContract(t *testing.T) {
	// Any Indexer implementation must satisfy these behaviors
	idx := &mockIndexer{
		exts: []string{".mock"},
		symbols: []Symbol{
			{Name: "Foo", Kind: "function", FilePath: "/a.mock", StartLine: 1, EndLine: 5, Source: "func Foo() {}"},
			{Name: "Bar", Kind: "class", FilePath: "/a.mock", StartLine: 7, EndLine: 20, Source: "class Bar {}"},
			{Name: "Baz", Kind: "function", FilePath: "/b.mock", StartLine: 1, EndLine: 3, Source: "func Baz() {}"},
		},
	}

	// Extensions must be non-empty
	exts := idx.Extensions()
	if len(exts) == 0 {
		t.Fatal("Extensions() must return at least one extension")
	}

	// SymbolsInFile scoped to one file
	syms, err := idx.SymbolsInFile("/a.mock")
	if err != nil {
		t.Fatalf("SymbolsInFile error: %v", err)
	}
	if len(syms) != 2 {
		t.Fatalf("expected 2 symbols in /a.mock, got %d", len(syms))
	}

	// FindSymbol by name
	found, err := idx.FindSymbol("Foo", "")
	if err != nil {
		t.Fatalf("FindSymbol error: %v", err)
	}
	if len(found) != 1 || found[0].Name != "Foo" {
		t.Fatal("FindSymbol should find Foo")
	}

	// FindSymbol scoped to file
	found, err = idx.FindSymbol("Foo", "/b.mock")
	if err != nil {
		t.Fatalf("FindSymbol error: %v", err)
	}
	if len(found) != 0 {
		t.Fatal("FindSymbol scoped to /b.mock should not find Foo")
	}

	// IndexDir
	report, err := idx.IndexDir("/project", 1)
	if err != nil {
		t.Fatalf("IndexDir error: %v", err)
	}
	if len(report.Symbols) != 3 {
		t.Fatalf("expected 3 symbols in report, got %d", len(report.Symbols))
	}
}

// ── External Indexer Protocol Tests ────────────────────────────────────────

func TestExternalIndexerMissingBinary(t *testing.T) {
	_, err := NewExternalIndexer("/nonexistent/binary")
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func TestExternalIndexerProtocol(t *testing.T) {
	// Create a mock external indexer as a shell script
	dir := t.TempDir()
	script := filepath.Join(dir, "mock-indexer")

	scriptContent := `#!/bin/sh
case "$1" in
  extensions)
    echo '[".mock"]'
    ;;
  symbols)
    echo '[{"name":"hello","kind":"function","filePath":"'$2'","startLine":1,"endLine":3,"source":"func hello() {}"}]'
    ;;
  index)
    echo '{"symbols":[{"name":"hello","kind":"function","filePath":"test.mock","startLine":1,"endLine":3,"source":"func hello() {}"}],"tokenEstimate":50}'
    ;;
  find)
    if [ "$2" = "hello" ]; then
      echo '[{"name":"hello","kind":"function","filePath":"test.mock","startLine":1,"endLine":3,"source":"func hello() {}"}]'
    else
      echo '[]'
    fi
    ;;
  *)
    echo "unknown command: $1" >&2
    exit 1
    ;;
esac
`
	os.WriteFile(script, []byte(scriptContent), 0755)

	// Create the external indexer
	idx, err := NewExternalIndexer(script)
	if err != nil {
		t.Fatalf("NewExternalIndexer error: %v", err)
	}

	// Test Extensions
	exts := idx.Extensions()
	if len(exts) != 1 || exts[0] != ".mock" {
		t.Fatalf("expected [.mock], got %v", exts)
	}

	// Test SymbolsInFile
	syms, err := idx.SymbolsInFile("test.mock")
	if err != nil {
		t.Fatalf("SymbolsInFile error: %v", err)
	}
	if len(syms) != 1 || syms[0].Name != "hello" {
		t.Fatalf("expected 1 symbol 'hello', got %v", syms)
	}
	if syms[0].Kind != "function" {
		t.Fatalf("expected kind 'function', got %q", syms[0].Kind)
	}
	if syms[0].StartLine != 1 || syms[0].EndLine != 3 {
		t.Fatalf("expected lines 1-3, got %d-%d", syms[0].StartLine, syms[0].EndLine)
	}

	// Test IndexDir
	report, err := idx.IndexDir(dir, 1)
	if err != nil {
		t.Fatalf("IndexDir error: %v", err)
	}
	if len(report.Symbols) != 1 {
		t.Fatalf("expected 1 symbol in report, got %d", len(report.Symbols))
	}
	if report.TokenEstimate != 50 {
		t.Fatalf("expected tokenEstimate 50, got %d", report.TokenEstimate)
	}

	// Test FindSymbol — found
	found, err := idx.FindSymbol("hello", "")
	if err != nil {
		t.Fatalf("FindSymbol error: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("expected 1 match, got %d", len(found))
	}

	// Test FindSymbol — not found
	found, err = idx.FindSymbol("missing", "")
	if err != nil {
		t.Fatalf("FindSymbol error: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(found))
	}
}

func TestExternalIndexerBadJSON(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "bad-indexer")

	// Returns valid extensions but garbage for symbols
	scriptContent := `#!/bin/sh
case "$1" in
  extensions)
    echo '[".bad"]'
    ;;
  symbols)
    echo 'this is not json'
    ;;
  *)
    exit 1
    ;;
esac
`
	os.WriteFile(script, []byte(scriptContent), 0755)

	idx, err := NewExternalIndexer(script)
	if err != nil {
		t.Fatalf("NewExternalIndexer should succeed: %v", err)
	}

	_, err = idx.SymbolsInFile("test.bad")
	if err == nil {
		t.Fatal("expected error for bad JSON")
	}
}

func TestExternalIndexerNonZeroExit(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "crash-indexer")

	scriptContent := `#!/bin/sh
case "$1" in
  extensions)
    echo '[".crash"]'
    ;;
  *)
    echo "crash!" >&2
    exit 1
    ;;
esac
`
	os.WriteFile(script, []byte(scriptContent), 0755)

	idx, err := NewExternalIndexer(script)
	if err != nil {
		t.Fatalf("NewExternalIndexer should succeed: %v", err)
	}

	_, err = idx.SymbolsInFile("test.crash")
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}
}

// ── Registration + External Integration ────────────────────────────────────

func TestExternalIndexerRegistration(t *testing.T) {
	Reset()

	dir := t.TempDir()
	script := filepath.Join(dir, "good-indexer")

	scriptContent := `#!/bin/sh
case "$1" in
  extensions)
    echo '[".good"]'
    ;;
  symbols)
    echo '[{"name":"test","kind":"function","filePath":"'$2'","startLine":1,"endLine":1,"source":"x"}]'
    ;;
  *)
    echo '{"symbols":[],"tokenEstimate":0}'
    ;;
esac
`
	os.WriteFile(script, []byte(scriptContent), 0755)

	idx, err := NewExternalIndexer(script)
	if err != nil {
		t.Fatalf("NewExternalIndexer: %v", err)
	}

	err = Register(idx)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Should be findable
	got := ForFile("app.good")
	if got == nil {
		t.Fatal("expected indexer for .good")
	}

	// Should be healthy
	statuses := Statuses()
	found := false
	for _, s := range statuses {
		if s.Name != "" && s.Healthy {
			found = true
		}
	}
	if !found {
		t.Fatal("expected healthy external indexer in statuses")
	}
}

// ── Built-in TS Indexer Tests ──────────────────────────────────────────────

func TestBuiltinTSIndexerExtensions(t *testing.T) {
	idx := NewBuiltinTSIndexer()
	exts := idx.Extensions()

	expected := map[string]bool{".ts": true, ".tsx": true, ".js": true, ".jsx": true}
	for _, ext := range exts {
		if !expected[ext] {
			t.Fatalf("unexpected extension: %s", ext)
		}
	}
	if len(exts) != 4 {
		t.Fatalf("expected 4 extensions, got %d", len(exts))
	}
}

func TestBuiltinTSIndexerSymbolsInFile(t *testing.T) {
	idx := NewBuiltinTSIndexer()

	// Create a temp TS file
	dir := t.TempDir()
	tsFile := filepath.Join(dir, "test.ts")
	os.WriteFile(tsFile, []byte(`
export function greet(name: string): string {
  return "hello " + name;
}

export class Greeter {
  name: string;
  constructor(name: string) {
    this.name = name;
  }
  greet(): string {
    return "hello " + this.name;
  }
}
`), 0644)

	syms, err := idx.SymbolsInFile(tsFile)
	if err != nil {
		t.Fatalf("SymbolsInFile error: %v", err)
	}

	if len(syms) == 0 {
		t.Fatal("expected symbols, got none")
	}

	// Should find greet function and Greeter class
	foundFunc := false
	foundClass := false
	for _, s := range syms {
		if s.Name == "greet" && s.Kind == "function" {
			foundFunc = true
			if s.Source == "" {
				t.Fatal("expected non-empty source for greet")
			}
		}
		if s.Name == "Greeter" && s.Kind == "class" {
			foundClass = true
		}
	}
	if !foundFunc {
		t.Fatal("expected to find greet function")
	}
	if !foundClass {
		t.Fatal("expected to find Greeter class")
	}
}

func TestBuiltinTSIndexerFindSymbol(t *testing.T) {
	idx := NewBuiltinTSIndexer()

	dir := t.TempDir()
	tsFile := filepath.Join(dir, "find.ts")
	os.WriteFile(tsFile, []byte(`
function alpha() { return 1; }
function beta() { return 2; }
`), 0644)

	// Index the file first so FindSymbol has data
	idx.SymbolsInFile(tsFile)

	found, err := idx.FindSymbol("alpha", tsFile)
	if err != nil {
		t.Fatalf("FindSymbol error: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("expected 1 match for alpha, got %d", len(found))
	}
	if found[0].Name != "alpha" {
		t.Fatalf("expected alpha, got %s", found[0].Name)
	}

	// Not found
	found, err = idx.FindSymbol("gamma", tsFile)
	if err != nil {
		t.Fatalf("FindSymbol error: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("expected 0 matches for gamma, got %d", len(found))
	}
}

func TestBuiltinTSIndexerRegistration(t *testing.T) {
	Reset()

	idx := NewBuiltinTSIndexer()
	err := Register(idx)
	if err != nil {
		t.Fatalf("Register built-in: %v", err)
	}

	// Should handle .ts files
	got := ForFile("app.ts")
	if got == nil {
		t.Fatal("expected indexer for .ts after registration")
	}

	// Should be healthy
	statuses := Statuses()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if !statuses[0].Healthy {
		t.Fatalf("expected healthy, got: %s", statuses[0].Error)
	}
}
