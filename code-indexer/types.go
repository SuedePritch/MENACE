package indexer

// SymbolKind represents the type of a code symbol.
type SymbolKind string

const (
	SymbolFunction  SymbolKind = "function"
	SymbolClass     SymbolKind = "class"
	SymbolMethod    SymbolKind = "method"
	SymbolType      SymbolKind = "type"
	SymbolInterface SymbolKind = "interface"
	SymbolEnum      SymbolKind = "enum"
)

// ExportStatus represents the export visibility of a symbol.
type ExportStatus string

const (
	ExportNamed      ExportStatus = "exported"
	ExportDefault    ExportStatus = "default"
	ExportUnexported ExportStatus = "unexported"
)

// Symbol represents a parsed code symbol extracted from a source file.
type Symbol struct {
	Name           string       `json:"name"`
	Kind           SymbolKind   `json:"kind"`
	FilePath       string       `json:"filePath"`
	StartLine      int          `json:"startLine"`
	EndLine        int          `json:"endLine"`
	BodyHash       string       `json:"bodyHash,omitempty"`
	StructuralHash string       `json:"structuralHash,omitempty"`
	Dependencies   []string     `json:"dependencies,omitempty"`
	Dependents     []string     `json:"dependents,omitempty"`
	ExportStatus   ExportStatus `json:"exportStatus"`
}

// IndexReport is the top-level output schema for LLM consumption.
type IndexReport struct {
	Symbols           []SymbolReport      `json:"symbols"`
	DuplicationGroups []DuplicationGroup  `json:"duplicationGroups,omitempty"`
	PatternGroups     []PatternGroup      `json:"patternGroups,omitempty"`
	TokenEstimate     int                 `json:"tokenEstimate"`
}

// SymbolReport is the per-symbol entry in the index report.
type SymbolReport struct {
	Name           string       `json:"name"`
	Kind           SymbolKind   `json:"kind"`
	FilePath       string       `json:"filePath"`
	StartLine      int          `json:"startLine"`
	EndLine        int          `json:"endLine"`
	SourceSnippet  string       `json:"sourceSnippet"`
	Dependencies   []string     `json:"dependencies,omitempty"`
	Dependents     []string     `json:"dependents,omitempty"`
	StructuralHash string       `json:"structuralHash,omitempty"`
	ExportStatus   ExportStatus `json:"exportStatus"`
	PotentiallyUnused bool      `json:"potentiallyUnused,omitempty"`
}

// DuplicationGroup represents a set of symbols with identical structural hashes.
type DuplicationGroup struct {
	StructuralHash string   `json:"structuralHash"`
	Symbols        []string `json:"symbols"`
}

// PatternOccurrence records where a sub-function pattern appears.
type PatternOccurrence struct {
	FunctionName   string `json:"functionName"`
	FilePath       string `json:"filePath"`
	StartLine      int    `json:"startLine"`
	EndLine        int    `json:"endLine"`
	StatementCount int    `json:"statementCount"`
}

// PatternGroup represents a set of locations sharing an identical sub-function AST pattern.
type PatternGroup struct {
	StructuralHash string              `json:"structuralHash"`
	StatementCount int                 `json:"statementCount"`
	Occurrences    []PatternOccurrence `json:"occurrences"`
	ExampleSnippet string              `json:"exampleSnippet,omitempty"`
}
