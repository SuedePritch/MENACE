package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/flitsinc/go-llms/tools"
)

const toolTimeout = 30 * time.Second

const (
	maxSearchResults   = 80
	maxFindSymbols     = 60
	maxReadFileSize    = 10 * 1024 * 1024
	maxBraceBlockLines = 200
)

// Pre-compiled regex for brace-based get_function fallback.
var funcStartRe = regexp.MustCompile(`^(func |.*function |.*class |def )`)

func init() {
	RegisterTool(ScopeBoth, treeTool)
	RegisterTool(ScopeBoth, findSymbolTool)
	RegisterTool(ScopeBoth, readFileTool)
	RegisterTool(ScopeBoth, searchCodeTool)
	RegisterTool(ScopeBoth, getFunctionTool)
	RegisterTool(ScopeBoth, callersTool)
	RegisterTool(ScopeBoth, calleesTool)
	RegisterTool(ScopeBoth, symbolContextTool)
	RegisterTool(ScopeBoth, grepFilesTool)
}

// ReadTools returns tools available to the architect (scope: architect or both).
// Lua tools in menaceDir/tools/ matching those scopes are loaded automatically.
func ReadTools(menaceDir, cwd string) []tools.Tool {
	return buildTools(menaceDir, cwd, ScopeArchitect)
}


// resolvePath resolves a relative or absolute path within cwd.
// Prevents path-traversal outside the working directory.
func resolvePath(cwd, path string) string {
	var resolved string
	if filepath.IsAbs(path) {
		resolved = filepath.Clean(path)
	} else {
		resolved = filepath.Join(cwd, path)
	}
	if evaled, err := filepath.EvalSymlinks(resolved); err == nil {
		resolved = evaled
	}
	evaledCwd := cwd
	if ec, err := filepath.EvalSymlinks(cwd); err == nil {
		evaledCwd = ec
	}
	if !strings.HasPrefix(resolved, evaledCwd+string(filepath.Separator)) && resolved != evaledCwd {
		return cwd
	}
	return resolved
}

// ── tree ───────────────────────────────────────────────────────────────────

type treeParams struct {
	Path  string `json:"path,omitempty" description:"Directory path (default: .)"`
	Depth int    `json:"depth,omitempty" description:"Max depth (default: 3)"`
}

func treeTool(cwd string) tools.Tool {
	return tools.Func("Tree", "Show directory tree. Returns compact indented layout. Use depth to avoid noise.", "tree",
		func(r tools.Runner, p treeParams) tools.Result {
			root := cwd
			if p.Path != "" {
				root = resolvePath(cwd, p.Path)
			}
			depth := 3
			if p.Depth > 0 {
				depth = p.Depth
			}
			var sb strings.Builder
			_ = walkTree(root, root, 0, depth, &sb)
			out := strings.TrimSpace(sb.String())
			if out == "" {
				return tools.SuccessFromString("(empty)")
			}
			return tools.SuccessFromString(out)
		})
}

func walkTree(root, path string, level, maxDepth int, sb *strings.Builder) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		// Skip common noise directories
		if e.IsDir() && (name == "node_modules" || name == "vendor" || name == ".git" ||
			name == "dist" || name == "build" || name == ".next") {
			continue
		}
		rel, _ := filepath.Rel(root, filepath.Join(path, name))
		indent := strings.Repeat("  ", level)
		if e.IsDir() {
			sb.WriteString(indent + name + "/\n")
			if level+1 < maxDepth {
				_ = walkTree(root, filepath.Join(path, name), level+1, maxDepth, sb)
			}
		} else {
			sb.WriteString(indent + rel[strings.LastIndex(rel, string(filepath.Separator))+1:] + "\n")
		}
	}
	return nil
}

// ── find_symbol ────────────────────────────────────────────────────────────

type findSymbolParams struct {
	Name string `json:"name" description:"Symbol name or partial name"`
	Path string `json:"path,omitempty" description:"File or directory to search (default: .)"`
}

// ctagsEntry is the JSON output format from universal-ctags --output-format=json
type ctagsEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Line    int    `json:"line"`
	End     int    `json:"end"` // populated with --fields=+e
	Kind    string `json:"kind"`
	Pattern string `json:"pattern"`
}

func findSymbolTool(cwd string) tools.Tool {
	return tools.Func("Find Symbol", "Find where a symbol is defined. Returns file:line-range for each match. Uses ctags (language-agnostic).", "find_symbol",
		func(r tools.Runner, p findSymbolParams) tools.Result {
			searchPath := cwd
			if p.Path != "" {
				searchPath = resolvePath(cwd, p.Path)
			}

			ctx, cancel := context.WithTimeout(r.Context(), toolTimeout)
			defer cancel()

			entries, err := runCtags(ctx, searchPath)
			if err != nil {
				// ctags not available — fall back to grep
				return findSymbolGrep(ctx, cwd, searchPath, p.Name)
			}

			// Filter by name (case-insensitive partial match)
			nameLower := strings.ToLower(p.Name)
			var matches []string
			for _, e := range entries {
				if strings.Contains(strings.ToLower(e.Name), nameLower) {
					rel, _ := filepath.Rel(cwd, e.Path)
					if rel == "" {
						rel = e.Path
					}
					loc := fmt.Sprintf("%s:%d", rel, e.Line)
					if e.End > 0 {
						loc = fmt.Sprintf("%s:%d-%d", rel, e.Line, e.End)
					}
					matches = append(matches, fmt.Sprintf("%s\t%s\t%s", loc, e.Kind, e.Name))
				}
			}

			if len(matches) == 0 {
				return tools.SuccessFromString(fmt.Sprintf("Symbol %q not found.", p.Name))
			}
			if len(matches) > maxFindSymbols {
				matches = matches[:maxFindSymbols]
			}
			return tools.SuccessFromString(strings.Join(matches, "\n"))
		})
}

func runCtags(ctx context.Context, path string) ([]ctagsEntry, error) {
	args := []string{
		"--output-format=json",
		"--fields=+ne", // n=line number, e=end line
		"-f", "-",      // output to stdout
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		args = append(args, "-R", path)
	} else {
		args = append(args, path)
	}

	cmd := exec.CommandContext(ctx, "ctags", args...)
	out, err := cmd.Output()
	if err != nil {
		// ctags exits non-zero on warnings sometimes; try to parse anyway
		if len(out) == 0 {
			return nil, err
		}
	}

	var entries []ctagsEntry
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var e ctagsEntry
		if json.Unmarshal(line, &e) == nil {
			entries = append(entries, e)
		}
	}
	return entries, nil
}

func findSymbolGrep(ctx context.Context, cwd, searchPath, name string) tools.Result {
	cmd := exec.CommandContext(ctx, "grep", "-rn", "-w", "--", name, searchPath)
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return tools.SuccessFromString(fmt.Sprintf("Symbol %q not found.", name))
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > maxFindSymbols {
		lines = lines[:maxFindSymbols]
	}
	var refs []string
	for _, l := range lines {
		parts := strings.SplitN(l, ":", 3)
		if len(parts) >= 2 {
			rel, _ := filepath.Rel(cwd, parts[0])
			if rel == "" {
				rel = parts[0]
			}
			refs = append(refs, rel+":"+parts[1])
		}
	}
	return tools.SuccessFromString(strings.Join(refs, "\n"))
}

// ── read_file ──────────────────────────────────────────────────────────────

type readFileParams struct {
	Path      string `json:"path" description:"File path"`
	StartLine *int   `json:"start_line,omitempty" description:"First line (1-based)"`
	EndLine   *int   `json:"end_line,omitempty" description:"Last line (1-based)"`
}

func readFileTool(cwd string) tools.Tool {
	return tools.Func("Read File", "Read file contents with line numbers. Use start_line/end_line to read a range.", "read_file",
		func(r tools.Runner, p readFileParams) tools.Result {
			target := resolvePath(cwd, p.Path)

			if p.StartLine != nil || p.EndLine != nil {
				return readFileRange(target, p.StartLine, p.EndLine)
			}

			info, err := os.Stat(target)
			if err != nil {
				return tools.Error(err)
			}
			if info.Size() > maxReadFileSize {
				return tools.SuccessFromString(fmt.Sprintf("File too large (%d bytes). Use start_line/end_line.", info.Size()))
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
	end := -1
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
	return tools.Func("Search Code", "Search for a pattern across files. Returns file:line:match. Max 80 results.", "search_code",
		func(r tools.Runner, p searchCodeParams) tools.Result {
			searchPath := cwd
			if p.Path != "" {
				searchPath = resolvePath(cwd, p.Path)
			}
			args := []string{"-rn"}
			if p.FileGlob != "" {
				args = append(args, "--include="+p.FileGlob)
			}
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
				if result == "" {
					return tools.SuccessFromString("No matches.")
				}
			}
			lines := strings.Split(result, "\n")
			if len(lines) > maxSearchResults {
				lines = lines[:maxSearchResults]
			}
			return tools.SuccessFromString(strings.Join(lines, "\n"))
		})
}

// ── get_function ───────────────────────────────────────────────────────────

type getFunctionParams struct {
	Name string `json:"name" description:"Function/method/class name"`
	Path string `json:"path" description:"File path"`
}

func getFunctionTool(cwd string) tools.Tool {
	return tools.Func("Get Function", "Get the full source of a named function/method/class from a file. 'name' must be the function name (e.g. 'MyFunc'), NOT a filename.", "get_function",
		func(r tools.Runner, p getFunctionParams) tools.Result {
			target := resolvePath(cwd, p.Path)
			data, err := os.ReadFile(target)
			if err != nil {
				return tools.Error(err)
			}
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

// ── callers ────────────────────────────────────────────────────────────────

type callersParams struct {
	Name string `json:"name" description:"Function/symbol name to find call sites for"`
	Path string `json:"path,omitempty" description:"Directory or file to search (default: .)"`
}

func callersTool(cwd string) tools.Tool {
	return tools.Func("Callers", "Find all call sites of a function. Returns file:line and the calling line snippet — not the full function body. Use this to understand usage patterns and blast radius before changing a function.", "callers",
		func(r tools.Runner, p callersParams) tools.Result {
			searchPath := cwd
			if p.Path != "" {
				searchPath = resolvePath(cwd, p.Path)
			}
			ctx, cancel := context.WithTimeout(r.Context(), toolTimeout)
			defer cancel()

			// Grep for all occurrences of the name
			cmd := exec.CommandContext(ctx, "grep", "-rn", "--", p.Name, searchPath)
			out, err := cmd.Output()
			if (err != nil && len(out) == 0) || len(out) == 0 {
				return tools.SuccessFromString(fmt.Sprintf("No call sites found for %q.", p.Name))
			}

			// Get definition locations via ctags so we can exclude them
			defLines := map[string]bool{}
			if entries, cerr := runCtags(ctx, searchPath); cerr == nil {
				for _, e := range entries {
					if strings.EqualFold(e.Name, p.Name) {
						defLines[fmt.Sprintf("%s:%d", e.Path, e.Line)] = true
					}
				}
			}

			var callSites []string
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				if line == "" {
					continue
				}
				parts := strings.SplitN(line, ":", 3)
				if len(parts) < 3 {
					continue
				}
				file, lineNum, content := parts[0], parts[1], strings.TrimSpace(parts[2])

				// Skip definition lines (by ctags lookup or by regex)
				if defLines[file+":"+lineNum] {
					continue
				}
				if funcStartRe.MatchString(content) && strings.Contains(content, p.Name) {
					continue
				}
				// Skip comment-only lines
				if isCommentLine(content) {
					continue
				}
				// Skip import/require lines — not call sites
				if isImportLine(content) {
					continue
				}
				// Must look like a call: name followed by ( somewhere on the line
				callRe := regexp.MustCompile(`\b` + regexp.QuoteMeta(p.Name) + `\s*[(\[]`)
				if !callRe.MatchString(line) {
					continue
				}

				rel, _ := filepath.Rel(cwd, file)
				if rel == "" {
					rel = file
				}
				// Trim content to keep output compact
				if len(content) > 120 {
					content = content[:120] + "…"
				}
				callSites = append(callSites, fmt.Sprintf("%s:%s\t%s", rel, lineNum, content))
			}

			if len(callSites) == 0 {
				return tools.SuccessFromString(fmt.Sprintf("No call sites found for %q (only definitions/imports).", p.Name))
			}
			if len(callSites) > maxSearchResults {
				callSites = callSites[:maxSearchResults]
			}
			return tools.SuccessFromString(strings.Join(callSites, "\n"))
		})
}

// isCommentLine returns true if the trimmed line is a comment in common languages.
func isCommentLine(s string) bool {
	return strings.HasPrefix(s, "//") || strings.HasPrefix(s, "#") ||
		strings.HasPrefix(s, "*") || strings.HasPrefix(s, "/*") ||
		strings.HasPrefix(s, "--")
}

// isImportLine returns true if the line is an import/require statement.
func isImportLine(s string) bool {
	return strings.HasPrefix(s, "import ") || strings.HasPrefix(s, "from ") ||
		strings.Contains(s, "require(") || strings.HasPrefix(s, "use ")
}

// ── callees ────────────────────────────────────────────────────────────────

type calleesParams struct {
	Name string `json:"name" description:"Function name"`
	Path string `json:"path" description:"File containing the function"`
}

func calleesTool(cwd string) tools.Tool {
	return tools.Func("Callees", "List functions called by the named function. Returns names and their definition locations — no source. Use to understand what a function depends on before modifying it.", "callees",
		func(r tools.Runner, p calleesParams) tools.Result {
			target := resolvePath(cwd, p.Path)
			data, err := os.ReadFile(target)
			if err != nil {
				return tools.Error(err)
			}
			lines := strings.Split(string(data), "\n")

			// Find the function body
			pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(p.Name) + `\b`)
			startIdx := -1
			for i, line := range lines {
				if pattern.MatchString(line) && funcStartRe.MatchString(line) {
					startIdx = i
					break
				}
			}
			if startIdx < 0 {
				return tools.SuccessFromString(fmt.Sprintf("Function %q not found in %s", p.Name, target))
			}
			endIdx, found := findFunctionEnd(lines, startIdx)
			if !found {
				endIdx = len(lines)
			}
			body := lines[startIdx:endIdx]

			// Extract call expressions: word followed by (
			callRe := regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
			// Keywords to skip
			keywords := map[string]bool{
				"if": true, "for": true, "while": true, "switch": true,
				"func": true, "function": true, "def": true, "class": true,
				"return": true, "new": true, "type": true, "var": true,
				"const": true, "let": true, "else": true, "case": true,
				p.Name: true, // don't list self
			}

			seen := map[string]bool{}
			var callees []string
			for _, line := range body {
				trimmed := strings.TrimSpace(line)
				if isCommentLine(trimmed) {
					continue
				}
				for _, m := range callRe.FindAllStringSubmatch(trimmed, -1) {
					name := m[1]
					if keywords[name] || seen[name] {
						continue
					}
					seen[name] = true
					callees = append(callees, name)
				}
			}

			if len(callees) == 0 {
				return tools.SuccessFromString(fmt.Sprintf("%s calls no other functions.", p.Name))
			}

			// Look up definition locations for each callee
			ctx, cancel := context.WithTimeout(r.Context(), toolTimeout)
			defer cancel()
			entries, _ := runCtags(ctx, filepath.Dir(target))

			defMap := map[string]string{}
			for _, e := range entries {
				rel, _ := filepath.Rel(cwd, e.Path)
				if rel == "" {
					rel = e.Path
				}
				defMap[e.Name] = fmt.Sprintf("%s:%d", rel, e.Line)
			}

			var lines2 []string
			for _, name := range callees {
				if loc, ok := defMap[name]; ok {
					lines2 = append(lines2, fmt.Sprintf("%s\t%s", name, loc))
				} else {
					lines2 = append(lines2, name) // stdlib or external
				}
			}
			return tools.SuccessFromString(strings.Join(lines2, "\n"))
		})
}

// ── symbol_context ─────────────────────────────────────────────────────────

type symbolContextParams struct {
	Name string `json:"name" description:"Symbol name (exact or partial)"`
	Path string `json:"path,omitempty" description:"Narrow search to this file or directory"`
}

func symbolContextTool(cwd string) tools.Tool {
	return tools.Func("Symbol Context", "Orientation snapshot for a symbol: definition location, signature line, caller count, callee count. No source dumped. Use this first to decide whether to dig deeper.", "symbol_context",
		func(r tools.Runner, p symbolContextParams) tools.Result {
			searchPath := cwd
			if p.Path != "" {
				searchPath = resolvePath(cwd, p.Path)
			}
			ctx, cancel := context.WithTimeout(r.Context(), toolTimeout)
			defer cancel()

			entries, err := runCtags(ctx, searchPath)
			if err != nil || len(entries) == 0 {
				return tools.SuccessFromString(fmt.Sprintf("Symbol %q not found (ctags unavailable or no matches).", p.Name))
			}

			nameLower := strings.ToLower(p.Name)
			var matches []ctagsEntry
			for _, e := range entries {
				if strings.EqualFold(e.Name, p.Name) {
					matches = append(matches, e) // exact first
				} else if strings.Contains(strings.ToLower(e.Name), nameLower) {
					matches = append(matches, e)
				}
			}
			if len(matches) == 0 {
				return tools.SuccessFromString(fmt.Sprintf("Symbol %q not found.", p.Name))
			}

			// Deduplicate to exact matches if any exist
			var exact []ctagsEntry
			for _, e := range matches {
				if strings.EqualFold(e.Name, p.Name) {
					exact = append(exact, e)
				}
			}
			if len(exact) > 0 {
				matches = exact
			}

			var sb strings.Builder
			for _, e := range matches {
				rel, _ := filepath.Rel(cwd, e.Path)
				if rel == "" {
					rel = e.Path
				}

				// Signature: the pattern field from ctags is the matched line
				sig := strings.TrimSpace(e.Pattern)
				sig = strings.TrimPrefix(sig, "/^")
				sig = strings.TrimSuffix(sig, "$/")
				sig = strings.TrimSpace(sig)
				if len(sig) > 100 {
					sig = sig[:100] + "…"
				}

				loc := fmt.Sprintf("%s:%d", rel, e.Line)
				if e.End > 0 {
					loc = fmt.Sprintf("%s:%d-%d", rel, e.Line, e.End)
				}

				sb.WriteString(fmt.Sprintf("symbol:  %s (%s)\n", e.Name, e.Kind))
				sb.WriteString(fmt.Sprintf("loc:     %s\n", loc))
				sb.WriteString(fmt.Sprintf("sig:     %s\n", sig))

				// Count callers (grep, fast)
				callerCount := 0
				cmd := exec.CommandContext(ctx, "grep", "-r", "--", e.Name, searchPath)
				if out, gerr := cmd.Output(); gerr == nil {
					callRe := regexp.MustCompile(`\b` + regexp.QuoteMeta(e.Name) + `\s*[(\[]`)
					for _, line := range strings.Split(string(out), "\n") {
						parts := strings.SplitN(line, ":", 3)
						if len(parts) < 3 {
							continue
						}
						content := strings.TrimSpace(parts[2])
						if isCommentLine(content) || isImportLine(content) {
							continue
						}
						if callRe.MatchString(line) && parts[0] != e.Path {
							callerCount++
						}
					}
				}
				sb.WriteString(fmt.Sprintf("callers: %d\n", callerCount))

				// Count callees from body
				calleeCount := 0
				if e.End > 0 {
					if fileData, ferr := os.ReadFile(e.Path); ferr == nil {
						fileLines := strings.Split(string(fileData), "\n")
						start, end := e.Line-1, e.End
						if end > len(fileLines) {
							end = len(fileLines)
						}
						callRe2 := regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
						seen := map[string]bool{e.Name: true}
						keywords := map[string]bool{"if": true, "for": true, "while": true, "switch": true, "func": true, "function": true, "def": true, "return": true, "new": true}
						for _, fl := range fileLines[start:end] {
							if isCommentLine(strings.TrimSpace(fl)) {
								continue
							}
							for _, m := range callRe2.FindAllStringSubmatch(fl, -1) {
								n := m[1]
								if !keywords[n] && !seen[n] {
									seen[n] = true
									calleeCount++
								}
							}
						}
					}
				}
				sb.WriteString(fmt.Sprintf("callees: %d\n", calleeCount))

				if len(matches) > 1 {
					sb.WriteString("---\n")
				}
			}

			return tools.SuccessFromString(strings.TrimRight(sb.String(), "\n"))
		})
}

// ── grep_files ─────────────────────────────────────────────────────────────

type grepFilesParams struct {
	Pattern  string `json:"pattern" description:"Regex pattern to search for"`
	Path     string `json:"path,omitempty" description:"Search root (default: .)"`
	FileGlob string `json:"file_glob,omitempty" description:"File filter, e.g. '*.go'"`
}

func grepFilesTool(cwd string) tools.Tool {
	return tools.Func("Grep Files", "Find which files contain a pattern — file paths only, no line content. Much cheaper than search_code when you just need to know if/where something exists.", "grep_files",
		func(r tools.Runner, p grepFilesParams) tools.Result {
			searchPath := cwd
			if p.Path != "" {
				searchPath = resolvePath(cwd, p.Path)
			}
			args := []string{"-rl"}
			if p.FileGlob != "" {
				args = append(args, "--include="+p.FileGlob)
			}
			args = append(args, "--", p.Pattern, searchPath)
			ctx, cancel := context.WithTimeout(r.Context(), toolTimeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, "grep", args...)
			out, err := cmd.Output()
			if err != nil || len(out) == 0 {
				return tools.SuccessFromString("No files match.")
			}
			var relPaths []string
			for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				rel, _ := filepath.Rel(cwd, f)
				if rel == "" {
					rel = f
				}
				relPaths = append(relPaths, rel)
			}
			return tools.SuccessFromString(strings.Join(relPaths, "\n"))
		})
}

// ── Helpers ────────────────────────────────────────────────────────────────

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
