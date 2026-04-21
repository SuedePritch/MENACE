package indexer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveNamedImport(t *testing.T) {
	source, err := os.ReadFile("../../testdata/imports.ts")
	if err != nil {
		t.Fatal(err)
	}
	imports, err := ResolveImports("../../testdata/imports.ts", source)
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, imp := range imports {
		if imp.LocalName == "helperFn" && imp.SourceName == "helperFn" {
			found = true
			if filepath.Base(imp.FromPath) != "imported.ts" {
				t.Errorf("expected FromPath to end with imported.ts, got %s", imp.FromPath)
			}
		}
	}
	if !found {
		t.Errorf("did not find named import 'helperFn'; imports: %+v", imports)
	}
}

func TestResolveDefaultImport(t *testing.T) {
	source, err := os.ReadFile("../../testdata/imports.ts")
	if err != nil {
		t.Fatal(err)
	}
	imports, err := ResolveImports("../../testdata/imports.ts", source)
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, imp := range imports {
		if imp.LocalName == "defaultHelper" && imp.SourceName == "default" {
			found = true
		}
	}
	if !found {
		t.Errorf("did not find default import 'defaultHelper'; imports: %+v", imports)
	}
}

func TestResolveReExports(t *testing.T) {
	tmp := t.TempDir()

	// Create source file
	barContent := []byte(`export function foo() { return 1; }`)
	os.WriteFile(filepath.Join(tmp, "bar.ts"), barContent, 0o644)

	// Create re-export file
	indexContent := []byte(`export { foo } from './bar';`)
	indexPath := filepath.Join(tmp, "index.ts")
	os.WriteFile(indexPath, indexContent, 0o644)

	reexports, err := ResolveExportStatements(indexPath, indexContent)
	if err != nil {
		t.Fatal(err)
	}

	if len(reexports) != 1 {
		t.Fatalf("expected 1 re-export, got %d: %+v", len(reexports), reexports)
	}
	if reexports[0].SourceName != "foo" {
		t.Errorf("expected source name 'foo', got %s", reexports[0].SourceName)
	}
}

func TestMatchImportToSymbol(t *testing.T) {
	// Parse the imported file
	importedSrc, err := os.ReadFile("../../testdata/imported.ts")
	if err != nil {
		t.Fatal(err)
	}
	importedSyms, err := ParseFile("../../testdata/imported.ts", importedSrc)
	if err != nil {
		t.Fatal(err)
	}

	// Parse imports from the importing file
	importsSrc, err := os.ReadFile("../../testdata/imports.ts")
	if err != nil {
		t.Fatal(err)
	}
	imports, err := ResolveImports("../../testdata/imports.ts", importsSrc)
	if err != nil {
		t.Fatal(err)
	}

	// Build file symbol map
	absPath, _ := filepath.Abs("../../testdata/imported.ts")
	fileSymbols := map[string][]Symbol{
		absPath: importedSyms,
	}

	// Fix import paths to absolute for matching
	for i := range imports {
		if imports[i].FromPath != "" {
			imports[i].FromPath, _ = filepath.Abs(imports[i].FromPath)
		}
	}

	// Test named import resolution
	for _, imp := range imports {
		if imp.LocalName == "helperFn" {
			sym := MatchImportToSymbol(imp, fileSymbols)
			if sym == nil {
				t.Error("failed to resolve 'helperFn' import to symbol")
			} else if sym.Name != "helperFn" {
				t.Errorf("expected symbol name 'helperFn', got %s", sym.Name)
			}
		}
		// Test default import resolution
		if imp.LocalName == "defaultHelper" {
			sym := MatchImportToSymbol(imp, fileSymbols)
			if sym == nil {
				t.Error("failed to resolve default import to symbol")
			} else if sym.Name != "defaultHelper" {
				t.Errorf("expected symbol name 'defaultHelper', got %s", sym.Name)
			}
		}
	}
}
