package indexer

import (
	"os"
	"strings"
)

// Indexer is the interface that MENACE (or any consumer) uses to query code symbols.
// Implement this for a new language backend.
type Indexer interface {
	// Extensions returns the file extensions this indexer handles (e.g. [".ts", ".tsx", ".js", ".jsx"]).
	Extensions() []string

	// IndexDir indexes all supported files in a directory tree and returns the full report.
	IndexDir(dir string, workers int) (*IndexReport, error)

	// SymbolsInFile returns all symbols in a specific file.
	SymbolsInFile(filePath string) ([]SymbolReport, error)

	// FindSymbol finds symbols by name, optionally scoped to a file.
	// If filePath is empty, searches the last indexed directory.
	FindSymbol(name string, filePath string) ([]SymbolReport, error)
}

// TSIndexer is the built-in TypeScript/JavaScript indexer using tree-sitter.
type TSIndexer struct {
	lastReport *IndexReport
}

func NewTSIndexer() *TSIndexer {
	return &TSIndexer{}
}

func (t *TSIndexer) Extensions() []string {
	return []string{".ts", ".tsx", ".js", ".jsx"}
}

func (t *TSIndexer) IndexDir(dir string, workers int) (*IndexReport, error) {
	if workers < 1 {
		workers = 4
	}

	cfg := DefaultCrawlerConfig()
	cfg.Workers = workers

	files, err := CrawlFiles(dir, cfg)
	if err != nil {
		return nil, err
	}

	results := IndexFiles(files, workers)

	fileSymbols := make(map[string][]Symbol)
	fileImports := make(map[string][]ImportInfo)

	for _, r := range results {
		if r.Err != nil {
			continue
		}
		fileSymbols[r.FilePath] = r.Symbols

		data, err := os.ReadFile(r.FilePath)
		if err == nil {
			imports, err := ResolveImports(r.FilePath, data)
			if err == nil {
				fileImports[r.FilePath] = imports
			}
		}
	}

	// Compute hashes
	for path, syms := range fileSymbols {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		for i := range syms {
			if syms[i].Kind == SymbolFunction || syms[i].Kind == SymbolMethod {
				snippet := extractLinesHelper(lines, syms[i].StartLine, syms[i].EndLine)
				syms[i].BodyHash = ComputeBodyHash(snippet)
				syms[i].StructuralHash = ComputeStructuralHash(snippet)
			}
		}
		fileSymbols[path] = syms
	}

	// Build call graph
	BuildCallGraph(fileSymbols, fileImports)

	report, err := GenerateReport(fileSymbols, nil)
	if err != nil {
		return nil, err
	}

	t.lastReport = report
	return report, nil
}

func (t *TSIndexer) SymbolsInFile(filePath string) ([]SymbolReport, error) {
	// If we have a cached report, use it
	if t.lastReport != nil {
		var syms []SymbolReport
		for _, s := range t.lastReport.Symbols {
			if s.FilePath == filePath {
				syms = append(syms, s)
			}
		}
		if len(syms) > 0 {
			return syms, nil
		}
	}

	// Parse just this file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	symbols, err := ParseFile(filePath, data)
	if err != nil {
		return nil, err
	}

	source := string(data)
	var reports []SymbolReport
	for _, s := range symbols {
		snippet := extractSnippet(source, s.StartLine, s.EndLine)
		reports = append(reports, SymbolReport{
			Name:          s.Name,
			Kind:          s.Kind,
			FilePath:      s.FilePath,
			StartLine:     s.StartLine,
			EndLine:       s.EndLine,
			SourceSnippet: snippet,
			ExportStatus:  s.ExportStatus,
		})
	}
	return reports, nil
}

func (t *TSIndexer) FindSymbol(name string, filePath string) ([]SymbolReport, error) {
	// Search cached report first
	if t.lastReport != nil {
		var matches []SymbolReport
		for _, s := range t.lastReport.Symbols {
			if s.Name == name && (filePath == "" || s.FilePath == filePath) {
				matches = append(matches, s)
			}
		}
		if len(matches) > 0 {
			return matches, nil
		}
	}

	// If scoped to a file, parse it
	if filePath != "" {
		syms, err := t.SymbolsInFile(filePath)
		if err != nil {
			return nil, err
		}
		var matches []SymbolReport
		for _, s := range syms {
			if s.Name == name {
				matches = append(matches, s)
			}
		}
		return matches, nil
	}

	return nil, nil
}

func extractLinesHelper(lines []string, start, end int) string {
	if start < 1 {
		start = 1
	}
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start-1:end], "\n")
}
