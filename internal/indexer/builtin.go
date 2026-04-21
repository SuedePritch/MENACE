package indexer

import (
	ci "github.com/jamespritchard/code-indexer"
)

// BuiltinTSIndexer wraps the code-indexer package as a MENACE Indexer.
type BuiltinTSIndexer struct {
	ts *ci.TSIndexer
}

// NewBuiltinTSIndexer creates the default TS/JS indexer.
func NewBuiltinTSIndexer() *BuiltinTSIndexer {
	return &BuiltinTSIndexer{ts: ci.NewTSIndexer()}
}

func (b *BuiltinTSIndexer) Extensions() []string {
	return b.ts.Extensions()
}

func (b *BuiltinTSIndexer) IndexDir(dir string, workers int) (*Report, error) {
	ciReport, err := b.ts.IndexDir(dir, workers)
	if err != nil {
		return nil, err
	}
	return convertReport(ciReport), nil
}

func (b *BuiltinTSIndexer) SymbolsInFile(filePath string) ([]Symbol, error) {
	ciSyms, err := b.ts.SymbolsInFile(filePath)
	if err != nil {
		return nil, err
	}
	return convertSymbols(ciSyms), nil
}

func (b *BuiltinTSIndexer) FindSymbol(name string, filePath string) ([]Symbol, error) {
	ciSyms, err := b.ts.FindSymbol(name, filePath)
	if err != nil {
		return nil, err
	}
	return convertSymbols(ciSyms), nil
}

func convertReport(r *ci.IndexReport) *Report {
	var syms []Symbol
	for _, s := range r.Symbols {
		syms = append(syms, Symbol{
			Name:         s.Name,
			Kind:         string(s.Kind),
			FilePath:     s.FilePath,
			StartLine:    s.StartLine,
			EndLine:      s.EndLine,
			Source:       s.SourceSnippet,
			ExportStatus: string(s.ExportStatus),
			Dependencies: s.Dependencies,
			Dependents:   s.Dependents,
		})
	}
	return &Report{Symbols: syms, TokenEstimate: r.TokenEstimate}
}

func convertSymbols(ciSyms []ci.SymbolReport) []Symbol {
	var syms []Symbol
	for _, s := range ciSyms {
		syms = append(syms, Symbol{
			Name:         s.Name,
			Kind:         string(s.Kind),
			FilePath:     s.FilePath,
			StartLine:    s.StartLine,
			EndLine:      s.EndLine,
			Source:       s.SourceSnippet,
			ExportStatus: string(s.ExportStatus),
			Dependencies: s.Dependencies,
			Dependents:   s.Dependents,
		})
	}
	return syms
}
