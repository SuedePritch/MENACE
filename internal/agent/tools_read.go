package agent

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"menace/internal/indexer"

	"github.com/flitsinc/go-llms/tools"
)

// toolTimeout is the maximum duration for shell-out tools (grep, find).
// Prevents pathological regex patterns or huge directory trees from hanging.
const toolTimeout = 30 * time.Second

// Pre-compiled regexes shared across tool handlers.
var (
	// sigRe matches function/type/class signature lines for outline extraction.
	sigRe = regexp.MustCompile(`^(func |type |interface |struct |export )?(function|class|interface|type|enum|func |type |struct )`)
	// funcStartRe matches the start of a function/method/class definition.
	funcStartRe = regexp.MustCompile(`^(func |.*function |.*class |def )`)
)

// Tool result limits — cap output to avoid LLM context bloat.
const (
	maxSearchResults   = 80
	maxFindFiles       = 50
	maxReferences      = 60
	maxBraceBlockLines = 200
	maxTypeBlockLines  = 150
	maxGrepPreview     = 20
	maxReadFileSize    = 10 * 1024 * 1024 // 10MB
)

// ReadTools returns the read-only tool set (for architect).
func ReadTools(cwd string) []tools.Tool {
	return []tools.Tool{
		listDirTool(cwd),
		readFileTool(cwd),
		searchCodeTool(cwd),
		findFilesTool(cwd),
		fileOutlineTool(cwd),
		getFunctionTool(cwd),
		getImportsTool(cwd),
		getTypeTool(cwd),
		findReferencesTool(cwd),
		fileStatsTool(cwd),
		projectOutlineTool(cwd),
	}
}

// resolvePath resolves a relative or absolute path and ensures the result is
// contained within cwd. This prevents path-traversal attacks where an LLM
// could request files outside the working directory (e.g. "../../../../etc/passwd").
// If the resolved path escapes cwd, we return cwd itself so the caller gets a
// harmless "is a directory" error instead of leaking sensitive data.
func resolvePath(cwd, path string) string {
	var resolved string
	if filepath.IsAbs(path) {
		resolved = filepath.Clean(path)
	} else {
		resolved = filepath.Join(cwd, path)
	}
	// Resolve symlinks to prevent escaping cwd via symlinked directories.
	// EvalSymlinks also cleans the path, normalising away ".." components.
	if evaled, err := filepath.EvalSymlinks(resolved); err == nil {
		resolved = evaled
	}
	// Re-evaluate cwd through symlinks too so the prefix check is consistent.
	evaledCwd := cwd
	if ec, err := filepath.EvalSymlinks(cwd); err == nil {
		evaledCwd = ec
	}
	if !strings.HasPrefix(resolved, evaledCwd+string(filepath.Separator)) && resolved != evaledCwd {
		return cwd
	}
	return resolved
}

// ── list_dir ───────────────────────────────────────────────────────────────

type listDirParams struct {
	Path string `json:"path" description:"Directory path"`
}

func listDirTool(cwd string) tools.Tool {
	return tools.Func("List Directory", "List directory contents. Returns names with / suffix for directories.", "list_dir",
		func(r tools.Runner, p listDirParams) tools.Result {
			target := resolvePath(cwd, p.Path)
			entries, err := os.ReadDir(target)
			if err != nil {
				return tools.Error(err)
			}
			var lines []string
			for _, e := range entries {
				name := e.Name()
				if e.IsDir() {
					name += "/"
				}
				lines = append(lines, name)
			}
			if len(lines) == 0 {
				return tools.SuccessFromString("(empty)")
			}
			return tools.SuccessFromString(strings.Join(lines, "\n"))
		})
}

// ── read_file ──────────────────────────────────────────────────────────────

type readFileParams struct {
	Path      string `json:"path" description:"File path"`
	StartLine *int   `json:"start_line,omitempty" description:"First line (1-based)"`
	EndLine   *int   `json:"end_line,omitempty" description:"Last line (1-based)"`
}

func readFileTool(cwd string) tools.Tool {
	return tools.Func("Read File", "Read file contents with line numbers. Supports optional line range.", "read_file",
		func(r tools.Runner, p readFileParams) tools.Result {
			target := resolvePath(cwd, p.Path)

			// When a line range is provided, stream the file to avoid loading it all.
			if p.StartLine != nil || p.EndLine != nil {
				return readFileRange(target, p.StartLine, p.EndLine)
			}

			info, err := os.Stat(target)
			if err != nil {
				return tools.Error(err)
			}
			if info.Size() > maxReadFileSize {
				return tools.SuccessFromString(fmt.Sprintf("File too large (%d bytes, max %d). Use start_line/end_line to read a range.", info.Size(), maxReadFileSize))
			}

			data, err := os.ReadFile(target)
			if err != nil {
				return tools.Error(err)
			}
			lines := strings.Split(string(data), "\n")
			var numbered []string
			for i, line := range lines {
				numbered = append(numbered, fmt.Sprintf("%d\t%s", i+1, line))
			}
			return tools.SuccessFromString(strings.Join(numbered, "\n"))
		})
}

// readFileRange reads only the requested line range using a streaming scanner.
func readFileRange(target string, startLine, endLine *int) tools.Result {
	f, err := os.Open(target)
	if err != nil {
		return tools.Error(err)
	}
	defer f.Close()

	start := 1
	if startLine != nil {
		start = *startLine
	}
	if start < 1 {
		start = 1
	}
	end := -1 // no limit
	if endLine != nil {
		end = *endLine
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 1024*1024)
	var numbered []string
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if end > 0 && lineNum > end {
			break
		}
		if lineNum >= start {
			numbered = append(numbered, fmt.Sprintf("%d\t%s", lineNum, scanner.Text()))
		}
	}
	if err := scanner.Err(); err != nil {
		return tools.Error(err)
	}
	if len(numbered) == 0 {
		return tools.SuccessFromString("No lines in the requested range.")
	}
	return tools.SuccessFromString(strings.Join(numbered, "\n"))
}

// ── search_code ────────────────────────────────────────────────────────────

type searchCodeParams struct {
	Pattern  string `json:"pattern" description:"Regex pattern"`
	Path     string `json:"path,omitempty" description:"Search root (default: .)"`
	FileGlob string `json:"file_glob,omitempty" description:"File filter glob, e.g. '*.go'"`
}

func searchCodeTool(cwd string) tools.Tool {
	return tools.Func("Search Code", "Search for a pattern across files using grep. Returns file:line:match. Max 80 results.", "search_code",
		func(r tools.Runner, p searchCodeParams) tools.Result {
			searchPath := cwd
			if p.Path != "" { searchPath = resolvePath(cwd, p.Path) }
			args := []string{"-rn"}
			if p.FileGlob != "" { args = append(args, "--include="+p.FileGlob) }
			args = append(args, "--", p.Pattern, searchPath)
			ctx, cancel := context.WithTimeout(r.Context(), toolTimeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, "grep", args...)
			out, err := cmd.Output()
			result := strings.TrimSpace(string(out))
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
					return tools.SuccessFromString("No matches.")
				}
				if result == "" { return tools.SuccessFromString("No matches.") }
			}
			lines := strings.Split(result, "\n")
			if len(lines) > maxSearchResults { lines = lines[:maxSearchResults] }
			return tools.SuccessFromString(strings.Join(lines, "\n"))
		})
}

// ── find_files ─────────────────────────────────────────────────────────────

type findFilesParams struct {
	Pattern string `json:"pattern" description:"Glob pattern, e.g. '*.go'"`
	Path    string `json:"path,omitempty" description:"Search root (default: .)"`
}

func findFilesTool(cwd string) tools.Tool {
	return tools.Func("Find Files", "Find files by name/glob pattern. Max 50 results.", "find_files",
		func(r tools.Runner, p findFilesParams) tools.Result {
			searchPath := cwd
			if p.Path != "" { searchPath = resolvePath(cwd, p.Path) }
			ctx, cancel := context.WithTimeout(r.Context(), toolTimeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, "find", searchPath,
				"-name", p.Pattern,
				"-not", "-path", "*/node_modules/*",
				"-not", "-path", "*/.git/*",
				"-not", "-path", "*/dist/*",
			)
			out, _ := cmd.Output()
			result := strings.TrimSpace(string(out))
			if result == "" { return tools.SuccessFromString("No files found.") }
			lines := strings.Split(result, "\n")
			if len(lines) > maxFindFiles { lines = lines[:maxFindFiles] }
			return tools.SuccessFromString(strings.Join(lines, "\n"))
		})
}

// ── file_outline ───────────────────────────────────────────────────────────

type fileOutlineParams struct {
	Path string `json:"path" description:"File path"`
}

func fileOutlineTool(cwd string) tools.Tool {
	return tools.Func("File Outline", "Get a compact outline of a file: symbol names, kinds, line ranges, exports. No source code.", "file_outline",
		func(r tools.Runner, p fileOutlineParams) tools.Result {
			target := resolvePath(cwd, p.Path)

			// Try AST indexer first
			if idx := indexer.ForFile(target); idx != nil {
				syms, err := idx.SymbolsInFile(target)
				if err == nil && len(syms) > 0 {
					var lines []string
					for _, s := range syms {
						exp := ""
						if s.ExportStatus == "exported" { exp = " [exp]" }
						if s.ExportStatus == "default" { exp = " [default]" }
						deps := ""
						if len(s.Dependencies) > 0 { deps = " → " + strings.Join(s.Dependencies, ", ") }
						lines = append(lines, fmt.Sprintf("%d-%d\t%s\t%s%s%s", s.StartLine, s.EndLine, s.Kind, s.Name, exp, deps))
					}
					return tools.SuccessFromString(strings.Join(lines, "\n"))
				}
			}

			// Regex fallback
			data, err := os.ReadFile(target)
			if err != nil { return tools.Error(err) }
			lines := strings.Split(string(data), "\n")
			var sigs []string
			for i, line := range lines {
				if sigRe.MatchString(strings.TrimSpace(line)) {
					sigs = append(sigs, fmt.Sprintf("%d\t%s", i+1, strings.TrimSpace(line)))
				}
			}
			if len(sigs) == 0 {
				return tools.SuccessFromString(fmt.Sprintf("(%d lines, no extractable signatures)", len(lines)))
			}
			return tools.SuccessFromString(strings.Join(sigs, "\n"))
		})
}

// ── get_function ───────────────────────────────────────────────────────────

type getFunctionParams struct {
	Name string `json:"name" description:"Function/method/class name"`
	Path string `json:"path,omitempty" description:"File path (searches project with grep if omitted)"`
}

func getFunctionTool(cwd string) tools.Tool {
	return tools.Func("Get Function", "Get the full source of a function/method/class by name. Uses AST for TS/JS, brace-matching for others.", "get_function",
		func(r tools.Runner, p getFunctionParams) tools.Result {
			// Try AST indexer
			filePath := ""
			if p.Path != "" { filePath = resolvePath(cwd, p.Path) }
			if idx := indexer.ForFile(filePath); idx != nil && filePath != "" {
				syms, err := idx.FindSymbol(p.Name, filePath)
				if err == nil && len(syms) > 0 {
					s := syms[0]
					return tools.SuccessFromString(fmt.Sprintf("%s:%d-%d (%s)\n\n%s", s.FilePath, s.StartLine, s.EndLine, s.Kind, s.Source))
				}
			}

			if p.Path == "" {
				gctx, gcancel := context.WithTimeout(r.Context(), toolTimeout)
				defer gcancel()
				cmd := exec.CommandContext(gctx, "grep", "-rn", "--include=*.go", "--include=*.ts", "--include=*.js",
					"-E", fmt.Sprintf(`\b%s\b`, p.Name), cwd)
				out, _ := cmd.Output()
				if len(out) == 0 { return tools.SuccessFromString(fmt.Sprintf("Symbol %q not found.", p.Name)) }
				lines := strings.Split(strings.TrimSpace(string(out)), "\n")
				if len(lines) > maxGrepPreview { lines = lines[:maxGrepPreview] }
				return tools.SuccessFromString("References:\n" + strings.Join(lines, "\n"))
			}
			target := resolvePath(cwd, p.Path)
			data, err := os.ReadFile(target)
			if err != nil { return tools.Error(err) }
			lines := strings.Split(string(data), "\n")
			pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(p.Name) + `\b`)
			for i, line := range lines {
				if pattern.MatchString(line) && funcStartRe.MatchString(line) {
					block := extractBraceBlock(lines, i, maxBraceBlockLines)
					return tools.SuccessFromString(strings.Join(block, "\n"))
				}
			}
			return tools.SuccessFromString(fmt.Sprintf("Function %q not found in %s", p.Name, target))
		})
}

// ── get_imports ─────────────────────────────────────────────────────────────

type getImportsParams struct {
	Path string `json:"path" description:"File path"`
}

func getImportsTool(cwd string) tools.Tool {
	return tools.Func("Get Imports", "Get just the import block from a file.", "get_imports",
		func(r tools.Runner, p getImportsParams) tools.Result {
			target := resolvePath(cwd, p.Path)
			data, err := os.ReadFile(target)
			if err != nil { return tools.Error(err) }
			lines := strings.Split(string(data), "\n")
			var imports []string
			inBlock := false
			for i, line := range lines {
				trimmed := strings.TrimSpace(line)
				if inBlock {
					imports = append(imports, fmt.Sprintf("%d\t%s", i+1, line))
					if trimmed == ")" || strings.HasSuffix(trimmed, ";") || strings.Contains(trimmed, "from ") { inBlock = false }
					continue
				}
				if strings.HasPrefix(trimmed, "import ") || strings.HasPrefix(trimmed, "import(") {
					imports = append(imports, fmt.Sprintf("%d\t%s", i+1, line))
					if trimmed == "import (" || (!strings.Contains(trimmed, "from ") && !strings.HasSuffix(trimmed, ";") && !strings.HasSuffix(trimmed, `"`)) {
						inBlock = true
					}
				}
			}
			if len(imports) == 0 { return tools.SuccessFromString("No imports found.") }
			return tools.SuccessFromString(strings.Join(imports, "\n"))
		})
}

// ── get_type ───────────────────────────────────────────────────────────────

type getTypeParams struct {
	Name string `json:"name" description:"Type name"`
	Path string `json:"path,omitempty" description:"File path (searches with grep if omitted)"`
}

func getTypeTool(cwd string) tools.Tool {
	return tools.Func("Get Type", "Get a struct/interface/type definition by name, plus its methods. Uses AST for TS/JS.", "get_type",
		func(r tools.Runner, p getTypeParams) tools.Result {
			// Try AST indexer
			filePath := ""
			if p.Path != "" { filePath = resolvePath(cwd, p.Path) }
			if idx := indexer.ForFile(filePath); idx != nil && filePath != "" {
				syms, err := idx.FindSymbol(p.Name, filePath)
				if err == nil && len(syms) > 0 {
					var parts []string
					for _, s := range syms {
						parts = append(parts, fmt.Sprintf("%s:%d-%d (%s)\n\n%s", s.FilePath, s.StartLine, s.EndLine, s.Kind, s.Source))
					}
					return tools.SuccessFromString(strings.Join(parts, "\n\n---\n\n"))
				}
			}

			if p.Path == "" {
				gctx, gcancel := context.WithTimeout(r.Context(), toolTimeout)
				defer gcancel()
				cmd := exec.CommandContext(gctx, "grep", "-rn", "--include=*.go",
					fmt.Sprintf("^type %s ", p.Name), cwd)
				out, _ := cmd.Output()
				if len(out) == 0 { return tools.SuccessFromString(fmt.Sprintf("Type %q not found.", p.Name)) }
				return tools.SuccessFromString(strings.TrimSpace(string(out)))
			}
			target := resolvePath(cwd, p.Path)
			data, err := os.ReadFile(target)
			if err != nil { return tools.Error(err) }
			lines := strings.Split(string(data), "\n")
			typeRe := regexp.MustCompile(fmt.Sprintf(`^type\s+%s\b`, regexp.QuoteMeta(p.Name)))
			for i, line := range lines {
				if typeRe.MatchString(line) {
					block := extractBraceBlock(lines, i, maxTypeBlockLines)
					return tools.SuccessFromString(strings.Join(block, "\n"))
				}
			}
			return tools.SuccessFromString(fmt.Sprintf("Type %q not found.", p.Name))
		})
}

// ── find_references ────────────────────────────────────────────────────────

type findReferencesParams struct {
	Name     string `json:"name" description:"Symbol name"`
	Path     string `json:"path,omitempty" description:"Search root (default: .)"`
	FileGlob string `json:"file_glob,omitempty" description:"File filter, e.g. '*.go'"`
}

func findReferencesTool(cwd string) tools.Tool {
	return tools.Func("Find References", "Find all references to a symbol. Returns compact file:line pairs.", "find_references",
		func(r tools.Runner, p findReferencesParams) tools.Result {
			searchPath := cwd
			if p.Path != "" { searchPath = resolvePath(cwd, p.Path) }
			args := []string{"-rn", "-w"}
			if p.FileGlob != "" { args = append(args, "--include="+p.FileGlob) }
			args = append(args, "--", p.Name, searchPath)
			ctx, cancel := context.WithTimeout(r.Context(), toolTimeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, "grep", args...)
			out, err := cmd.Output()
			if err != nil { return tools.SuccessFromString("No references found.") }
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) > maxReferences { lines = lines[:maxReferences] }
			var refs []string
			for _, line := range lines {
				parts := strings.SplitN(line, ":", 3)
				if len(parts) >= 2 { refs = append(refs, parts[0]+":"+parts[1]) }
			}
			if len(refs) == 0 { return tools.SuccessFromString("No references found.") }
			return tools.SuccessFromString(strings.Join(refs, "\n"))
		})
}

// ── file_stats ─────────────────────────────────────────────────────────────

type fileStatsParams struct {
	Path string `json:"path" description:"File path"`
}

func fileStatsTool(cwd string) tools.Tool {
	return tools.Func("File Stats", "Get file metadata: line count, size, last modified. No content returned.", "file_stats",
		func(r tools.Runner, p fileStatsParams) tools.Result {
			target := resolvePath(cwd, p.Path)
			info, err := os.Stat(target)
			if err != nil { return tools.Error(err) }
			lineCount, err := countLines(target)
			if err != nil { return tools.Error(err) }
			return tools.SuccessFromString(fmt.Sprintf("Lines: %d | Size: %dB | Modified: %s", lineCount, info.Size(), info.ModTime().Format("2006-01-02T15:04:05Z")))
		})
}

// countLines counts newlines in a file using a streaming buffer to avoid
// reading the entire file into memory.
func countLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	buf := make([]byte, 32*1024)
	count := 0
	for {
		n, err := f.Read(buf)
		for i := 0; i < n; i++ {
			if buf[i] == '\n' {
				count++
			}
		}
		if err != nil {
			break
		}
	}
	return count + 1, nil // +1 for last line without trailing newline
}

// ── project_outline ────────────────────────────────────────────────────────

type projectOutlineParams struct {
	Path string `json:"path" description:"Project root directory"`
}

func projectOutlineTool(cwd string) tools.Tool {
	return tools.Func("Project Outline", "Full project symbol map. AST-powered for TS/JS, regex for others. No source — just names, kinds, lines, exports.", "project_outline",
		func(r tools.Runner, p projectOutlineParams) tools.Result {
			root := resolvePath(cwd, p.Path)

			// Try AST indexers first for a full index
			var astResult strings.Builder
			for _, idx := range indexer.All() {
				report, err := idx.IndexDir(root, 4)
				if err != nil || len(report.Symbols) == 0 { continue }

				// Group by file
				byFile := map[string][]indexer.Symbol{}
				for _, s := range report.Symbols {
					byFile[s.FilePath] = append(byFile[s.FilePath], s)
				}
				for file, syms := range byFile {
					rel, _ := filepath.Rel(root, file)
					if rel == "" { rel = file }
					astResult.WriteString(fmt.Sprintf("\n── %s ──\n", rel))
					for _, s := range syms {
						exp := ""
						if s.ExportStatus == "exported" { exp = " [exp]" }
						if s.ExportStatus == "default" { exp = " [default]" }
						astResult.WriteString(fmt.Sprintf("  %d-%d\t%s\t%s%s\n", s.StartLine, s.EndLine, s.Kind, s.Name, exp))
					}
				}
			}

			// Regex fallback for non-indexed files
			const maxOutlineDepth = 4
			const maxOutlineSymbols = 2000
			symbolCount := 0
			var regexResult strings.Builder
			_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
				if err != nil { return nil }
				if symbolCount >= maxOutlineSymbols { return filepath.SkipAll }
				name := info.Name()
				if info.IsDir() {
					if strings.HasPrefix(name, ".") || name == "node_modules" || name == "dist" || name == "vendor" { return filepath.SkipDir }
					rel, _ := filepath.Rel(root, path)
					if rel != "" && strings.Count(rel, string(filepath.Separator)) >= maxOutlineDepth {
						return filepath.SkipDir
					}
					return nil
				}
				// Skip files already handled by AST indexer
				if indexer.ForFile(path) != nil { return nil }
				ext := filepath.Ext(name)
				if ext != ".go" && ext != ".py" && ext != ".rs" && ext != ".c" && ext != ".cpp" && ext != ".java" { return nil }
				data, err := os.ReadFile(path)
				if err != nil { return nil }
				lines := strings.Split(string(data), "\n")
				var sigs []string
				for i, line := range lines {
					if sigRe.MatchString(strings.TrimSpace(line)) {
						sigs = append(sigs, fmt.Sprintf("  %d\t%s", i+1, strings.TrimSpace(line)))
						symbolCount++
						if symbolCount >= maxOutlineSymbols { break }
					}
				}
				if len(sigs) > 0 {
					rel, _ := filepath.Rel(root, path)
					if rel == "" { rel = path }
					regexResult.WriteString(fmt.Sprintf("\n── %s ──\n", rel))
					regexResult.WriteString(strings.Join(sigs, "\n"))
					regexResult.WriteString("\n")
				}
				return nil
			})

			out := astResult.String() + regexResult.String()
			if out == "" { return tools.SuccessFromString("No symbols found.") }
			return tools.SuccessFromString(out)
		})
}

// ── Helpers ────────────────────────────────────────────────────────────────

// findFunctionEnd finds the closing brace of a function starting at the given line.
// Returns the line index after the closing brace and whether it was found.
func findFunctionEnd(lines []string, start int) (end int, found bool) {
	depth := 0
	started := false
	for j := start; j < len(lines); j++ {
		countBraces(lines[j], &depth, &started)
		if started && depth <= 0 {
			return j + 1, true
		}
	}
	return 0, false
}

// extractBraceBlock extracts a brace-delimited block starting from the given line.
// It skips braces inside strings and single-line comments to avoid false matches.
func extractBraceBlock(lines []string, start, maxLines int) []string {
	var block []string
	depth := 0
	started := false
	for j := start; j < len(lines); j++ {
		block = append(block, fmt.Sprintf("%d\t%s", j+1, lines[j]))
		countBraces(lines[j], &depth, &started)
		if started && depth <= 0 {
			break
		}
		if len(block) > maxLines {
			block = append(block, "... (truncated)")
			break
		}
	}
	return block
}

// countBraces counts net brace depth on a line, skipping braces inside
// strings (single/double/backtick) and line comments (// and #).
func countBraces(line string, depth *int, started *bool) {
	inString := rune(0)
	escaped := false
	for i, ch := range line {
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && inString != 0 {
			escaped = true
			continue
		}
		if inString != 0 {
			if ch == inString {
				inString = 0
			}
			continue
		}
		// Line comment — stop processing this line
		if ch == '/' && i+1 < len(line) && line[i+1] == '/' {
			return
		}
		if ch == '#' {
			return
		}
		if ch == '"' || ch == '\'' || ch == '`' {
			inString = ch
			continue
		}
		if ch == '{' {
			*depth++
			*started = true
		}
		if ch == '}' {
			*depth--
		}
	}
}
