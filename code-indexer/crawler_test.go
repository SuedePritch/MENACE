package indexer

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestCrawlFindsAllFixtures(t *testing.T) {
	cfg := DefaultCrawlerConfig()
	files, err := CrawlFiles("../../testdata", cfg)
	if err != nil {
		t.Fatalf("CrawlFiles: %v", err)
	}

	// Should find all .ts and .tsx files in testdata/
	want := map[string]bool{
		"simple.ts":    false,
		"arrows.ts":    false,
		"class.ts":     false,
		"types.ts":     false,
		"duplicates.ts": false,
		"imported.ts":  false,
		"imports.ts":   false,
		"component.tsx": false,
		"malformed.ts": false,
	}

	for _, f := range files {
		base := filepath.Base(f)
		if _, ok := want[base]; ok {
			want[base] = true
		}
	}

	for name, found := range want {
		if !found {
			t.Errorf("expected to find %s in crawl results", name)
		}
	}
}

func TestCrawlExcludesNodeModules(t *testing.T) {
	// Create a temp directory with node_modules
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, "src"), 0o755)
	os.MkdirAll(filepath.Join(tmp, "node_modules", "pkg"), 0o755)
	os.MkdirAll(filepath.Join(tmp, "dist"), 0o755)
	os.MkdirAll(filepath.Join(tmp, "build"), 0o755)
	os.MkdirAll(filepath.Join(tmp, ".next"), 0o755)
	os.MkdirAll(filepath.Join(tmp, "coverage"), 0o755)

	os.WriteFile(filepath.Join(tmp, "src", "app.ts"), []byte("export function app() {}"), 0o644)
	os.WriteFile(filepath.Join(tmp, "node_modules", "pkg", "index.ts"), []byte("export default 1"), 0o644)
	os.WriteFile(filepath.Join(tmp, "dist", "out.js"), []byte("var x = 1"), 0o644)
	os.WriteFile(filepath.Join(tmp, "build", "out.ts"), []byte("var x = 1"), 0o644)
	os.WriteFile(filepath.Join(tmp, ".next", "page.ts"), []byte("var x = 1"), 0o644)
	os.WriteFile(filepath.Join(tmp, "coverage", "report.ts"), []byte("var x = 1"), 0o644)

	cfg := DefaultCrawlerConfig()
	files, err := CrawlFiles(tmp, cfg)
	if err != nil {
		t.Fatalf("CrawlFiles: %v", err)
	}

	if len(files) != 1 {
		t.Errorf("expected 1 file (src/app.ts), got %d: %v", len(files), files)
	}
	if len(files) == 1 && filepath.Base(files[0]) != "app.ts" {
		t.Errorf("expected app.ts, got %s", files[0])
	}
}

func TestCrawlHandlesPermissionErrors(t *testing.T) {
	tmp := t.TempDir()
	noRead := filepath.Join(tmp, "noaccess")
	os.MkdirAll(noRead, 0o000)
	defer os.Chmod(noRead, 0o755) // cleanup

	os.WriteFile(filepath.Join(tmp, "ok.ts"), []byte("const x = 1"), 0o644)

	cfg := DefaultCrawlerConfig()
	files, err := CrawlFiles(tmp, cfg)
	if err != nil {
		t.Fatalf("CrawlFiles should not error: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("expected 1 file, got %d", len(files))
	}
}

func TestCrawlSymlinkCycleDetection(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "index.ts"), []byte("export const x = 1"), 0o644)

	// Create a symlink that points back to parent
	os.Symlink(tmp, filepath.Join(src, "loop"))

	cfg := DefaultCrawlerConfig()
	files, err := CrawlFiles(tmp, cfg)
	if err != nil {
		t.Fatalf("CrawlFiles: %v", err)
	}
	// Should find index.ts but not loop infinitely
	if len(files) < 1 {
		t.Errorf("expected at least 1 file, got %d", len(files))
	}
}

// --- Concurrency tests ---

func TestIndexFilesCorrectnessInvariant(t *testing.T) {
	cfg := DefaultCrawlerConfig()
	files, err := CrawlFiles("../../testdata", cfg)
	if err != nil {
		t.Fatalf("CrawlFiles: %v", err)
	}

	// Parse with 1 worker
	results1 := IndexFiles(files, 1)
	// Parse with 8 workers
	results8 := IndexFiles(files, 8)

	if len(results1) != len(results8) {
		t.Fatalf("different result counts: %d vs %d", len(results1), len(results8))
	}

	// Compare symbol names (order within each file should be deterministic)
	for i := range results1 {
		names1 := symbolNames(results1[i].Symbols)
		names8 := symbolNames(results8[i].Symbols)
		sort.Strings(names1)
		sort.Strings(names8)
		if len(names1) != len(names8) {
			t.Errorf("file %s: different symbol counts: %d vs %d", results1[i].FilePath, len(names1), len(names8))
			continue
		}
		for j := range names1 {
			if names1[j] != names8[j] {
				t.Errorf("file %s: symbol mismatch at %d: %s vs %s", results1[i].FilePath, j, names1[j], names8[j])
			}
		}
	}
}

func symbolNames(syms []Symbol) []string {
	names := make([]string, len(syms))
	for i, s := range syms {
		names[i] = s.Name
	}
	return names
}
