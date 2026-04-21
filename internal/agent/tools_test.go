package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── resolvePath ──────────────────────────────────────────────────────────────

func TestResolvePath_RelativeUnderCwd(t *testing.T) {
	got := resolvePath("/project", "src/main.go")
	if got != "/project/src/main.go" {
		t.Errorf("got %q, want /project/src/main.go", got)
	}
}

func TestResolvePath_TraversalBlocked(t *testing.T) {
	got := resolvePath("/project", "../../../etc/passwd")
	if got != "/project" {
		t.Errorf("got %q, want /project", got)
	}
}

func TestResolvePath_AbsoluteOutsideBlocked(t *testing.T) {
	got := resolvePath("/project", "/etc/passwd")
	if got != "/project" {
		t.Errorf("got %q, want /project", got)
	}
}

func TestResolvePath_AbsoluteInsideCwd(t *testing.T) {
	got := resolvePath("/project", "/project/src/main.go")
	if got != "/project/src/main.go" {
		t.Errorf("got %q, want /project/src/main.go", got)
	}
}

func TestResolvePath_CleanPaths(t *testing.T) {
	got := resolvePath("/project", "src/../src/main.go")
	if got != "/project/src/main.go" {
		t.Errorf("got %q, want /project/src/main.go", got)
	}
}

func TestResolvePath_SymlinkEscape(t *testing.T) {
	// Create a temp directory structure:
	//   tmpdir/project/
	//   tmpdir/secret/passwd.txt
	//   tmpdir/project/link -> tmpdir/secret
	tmpdir := t.TempDir()
	project := filepath.Join(tmpdir, "project")
	secret := filepath.Join(tmpdir, "secret")
	os.MkdirAll(project, 0755)
	os.MkdirAll(secret, 0755)
	os.WriteFile(filepath.Join(secret, "passwd.txt"), []byte("secret"), 0644)

	link := filepath.Join(project, "link")
	if err := os.Symlink(secret, link); err != nil {
		t.Skip("symlinks not supported:", err)
	}

	got := resolvePath(project, "link/passwd.txt")
	// Should be blocked — the resolved path is outside project
	if got != project {
		t.Errorf("symlink escape not blocked: got %q, want %q", got, project)
	}
}

func TestResolvePath_CwdExact(t *testing.T) {
	// resolvePath(cwd, ".") should return cwd itself
	got := resolvePath("/project", ".")
	if got != "/project" {
		t.Errorf("got %q, want /project", got)
	}
}

func TestResolvePath_EmptyPath(t *testing.T) {
	got := resolvePath("/project", "")
	if got != "/project" {
		t.Errorf("got %q, want /project", got)
	}
}

// ── countBraces ──────────────────────────────────────────────────────────────

func TestCountBraces_Basic(t *testing.T) {
	depth := 0
	started := false
	countBraces("func foo() {", &depth, &started)
	if depth != 1 || !started {
		t.Errorf("depth=%d started=%v, want 1/true", depth, started)
	}
}

func TestCountBraces_Closing(t *testing.T) {
	depth := 1
	started := true
	countBraces("}", &depth, &started)
	if depth != 0 {
		t.Errorf("depth=%d, want 0", depth)
	}
}

func TestCountBraces_StringLiteral(t *testing.T) {
	depth := 0
	started := false
	countBraces(`x := "{ not a brace }"`, &depth, &started)
	if depth != 0 || started {
		t.Errorf("depth=%d started=%v, want 0/false", depth, started)
	}
}

func TestCountBraces_SingleQuoted(t *testing.T) {
	depth := 0
	started := false
	countBraces("x := '{'", &depth, &started)
	if depth != 0 || started {
		t.Errorf("depth=%d started=%v, want 0/false", depth, started)
	}
}

func TestCountBraces_Backtick(t *testing.T) {
	depth := 0
	started := false
	countBraces("x := `{`", &depth, &started)
	if depth != 0 || started {
		t.Errorf("depth=%d started=%v, want 0/false", depth, started)
	}
}

func TestCountBraces_LineComment(t *testing.T) {
	depth := 0
	started := false
	countBraces("// { not counted", &depth, &started)
	if depth != 0 || started {
		t.Errorf("depth=%d started=%v, want 0/false", depth, started)
	}
}

func TestCountBraces_HashComment(t *testing.T) {
	depth := 0
	started := false
	countBraces("# { not counted", &depth, &started)
	if depth != 0 || started {
		t.Errorf("depth=%d started=%v, want 0/false", depth, started)
	}
}

func TestCountBraces_Mixed(t *testing.T) {
	depth := 0
	started := false
	countBraces(`real { "fake {" // comment {`, &depth, &started)
	if depth != 1 || !started {
		t.Errorf("depth=%d started=%v, want 1/true", depth, started)
	}
}

func TestCountBraces_EscapedQuote(t *testing.T) {
	depth := 0
	started := false
	countBraces(`x := "\"{\"" `, &depth, &started)
	if depth != 0 || started {
		t.Errorf("depth=%d started=%v, want 0/false", depth, started)
	}
}

// ── extractBraceBlock ────────────────────────────────────────────────────────

func TestExtractBraceBlock_MultiLine(t *testing.T) {
	lines := []string{
		`func foo() {`,
		`  x := "}"`,
		`  return x`,
		`}`,
	}
	block := extractBraceBlock(lines, 0, 100)
	if len(block) != 4 {
		t.Fatalf("got %d lines, want 4:\n%s", len(block), strings.Join(block, "\n"))
	}
	// Last line should contain the closing brace
	if !strings.Contains(block[3], "}") {
		t.Errorf("expected closing brace in last line, got %q", block[3])
	}
}
