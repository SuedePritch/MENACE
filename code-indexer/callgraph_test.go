package indexer

import (
	"os"
	"testing"
)

func TestDirectCall(t *testing.T) {
	code := `function a() { b(); }
function b() { return 1; }`
	entries, err := ExtractCallsFromSource([]byte(code), "test.ts")
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, e := range entries {
		if e.Caller == "a" && e.Callee == "b" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a -> b call; entries: %+v", entries)
	}
}

func TestMethodCall(t *testing.T) {
	code := `function doStuff() { obj.method(); }`
	entries, err := ExtractCallsFromSource([]byte(code), "test.ts")
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, e := range entries {
		if e.Callee == "obj.method" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected obj.method call; entries: %+v", entries)
	}
}

func TestPassedReference(t *testing.T) {
	code := `function process() { arr.map(fn); }`
	entries, err := ExtractCallsFromSource([]byte(code), "test.ts")
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, e := range entries {
		if e.Callee == "fn" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'fn' as passed reference; entries: %+v", entries)
	}
}

func TestCrossFileCall(t *testing.T) {
	// Use real testdata files for cross-file call resolution
	importsSrc, err := os.ReadFile("../../testdata/imports.ts")
	if err != nil {
		t.Fatal(err)
	}
	importedSrc, err := os.ReadFile("../../testdata/imported.ts")
	if err != nil {
		t.Fatal(err)
	}

	importSyms, _ := ParseFile("../../testdata/imports.ts", importsSrc)
	importedSyms, _ := ParseFile("../../testdata/imported.ts", importedSrc)

	fileSymbols := map[string][]Symbol{
		"../../testdata/imports.ts":  importSyms,
		"../../testdata/imported.ts": importedSyms,
	}

	imports, _ := ResolveImports("../../testdata/imports.ts", importsSrc)
	fileImports := map[string][]ImportInfo{
		"../../testdata/imports.ts": imports,
	}

	BuildCallGraph(fileSymbols, fileImports)

	// Check that helperFn has dependents after cross-file resolution
	for _, s := range fileSymbols["../../testdata/imported.ts"] {
		if s.Name == "helperFn" {
			if len(s.Dependents) == 0 {
				t.Error("expected helperFn to have dependents after BuildCallGraph")
			}
		}
	}
}

// Design decision test: document what does NOT count as a dependency.
// Dynamic calls (eval, string-based, computed property) are intentionally excluded.
// Rationale: These cannot be statically resolved and would produce false positives.
func TestNonDependencies(t *testing.T) {
	code := `function dynamic() {
  eval("something()");
  const name = "method";
  obj[name]();
  const fn = getFn();
  fn();
}`
	entries, err := ExtractCallsFromSource([]byte(code), "test.ts")
	if err != nil {
		t.Fatal(err)
	}

	// eval is captured as a call (it's a regular call_expression),
	// but string-based references inside eval are NOT.
	// Computed property access obj[name]() is NOT captured as a named call.
	// However fn() IS captured since fn is a regular identifier call.
	for _, e := range entries {
		if e.Callee == "something" {
			t.Error("string inside eval should not be captured as dependency")
		}
	}
}

// --- Dead Code Detection ---

func TestPotentiallyUnusedDetection(t *testing.T) {
	symbols := []Symbol{
		{Name: "used", Kind: SymbolFunction, ExportStatus: ExportUnexported, Dependents: []string{"caller"}},
		{Name: "unused", Kind: SymbolFunction, ExportStatus: ExportUnexported, Dependents: nil},
		{Name: "exported", Kind: SymbolFunction, ExportStatus: ExportNamed, Dependents: nil},
	}

	unused := FindPotentiallyUnused(symbols)
	if len(unused) != 1 || unused[0] != "unused" {
		t.Errorf("expected only 'unused' to be flagged, got %v", unused)
	}
}

func TestExportedNotFlaggedAsUnused(t *testing.T) {
	symbols := []Symbol{
		{Name: "publicApi", Kind: SymbolFunction, ExportStatus: ExportNamed, Dependents: nil},
	}

	unused := FindPotentiallyUnused(symbols)
	if len(unused) != 0 {
		t.Errorf("exported function should not be flagged unused, got %v", unused)
	}
}

// --- Impact Analysis ---

func TestDirectImpact(t *testing.T) {
	fileSymbols := map[string][]Symbol{
		"f.ts": {
			{Name: "B", Kind: SymbolFunction, Dependencies: []string{"C"}},
			{Name: "C", Kind: SymbolFunction},
		},
	}
	chain := AnalyzeImpact("C", fileSymbols, 10)
	found := false
	for _, c := range chain {
		if c.Symbol == "B" && c.Depth == 1 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected B at depth 1; chain: %+v", chain)
	}
}

func TestTransitiveImpact(t *testing.T) {
	fileSymbols := map[string][]Symbol{
		"f.ts": {
			{Name: "A", Kind: SymbolFunction, Dependencies: []string{"B"}},
			{Name: "B", Kind: SymbolFunction, Dependencies: []string{"C"}},
			{Name: "C", Kind: SymbolFunction},
		},
	}
	chain := AnalyzeImpact("C", fileSymbols, 10)

	names := make(map[string]bool)
	for _, c := range chain {
		names[c.Symbol] = true
	}
	if !names["B"] {
		t.Error("expected B in impact chain")
	}
	if !names["A"] {
		t.Error("expected A in impact chain")
	}
}

// --- Duplication Report ---

func TestDuplicateGrouping(t *testing.T) {
	symbols := []Symbol{
		{Name: "a", StructuralHash: "abc123"},
		{Name: "b", StructuralHash: "abc123"},
		{Name: "c", StructuralHash: "def456"},
	}
	groups := FindDuplicateGroups(symbols)
	if len(groups) != 1 {
		t.Errorf("expected 1 duplication group, got %d", len(groups))
	}
	if len(groups) == 1 && len(groups[0].Symbols) != 2 {
		t.Errorf("expected 2 members in group, got %d", len(groups[0].Symbols))
	}
}
