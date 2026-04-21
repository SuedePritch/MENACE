package indexer

import (
	"os"
	"testing"
)

// --- Test helper ---

func parseTS(t *testing.T, code string) []Symbol {
	t.Helper()
	syms, err := ParseSource(code)
	if err != nil {
		t.Fatalf("ParseSource failed: %v", err)
	}
	return syms
}

func findSymbol(syms []Symbol, name string) *Symbol {
	for i := range syms {
		if syms[i].Name == name {
			return &syms[i]
		}
	}
	return nil
}

func requireSymbol(t *testing.T, syms []Symbol, name string) Symbol {
	t.Helper()
	s := findSymbol(syms, name)
	if s == nil {
		names := make([]string, len(syms))
		for i, sym := range syms {
			names[i] = sym.Name
		}
		t.Fatalf("symbol %q not found; have: %v", name, names)
	}
	return *s
}

// --- Bad input test ---

func TestMalformedFileDoesNotPanic(t *testing.T) {
	data, err := os.ReadFile("../../testdata/malformed.ts")
	if err != nil {
		t.Fatalf("read malformed.ts: %v", err)
	}
	// Should not panic — may return symbols or not, but must not error fatally
	_, err = ParseFile("../../testdata/malformed.ts", data)
	if err != nil {
		t.Fatalf("ParseFile returned error on malformed input: %v", err)
	}
}

// --- Phase 2: Functions ---

func TestExtractFunctionName(t *testing.T) {
	syms := parseTS(t, `function myFunc() {}`)
	s := requireSymbol(t, syms, "myFunc")
	if s.Kind != SymbolFunction {
		t.Errorf("expected kind function, got %s", s.Kind)
	}
}

func TestFunctionLineRanges(t *testing.T) {
	code := `function multi(
  a: number,
  b: number
): number {
  return a + b;
}`
	syms := parseTS(t, code)
	s := requireSymbol(t, syms, "multi")
	if s.StartLine != 1 {
		t.Errorf("expected StartLine=1, got %d", s.StartLine)
	}
	if s.EndLine != 6 {
		t.Errorf("expected EndLine=6, got %d", s.EndLine)
	}
}

func TestExportedFunctions(t *testing.T) {
	code := `export function x() {}
export default function y() {}
function z() {}`
	syms := parseTS(t, code)

	x := requireSymbol(t, syms, "x")
	if x.ExportStatus != ExportNamed {
		t.Errorf("x: expected exported, got %s", x.ExportStatus)
	}

	y := requireSymbol(t, syms, "y")
	if y.ExportStatus != ExportDefault {
		t.Errorf("y: expected default, got %s", y.ExportStatus)
	}

	z := requireSymbol(t, syms, "z")
	if z.ExportStatus != ExportUnexported {
		t.Errorf("z: expected unexported, got %s", z.ExportStatus)
	}
}

// --- Phase 2: Arrow Functions ---

func TestBasicArrowFunction(t *testing.T) {
	syms := parseTS(t, `const x = () => {}`)
	s := requireSymbol(t, syms, "x")
	if s.Kind != SymbolFunction {
		t.Errorf("expected function, got %s", s.Kind)
	}
}

func TestAsyncTypedArrow(t *testing.T) {
	syms := parseTS(t, `const x = async (arg: string): Promise<string> => { return arg; }`)
	requireSymbol(t, syms, "x")
}

func TestExportedArrow(t *testing.T) {
	syms := parseTS(t, `export const x = () => {}`)
	s := requireSymbol(t, syms, "x")
	if s.ExportStatus != ExportNamed {
		t.Errorf("expected exported, got %s", s.ExportStatus)
	}
}

func TestNestedArrow(t *testing.T) {
	code := `function outer() {
  const inner = (x: number) => x * 2;
  return inner(5);
}`
	syms := parseTS(t, code)
	// Design decision: we flatten — only top-level symbols are extracted.
	// Nested arrows inside function bodies are NOT indexed as separate symbols.
	requireSymbol(t, syms, "outer")
	if findSymbol(syms, "inner") != nil {
		t.Errorf("nested arrow 'inner' should not be extracted as top-level symbol")
	}
}

// --- Phase 2: Classes ---

func TestClassMethods(t *testing.T) {
	code := `class UserService {
  login(user: string) { return true; }
  logout() {}
}`
	syms := parseTS(t, code)
	requireSymbol(t, syms, "UserService")
	requireSymbol(t, syms, "UserService.login")
	requireSymbol(t, syms, "UserService.logout")
}

func TestClassConstructorStaticGetterSetter(t *testing.T) {
	code := `class Foo {
  constructor() {}
  static create() { return new Foo(); }
  get value() { return 1; }
  set value(v: number) {}
  private secret() {}
  protected internal() {}
}`
	syms := parseTS(t, code)
	requireSymbol(t, syms, "Foo")
	requireSymbol(t, syms, "Foo.constructor")
	requireSymbol(t, syms, "Foo.create")
	requireSymbol(t, syms, "Foo.value") // getter
	requireSymbol(t, syms, "Foo.secret")
	requireSymbol(t, syms, "Foo.internal")
}

func TestAsyncDecoratedMethods(t *testing.T) {
	code := `function log(t: any, k: string, d: PropertyDescriptor) {}
class Svc {
  @log
  async fetch() { return null; }
}`
	syms := parseTS(t, code)
	requireSymbol(t, syms, "Svc.fetch")
}

// --- Phase 2: Types & Interfaces ---

func TestInterfaceDeclaration(t *testing.T) {
	code := `interface User {
  id: number;
  name: string;
}`
	syms := parseTS(t, code)
	s := requireSymbol(t, syms, "User")
	if s.Kind != SymbolInterface {
		t.Errorf("expected interface, got %s", s.Kind)
	}
	if s.StartLine != 1 {
		t.Errorf("expected StartLine=1, got %d", s.StartLine)
	}
}

func TestTypeAliasAndEnum(t *testing.T) {
	code := `type Config = { host: string; };
export type Response = { data: any; };
enum Direction { Up, Down }
export enum Status { OK = 200 }`
	syms := parseTS(t, code)

	cfg := requireSymbol(t, syms, "Config")
	if cfg.Kind != SymbolType {
		t.Errorf("Config: expected type, got %s", cfg.Kind)
	}
	if cfg.ExportStatus != ExportUnexported {
		t.Errorf("Config: expected unexported, got %s", cfg.ExportStatus)
	}

	resp := requireSymbol(t, syms, "Response")
	if resp.ExportStatus != ExportNamed {
		t.Errorf("Response: expected exported, got %s", resp.ExportStatus)
	}

	dir := requireSymbol(t, syms, "Direction")
	if dir.Kind != SymbolEnum {
		t.Errorf("Direction: expected enum, got %s", dir.Kind)
	}

	status := requireSymbol(t, syms, "Status")
	if status.ExportStatus != ExportNamed {
		t.Errorf("Status: expected exported, got %s", status.ExportStatus)
	}
}

// --- Phase 2: Integration — parse all fixtures ---

func TestParseAllFixtures(t *testing.T) {
	fixtures := []struct {
		path    string
		minSyms int
	}{
		{"../../testdata/simple.ts", 3},
		{"../../testdata/arrows.ts", 4},
		{"../../testdata/class.ts", 7},
		{"../../testdata/types.ts", 6},
		{"../../testdata/duplicates.ts", 4},
		{"../../testdata/imported.ts", 2},
		{"../../testdata/imports.ts", 1},
		{"../../testdata/component.tsx", 1},
		{"../../testdata/malformed.ts", 0},
	}
	for _, f := range fixtures {
		t.Run(f.path, func(t *testing.T) {
			data, err := os.ReadFile(f.path)
			if err != nil {
				t.Fatalf("read %s: %v", f.path, err)
			}
			syms, err := ParseFile(f.path, data)
			if err != nil {
				t.Fatalf("ParseFile %s: %v", f.path, err)
			}
			if len(syms) < f.minSyms {
				t.Errorf("%s: expected at least %d symbols, got %d", f.path, f.minSyms, len(syms))
				for _, s := range syms {
					t.Logf("  %s (%s) [%s]", s.Name, s.Kind, s.ExportStatus)
				}
			}
		})
	}
}

// --- Exported class test ---

func TestExportedClass(t *testing.T) {
	code := `export class MyService {
  run() {}
}`
	syms := parseTS(t, code)
	s := requireSymbol(t, syms, "MyService")
	if s.ExportStatus != ExportNamed {
		t.Errorf("expected exported, got %s", s.ExportStatus)
	}
	if s.Kind != SymbolClass {
		t.Errorf("expected class, got %s", s.Kind)
	}
}

// --- Edge cases ---

func TestEmptyFile(t *testing.T) {
	syms, err := ParseSource("")
	if err != nil {
		t.Fatalf("empty file should not error: %v", err)
	}
	if len(syms) != 0 {
		t.Errorf("expected 0 symbols from empty file, got %d", len(syms))
	}
}

func TestFileWithOnlyComments(t *testing.T) {
	code := `// This file has only comments
/* Block comment
   spanning multiple lines
*/
// Another comment`
	syms, err := ParseSource(code)
	if err != nil {
		t.Fatalf("comments-only file should not error: %v", err)
	}
	if len(syms) != 0 {
		t.Errorf("expected 0 symbols from comments-only file, got %d", len(syms))
	}
}

func TestFileWithOnlyImports(t *testing.T) {
	code := `import { foo } from 'bar';
import * as baz from 'qux';`
	syms, err := ParseSource(code)
	if err != nil {
		t.Fatalf("imports-only file should not error: %v", err)
	}
	if len(syms) != 0 {
		t.Errorf("expected 0 symbols from imports-only file, got %d", len(syms))
	}
}

func TestExtremelyLongSingleLineFunction(t *testing.T) {
	// Build a very long single-line function
	code := `function longOne() { return "` + stringRepeat("a", 10000) + `"; }`
	syms, err := ParseSource(code)
	if err != nil {
		t.Fatalf("long single-line function should not error: %v", err)
	}
	s := requireSymbol(t, syms, "longOne")
	if s.StartLine != 1 || s.EndLine != 1 {
		t.Errorf("expected single-line range, got %d-%d", s.StartLine, s.EndLine)
	}
}

func TestUnsupportedExtension(t *testing.T) {
	_, err := ParseFile("test.py", []byte("def foo(): pass"))
	if err == nil {
		t.Error("expected error for unsupported extension")
	}
}

func stringRepeat(s string, n int) string {
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}
