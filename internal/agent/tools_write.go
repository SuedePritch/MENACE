package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/flitsinc/go-llms/tools"
)

// Pre-compiled regexes for write tool handlers.
var (
	// funcStartWriteRe matches function starts including arrow functions.
	funcStartWriteRe = regexp.MustCompile(`^(func |.*function |.*class |def |.*=>)`)
	// funcStartInsertRe matches function starts for insert_after (no arrow fns).
	funcStartInsertRe = regexp.MustCompile(`^(func |.*function |.*class )`)
	// importRequireRe matches CommonJS require() imports.
	importRequireRe = regexp.MustCompile(`^(const|let|var)\s.*=\s*require\(`)
)

// WriteTools returns read + write tools (for worker).
func WriteTools(cwd string) []tools.Tool {
	t := ReadTools(cwd)
	t = append(t,
		writeFileTool(cwd),
		editFileTool(cwd),
		replaceFunctionTool(cwd),
		insertAfterTool(cwd),
		addImportTool(cwd),
		diffPreviewTool(cwd),
	)
	return t
}

// ── write_file ─────────────────────────────────────────────────────────────

type writeFileParams struct {
	Path    string `json:"path" description:"File path"`
	Content string `json:"content" description:"Full file content"`
}

const maxWriteFileSize = 1024 * 1024 // 1MB

func writeFileTool(cwd string) tools.Tool {
	return tools.Func("Write File", "Write full content to a file (create or overwrite). Prefer edit_file for modifications.", "write_file",
		func(r tools.Runner, p writeFileParams) tools.Result {
			if len(p.Content) > maxWriteFileSize {
				return tools.SuccessFromString(fmt.Sprintf("Content too large (%d bytes, max %d).", len(p.Content), maxWriteFileSize))
			}
			target := resolvePath(cwd, p.Path)
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return tools.Error(err)
			}
			if err := os.WriteFile(target, []byte(p.Content), 0644); err != nil { return tools.Error(err) }
			return tools.SuccessFromString(fmt.Sprintf("Wrote %d lines to %s", len(strings.Split(p.Content, "\n")), target))
		})
}

// ── edit_file ──────────────────────────────────────────────────────────────

type editFileParams struct {
	Path      string `json:"path" description:"File path"`
	OldString string `json:"old_string" description:"Exact text to find (must be unique)"`
	NewString string `json:"new_string" description:"Replacement text"`
}

func editFileTool(cwd string) tools.Tool {
	return tools.Func("Edit File", "Replace an exact string in a file. old_string must be unique. Always read_file first.", "edit_file",
		func(r tools.Runner, p editFileParams) tools.Result {
			target := resolvePath(cwd, p.Path)
			data, err := os.ReadFile(target)
			if err != nil { return tools.Error(err) }
			content := string(data)
			count := strings.Count(content, p.OldString)
			if count == 0 { return tools.SuccessFromString("Error: old_string not found. Read the file first.") }
			if count > 1 { return tools.SuccessFromString(fmt.Sprintf("Error: old_string found %d times. Add more context.", count)) }
			content = strings.Replace(content, p.OldString, p.NewString, 1)
			if err := os.WriteFile(target, []byte(content), 0644); err != nil { return tools.Error(err) }
			return tools.SuccessFromString(fmt.Sprintf("Edited %s - 1 replacement.", target))
		})
}

// ── replace_function ───────────────────────────────────────────────────────

type replaceFunctionParams struct {
	Path      string `json:"path" description:"File path"`
	Name      string `json:"name" description:"Function/method name to replace"`
	NewSource string `json:"new_source" description:"Complete new function source (including signature)"`
}

func replaceFunctionTool(cwd string) tools.Tool {
	return tools.Func("Replace Function", "Replace an entire function body by name using brace-matching.", "replace_function",
		func(r tools.Runner, p replaceFunctionParams) tools.Result {
			target := resolvePath(cwd, p.Path)
			data, err := os.ReadFile(target)
			if err != nil { return tools.Error(err) }
			lines := strings.Split(string(data), "\n")
			pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(p.Name) + `\b`)
			var startLine, endLine int
			found := false
			for i, line := range lines {
				if pattern.MatchString(line) && funcStartWriteRe.MatchString(line) {
					startLine = i
					endLine, found = findFunctionEnd(lines, i)
					break
				}
			}
			if !found { return tools.SuccessFromString(fmt.Sprintf("Function %q not found in %s", p.Name, target)) }
			updated := make([]string, 0, len(lines))
			updated = append(updated, lines[:startLine]...)
			updated = append(updated, p.NewSource)
			updated = append(updated, lines[endLine:]...)
			if err := os.WriteFile(target, []byte(strings.Join(updated, "\n")), 0644); err != nil { return tools.Error(err) }
			return tools.SuccessFromString(fmt.Sprintf("Replaced %q at lines %d-%d in %s", p.Name, startLine+1, endLine, target))
		})
}

// ── insert_after ───────────────────────────────────────────────────────────

type insertAfterParams struct {
	Path          string  `json:"path" description:"File path"`
	Content       string  `json:"content" description:"Code to insert"`
	AfterLine     *int    `json:"after_line,omitempty" description:"Line number to insert after (1-based)"`
	AfterFunction *string `json:"after_function,omitempty" description:"Insert after this function"`
}

func insertAfterTool(cwd string) tools.Tool {
	return tools.Func("Insert After", "Insert code after a line number or after a named function.", "insert_after",
		func(r tools.Runner, p insertAfterParams) tools.Result {
			target := resolvePath(cwd, p.Path)
			data, err := os.ReadFile(target)
			if err != nil { return tools.Error(err) }
			lines := strings.Split(string(data), "\n")
			insertAt := -1
			if p.AfterLine != nil {
				insertAt = *p.AfterLine
			} else if p.AfterFunction != nil {
				pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(*p.AfterFunction) + `\b`)
				for i, line := range lines {
					if pattern.MatchString(line) && funcStartInsertRe.MatchString(line) {
						insertAt, _ = findFunctionEnd(lines, i)
						break
					}
				}
				if insertAt < 0 { return tools.SuccessFromString(fmt.Sprintf("Function %q not found.", *p.AfterFunction)) }
			} else {
				return tools.SuccessFromString("Provide either after_line or after_function.")
			}
			newLines := make([]string, 0, len(lines)+1)
			newLines = append(newLines, lines[:insertAt]...)
			newLines = append(newLines, p.Content)
			newLines = append(newLines, lines[insertAt:]...)
			if err := os.WriteFile(target, []byte(strings.Join(newLines, "\n")), 0644); err != nil { return tools.Error(err) }
			return tools.SuccessFromString(fmt.Sprintf("Inserted %d lines after line %d in %s", len(strings.Split(p.Content, "\n")), insertAt, target))
		})
}

// ── add_import ─────────────────────────────────────────────────────────────

type addImportParams struct {
	Path       string `json:"path" description:"File path"`
	ImportLine string `json:"import_line" description:"Full import statement"`
}

func addImportTool(cwd string) tools.Tool {
	return tools.Func("Add Import", "Add an import statement, auto-deduplicating. Skips if already present.", "add_import",
		func(r tools.Runner, p addImportParams) tools.Result {
			target := resolvePath(cwd, p.Path)
			data, err := os.ReadFile(target)
			if err != nil { return tools.Error(err) }
			content := string(data)
			if strings.Contains(content, strings.TrimSpace(p.ImportLine)) {
				return tools.SuccessFromString("Import already exists. Skipped.")
			}
			lines := strings.Split(content, "\n")
			lastImport := -1
			for i, line := range lines {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "import ") || importRequireRe.MatchString(trimmed) {
					lastImport = i
					if !strings.Contains(trimmed, "from ") && !strings.HasSuffix(trimmed, ";") && !strings.HasSuffix(trimmed, `"`) {
						for j := i + 1; j < len(lines); j++ {
							lastImport = j
							jt := strings.TrimSpace(lines[j])
							if strings.Contains(jt, "from ") || strings.HasSuffix(jt, ";") || jt == ")" { break }
						}
					}
				}
			}
			if lastImport >= 0 {
				newLines := make([]string, 0, len(lines)+1)
				newLines = append(newLines, lines[:lastImport+1]...)
				newLines = append(newLines, p.ImportLine)
				newLines = append(newLines, lines[lastImport+1:]...)
				lines = newLines
			} else {
				lines = append([]string{p.ImportLine}, lines...)
			}
			if err := os.WriteFile(target, []byte(strings.Join(lines, "\n")), 0644); err != nil { return tools.Error(err) }
			return tools.SuccessFromString(fmt.Sprintf("Added import at line %d in %s", lastImport+2, target))
		})
}

// ── diff_preview ───────────────────────────────────────────────────────────

type diffPreviewParams struct {
	Path      string `json:"path" description:"File path"`
	OldString string `json:"old_string" description:"Text to replace"`
	NewString string `json:"new_string" description:"Replacement text"`
}

func diffPreviewTool(cwd string) tools.Tool {
	return tools.Func("Diff Preview", "Preview what an edit_file would produce without writing. Shows a unified diff.", "diff_preview",
		func(r tools.Runner, p diffPreviewParams) tools.Result {
			target := resolvePath(cwd, p.Path)
			data, err := os.ReadFile(target)
			if err != nil { return tools.Error(err) }
			content := string(data)
			count := strings.Count(content, p.OldString)
			if count == 0 { return tools.SuccessFromString("old_string not found.") }
			if count > 1 { return tools.SuccessFromString(fmt.Sprintf("old_string found %d times - must be unique.", count)) }
			idx := strings.Index(content, p.OldString)
			lineNum := strings.Count(content[:idx], "\n") + 1
			oldLines := strings.Split(p.OldString, "\n")
			newLines := strings.Split(p.NewString, "\n")
			var diff strings.Builder
			fmt.Fprintf(&diff, "@@ line %d @@\n", lineNum)
			for _, l := range oldLines { fmt.Fprintf(&diff, "- %s\n", l) }
			for _, l := range newLines { fmt.Fprintf(&diff, "+ %s\n", l) }
			return tools.SuccessFromString(diff.String())
		})
}
