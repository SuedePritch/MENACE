package indexer

import (
	"os"
	"path/filepath"
	"testing"
)

// helper: parse source into symbols with a temp file so FindPatternDuplicates can read it.
func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func makeFileSymbols(path string, syms []Symbol) map[string][]Symbol {
	return map[string][]Symbol{path: syms}
}

// --- Test 1: Basic cross-function duplicate ---

func TestPatternBasicCrossFunctionDuplicate(t *testing.T) {
	src := `
function alpha(a: any) {
  const x = a.name.trim().toLowerCase();
  const y = x.replace(/bad/g, "");
  console.log("done", y);
  return y;
}

function beta(b: any) {
  const x = b.name.trim().toLowerCase();
  const y = x.replace(/bad/g, "");
  console.log("done", y);
  return y;
}
`
	path := writeTemp(t, "basic.ts", src)
	syms, err := ParseFile(path, []byte(src))
	if err != nil {
		t.Fatal(err)
	}

	cfg := DefaultPatternConfig()
	cfg.MinNodeCount = 1 // lower threshold for test
	groups := FindPatternDuplicates(makeFileSymbols(path, syms), cfg)

	if len(groups) == 0 {
		t.Fatal("expected at least one pattern group, got 0")
	}

	// The largest matching group should have 2 occurrences from different functions.
	found := false
	for _, g := range groups {
		if len(g.Occurrences) >= 2 {
			funcs := map[string]bool{}
			for _, o := range g.Occurrences {
				funcs[o.FunctionName] = true
			}
			if len(funcs) >= 2 {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("expected a pattern group with occurrences in at least 2 different functions")
	}
}

// --- Test 2: Same structure, different variable names → same hash ---

func TestPatternDifferentVarNamesSameHash(t *testing.T) {
	src := `
function first(a: any) {
  const foo = a.value.trim().toLowerCase();
  const bar = foo.replace(/x/g, "");
  console.log("result", bar);
  return bar;
}

function second(b: any) {
  const qux = b.value.trim().toLowerCase();
  const baz = qux.replace(/x/g, "");
  console.log("result", baz);
  return baz;
}
`
	path := writeTemp(t, "varnames.ts", src)
	syms, err := ParseFile(path, []byte(src))
	if err != nil {
		t.Fatal(err)
	}

	cfg := DefaultPatternConfig()
	cfg.MinNodeCount = 1
	groups := FindPatternDuplicates(makeFileSymbols(path, syms), cfg)

	if len(groups) == 0 {
		t.Fatal("expected pattern groups despite different variable names")
	}
}

// --- Test 3: Different structure → no match ---

func TestPatternDifferentStructureNoMatch(t *testing.T) {
	src := `
function one(a: any) {
  const x = a.name.trim();
  console.log(x);
  return x;
}

function two(b: any) {
  const parts = b.name.split(",");
  const mapped = parts.map((p: string) => p.trim());
  return mapped.join(" | ");
}
`
	path := writeTemp(t, "diffstruct.ts", src)
	syms, err := ParseFile(path, []byte(src))
	if err != nil {
		t.Fatal(err)
	}

	cfg := DefaultPatternConfig()
	cfg.MinNodeCount = 1
	groups := FindPatternDuplicates(makeFileSymbols(path, syms), cfg)

	// Should find no cross-function duplicates
	for _, g := range groups {
		funcs := map[string]bool{}
		for _, o := range g.Occurrences {
			funcs[o.FunctionName] = true
		}
		if len(funcs) > 1 {
			t.Errorf("unexpected cross-function match with hash %s", g.StructuralHash)
		}
	}
}

// --- Test 4: Nested block detection (pattern inside if bodies) ---

func TestPatternNestedBlockDetection(t *testing.T) {
	src := `
function outer1(a: any) {
  if (a.ok) {
    const x = a.name.trim().toLowerCase();
    const y = x.replace(/bad/g, "");
    console.log("done", y);
    return y;
  }
  return null;
}

function outer2(b: any) {
  const x = b.name.trim().toLowerCase();
  const y = x.replace(/bad/g, "");
  console.log("done", y);
  return y;
}
`
	path := writeTemp(t, "nested.ts", src)
	syms, err := ParseFile(path, []byte(src))
	if err != nil {
		t.Fatal(err)
	}

	cfg := DefaultPatternConfig()
	cfg.MinNodeCount = 1
	groups := FindPatternDuplicates(makeFileSymbols(path, syms), cfg)

	found := false
	for _, g := range groups {
		funcs := map[string]bool{}
		for _, o := range g.Occurrences {
			funcs[o.FunctionName] = true
		}
		if len(funcs) >= 2 {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected cross-function pattern match including nested block")
	}
}

// --- Test 5: Subsumption (5-stmt match subsumes 3-stmt match) ---

func TestPatternSubsumption(t *testing.T) {
	src := `
function funcA(a: any) {
  const x = a.name.trim().toLowerCase();
  const y = x.replace(/bad/g, "");
  console.log("step1", y);
  console.log("step2", y);
  console.log("step3", y);
  return y;
}

function funcB(b: any) {
  const x = b.name.trim().toLowerCase();
  const y = x.replace(/bad/g, "");
  console.log("step1", y);
  console.log("step2", y);
  console.log("step3", y);
  return y;
}
`
	path := writeTemp(t, "subsume.ts", src)
	syms, err := ParseFile(path, []byte(src))
	if err != nil {
		t.Fatal(err)
	}

	cfg := DefaultPatternConfig()
	cfg.MinNodeCount = 1
	groups := FindPatternDuplicates(makeFileSymbols(path, syms), cfg)

	// Should not have a 3-stmt group that is fully covered by a larger group.
	// All groups that remain should either be the largest or not subsumed.
	for _, g := range groups {
		if g.StatementCount < 4 {
			// Check this small group isn't fully inside a bigger one
			for _, big := range groups {
				if big.StatementCount <= g.StatementCount {
					continue
				}
				allCovered := true
				for _, occ := range g.Occurrences {
					covered := false
					for _, bigOcc := range big.Occurrences {
						if bigOcc.FilePath == occ.FilePath &&
							bigOcc.FunctionName == occ.FunctionName &&
							bigOcc.StartLine <= occ.StartLine &&
							bigOcc.EndLine >= occ.EndLine {
							covered = true
							break
						}
					}
					if !covered {
						allCovered = false
						break
					}
				}
				if allCovered {
					t.Errorf("pattern (stmts=%d, hash=%s) should have been subsumed by larger pattern (stmts=%d, hash=%s)",
						g.StatementCount, g.StructuralHash, big.StatementCount, big.StructuralHash)
				}
			}
		}
	}
}

// --- Test 6: MinStatements threshold (2-stmt duplicate skipped) ---

func TestPatternMinStatementsThreshold(t *testing.T) {
	src := `
function a1(x: any) {
  console.log(x);
  return x;
}

function a2(y: any) {
  console.log(y);
  return y;
}
`
	path := writeTemp(t, "minstmt.ts", src)
	syms, err := ParseFile(path, []byte(src))
	if err != nil {
		t.Fatal(err)
	}

	cfg := DefaultPatternConfig()
	cfg.MinStatements = 3 // the functions only have 2 statements each
	cfg.MinNodeCount = 1
	groups := FindPatternDuplicates(makeFileSymbols(path, syms), cfg)

	for _, g := range groups {
		funcs := map[string]bool{}
		for _, o := range g.Occurrences {
			funcs[o.FunctionName] = true
		}
		if len(funcs) > 1 {
			t.Errorf("should not find cross-function match with only 2 statements per function (min=3)")
		}
	}
}

// --- Test 7: MinNodeCount threshold (trivial statements skipped) ---

func TestPatternMinNodeCountThreshold(t *testing.T) {
	src := `
function inc1(a: number) {
  a++;
  a++;
  a++;
}

function inc2(b: number) {
  b++;
  b++;
  b++;
}
`
	path := writeTemp(t, "minnodes.ts", src)
	syms, err := ParseFile(path, []byte(src))
	if err != nil {
		t.Fatal(err)
	}

	// With a very high MinNodeCount, trivial patterns should be filtered out
	cfg := DefaultPatternConfig()
	cfg.MinNodeCount = 50
	groups := FindPatternDuplicates(makeFileSymbols(path, syms), cfg)

	for _, g := range groups {
		funcs := map[string]bool{}
		for _, o := range g.Occurrences {
			funcs[o.FunctionName] = true
		}
		if len(funcs) > 1 {
			t.Errorf("trivial statements should be filtered by MinNodeCount=50, got match with hash %s", g.StructuralHash)
		}
	}

	// With a low MinNodeCount, should find matches
	cfg.MinNodeCount = 1
	groups = FindPatternDuplicates(makeFileSymbols(path, syms), cfg)
	found := false
	for _, g := range groups {
		funcs := map[string]bool{}
		for _, o := range g.Occurrences {
			funcs[o.FunctionName] = true
		}
		if len(funcs) > 1 {
			found = true
			break
		}
	}
	if !found {
		t.Error("with MinNodeCount=1, expected to find cross-function matches for trivial statements")
	}
}

// --- Test 8: CrossFunctionOnly (same-function repeats excluded) ---

func TestPatternCrossFunctionOnly(t *testing.T) {
	src := `
function repeater(a: any) {
  const x = a.name.trim().toLowerCase();
  const y = x.replace(/bad/g, "");
  console.log("done", y);

  const x2 = a.name.trim().toLowerCase();
  const y2 = x2.replace(/bad/g, "");
  console.log("done", y2);
}
`
	path := writeTemp(t, "samefunc.ts", src)
	syms, err := ParseFile(path, []byte(src))
	if err != nil {
		t.Fatal(err)
	}

	cfg := DefaultPatternConfig()
	cfg.MinNodeCount = 1
	cfg.CrossFunctionOnly = true
	groups := FindPatternDuplicates(makeFileSymbols(path, syms), cfg)

	if len(groups) != 0 {
		t.Errorf("CrossFunctionOnly should filter same-function duplicates, got %d groups", len(groups))
	}

	// With CrossFunctionOnly disabled, should find matches
	cfg.CrossFunctionOnly = false
	groups = FindPatternDuplicates(makeFileSymbols(path, syms), cfg)
	if len(groups) == 0 {
		t.Error("with CrossFunctionOnly=false, expected same-function duplicates")
	}
}

// --- Test 9: Integration test with real testdata fixture ---

func TestPatternIntegrationWithFixture(t *testing.T) {
	fixturePath := "../../testdata/pattern_duplicates.ts"
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Skipf("testdata fixture not found: %v", err)
	}

	syms, err := ParseFile(fixturePath, data)
	if err != nil {
		t.Fatal(err)
	}

	cfg := DefaultPatternConfig()
	groups := FindPatternDuplicates(makeFileSymbols(fixturePath, syms), cfg)

	if len(groups) == 0 {
		t.Fatal("expected pattern groups from testdata/pattern_duplicates.ts")
	}

	// The fixture has 5 functions (processUser, processProduct, processCategory,
	// handleRequest, handleEvent) sharing the same validation+transform pattern.
	// At least one group should have ≥3 occurrences.
	foundLargeGroup := false
	for _, g := range groups {
		if len(g.Occurrences) >= 3 {
			foundLargeGroup = true
			break
		}
	}
	if !foundLargeGroup {
		t.Error("expected at least one pattern group with ≥3 occurrences")
	}

	// Verify ExampleSnippet is populated.
	for _, g := range groups {
		if g.ExampleSnippet == "" {
			t.Errorf("pattern group %s missing ExampleSnippet", g.StructuralHash)
		}
	}
}
