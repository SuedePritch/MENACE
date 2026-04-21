package indexer

// Indexer is the interface for code intelligence backends.
// The built-in TS/JS indexer implements this via code-indexer package.
// Users can provide external indexers for other languages.
type Indexer interface {
	// Extensions returns file extensions this indexer handles.
	Extensions() []string

	// IndexDir indexes all supported files in a directory tree.
	IndexDir(dir string, workers int) (*Report, error)

	// SymbolsInFile returns symbols in a specific file.
	SymbolsInFile(filePath string) ([]Symbol, error)

	// FindSymbol finds symbols by name, optionally scoped to a file.
	FindSymbol(name string, filePath string) ([]Symbol, error)
}

// Symbol is the MENACE-side symbol representation.
type Symbol struct {
	Name          string
	Kind          string // "function", "class", "method", "type", "interface", "enum"
	FilePath      string
	StartLine     int
	EndLine       int
	Source        string   // full source text
	ExportStatus  string   // "exported", "unexported", "default"
	Dependencies  []string
	Dependents    []string
}

// Report is the full index output.
type Report struct {
	Symbols       []Symbol
	TokenEstimate int
}
