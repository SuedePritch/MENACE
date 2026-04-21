package indexer

import (
	"encoding/json"
	"os"
	"strings"
)

// GenerateReport builds the full IndexReport from parsed file symbols.
// When patternCfg is non-nil, sub-function pattern duplicate detection is enabled.
func GenerateReport(fileSymbols map[string][]Symbol, patternCfg *PatternConfig) (*IndexReport, error) {
	var allSymbols []Symbol
	for _, syms := range fileSymbols {
		allSymbols = append(allSymbols, syms...)
	}

	// Compute hashes and populate source snippets
	var reports []SymbolReport
	for _, s := range allSymbols {
		snippet := ""
		if s.FilePath != "" {
			data, err := os.ReadFile(s.FilePath)
			if err == nil {
				snippet = extractSnippet(string(data), s.StartLine, s.EndLine)
			}
		}

		reports = append(reports, SymbolReport{
			Name:              s.Name,
			Kind:              s.Kind,
			FilePath:          s.FilePath,
			StartLine:         s.StartLine,
			EndLine:           s.EndLine,
			SourceSnippet:     snippet,
			Dependencies:      s.Dependencies,
			Dependents:        s.Dependents,
			StructuralHash:    s.StructuralHash,
			ExportStatus:      s.ExportStatus,
			PotentiallyUnused: s.ExportStatus == ExportUnexported && len(s.Dependents) == 0 && (s.Kind == SymbolFunction || s.Kind == SymbolMethod),
		})
	}

	dupGroups := FindDuplicateGroups(allSymbols)

	// Estimate tokens (chars/4)
	jsonBytes, _ := json.Marshal(reports)
	tokenEstimate := len(jsonBytes) / 4

	report := &IndexReport{
		Symbols:           reports,
		DuplicationGroups: dupGroups,
		TokenEstimate:     tokenEstimate,
	}

	// Sub-function pattern detection (opt-in)
	if patternCfg != nil {
		report.PatternGroups = FindPatternDuplicates(fileSymbols, *patternCfg)
	}

	return report, nil
}

func extractSnippet(source string, startLine, endLine int) string {
	lines := strings.Split(source, "\n")
	if startLine < 1 {
		startLine = 1
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	selected := lines[startLine-1 : endLine]
	return strings.Join(selected, "\n")
}

// SerializeReport outputs the report as JSON.
func SerializeReport(report *IndexReport, compact bool) ([]byte, error) {
	if compact {
		return json.Marshal(report)
	}
	return json.MarshalIndent(report, "", "  ")
}
