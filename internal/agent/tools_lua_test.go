package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	gollmstools "github.com/flitsinc/go-llms/tools"
)

// writeLua writes a .lua file into dir and returns its path.
func writeLua(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadLuaTools_ScopeFiltering(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()

	writeLua(t, dir, "arch.lua", `
name        = "arch_tool"
description = "architect only"
scope       = "architect"
params      = {}
function run(cwd, p) return "arch" end
`)
	writeLua(t, dir, "worker.lua", `
name        = "worker_tool"
description = "worker only"
scope       = "worker"
params      = {}
function run(cwd, p) return "worker" end
`)
	writeLua(t, dir, "both.lua", `
name        = "both_tool"
description = "both"
scope       = "both"
params      = {}
function run(cwd, p) return "both" end
`)

	archTools := LoadLuaTools(dir, cwd, ScopeArchitect)
	workerTools := LoadLuaTools(dir, cwd, ScopeWorker)

	archNames := toolNames(archTools)
	workerNames := toolNames(workerTools)

	// architect gets: architect + both
	if !contains(archNames, "arch_tool") {
		t.Errorf("architect missing arch_tool, got %v", archNames)
	}
	if !contains(archNames, "both_tool") {
		t.Errorf("architect missing both_tool, got %v", archNames)
	}
	if contains(archNames, "worker_tool") {
		t.Errorf("architect should not have worker_tool, got %v", archNames)
	}

	// worker gets: worker + both
	if !contains(workerNames, "worker_tool") {
		t.Errorf("worker missing worker_tool, got %v", workerNames)
	}
	if !contains(workerNames, "both_tool") {
		t.Errorf("worker missing both_tool, got %v", workerNames)
	}
	if contains(workerNames, "arch_tool") {
		t.Errorf("worker should not have arch_tool, got %v", workerNames)
	}
}

func TestLoadLuaTools_InvalidScopeSkipped(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()

	writeLua(t, dir, "bad_scope.lua", `
name        = "bad"
description = "bad scope"
scope       = "readwrite"
params      = {}
function run(cwd, p) return "" end
`)
	writeLua(t, dir, "good.lua", `
name        = "good"
description = "good"
scope       = "both"
params      = {}
function run(cwd, p) return "ok" end
`)

	tt := LoadLuaTools(dir, cwd, ScopeArchitect)
	names := toolNames(tt)
	if contains(names, "bad") {
		t.Errorf("bad_scope.lua should have been skipped")
	}
	if !contains(names, "good") {
		t.Errorf("good.lua should be loaded")
	}
}

func TestLoadLuaTools_MissingScopeSkipped(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()

	writeLua(t, dir, "noscope.lua", `
name        = "noscope"
description = "no scope field"
params      = {}
function run(cwd, p) return "" end
`)

	tools := LoadLuaTools(dir, cwd, ScopeArchitect)
	if len(tools) != 0 {
		t.Errorf("expected no tools, got %d", len(tools))
	}
}

func TestLoadLuaTools_RunExec(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()

	writeLua(t, dir, "echo.lua", `
name        = "echo_tool"
description = "echoes input"
scope       = "both"
params      = {
  { name = "msg", type = "string", description = "message to echo" },
}
function run(cwd, p)
  return exec(cwd, "echo", p.msg)
end
`)

	tt := LoadLuaTools(dir, cwd, ScopeBoth)
	if len(tt) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tt))
	}

	result := runTool(t, tt[0], `{"msg":"hello"}`)
	if !strings.Contains(result, "hello") {
		t.Errorf("expected 'hello' in output, got %q", result)
	}
}

func TestLoadLuaTools_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	tools := LoadLuaTools(dir, cwd, ScopeArchitect)
	if len(tools) != 0 {
		t.Errorf("expected 0 tools from empty dir, got %d", len(tools))
	}
}

func TestLoadLuaTools_NonExistentDir(t *testing.T) {
	tools := LoadLuaTools("/does/not/exist", t.TempDir(), ScopeArchitect)
	if len(tools) != 0 {
		t.Errorf("expected 0 tools from missing dir, got %d", len(tools))
	}
}

func TestBuildTools_BuiltinScopes(t *testing.T) {
	menaceDir := t.TempDir()
	os.MkdirAll(filepath.Join(menaceDir, "tools"), 0755)
	cwd := t.TempDir()

	archTools := buildTools(menaceDir, cwd, ScopeArchitect)
	workerTools := buildTools(menaceDir, cwd, ScopeWorker)

	archNames := toolNames(archTools)
	workerNames := toolNames(workerTools)

	// Navigation tools (ScopeBoth) should appear in both.
	for _, name := range []string{"tree", "find_symbol", "read_file", "search_code",
		"get_function", "callers", "callees", "symbol_context", "grep_files"} {
		if !contains(archNames, name) {
			t.Errorf("architect missing built-in %q", name)
		}
		if !contains(workerNames, name) {
			t.Errorf("worker missing built-in %q", name)
		}
	}

	// Write tools (ScopeWorker) should only appear for workers.
	for _, name := range []string{"write_file", "edit_file", "replace_function",
		"insert_after", "add_import", "diff_preview"} {
		if contains(archNames, name) {
			t.Errorf("architect should not have write tool %q", name)
		}
		if !contains(workerNames, name) {
			t.Errorf("worker missing write tool %q", name)
		}
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func toolNames(tt []gollmstools.Tool) []string {
	names := make([]string, len(tt))
	for i, t := range tt {
		names[i] = t.FuncName()
	}
	return names
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
