package indexer

import (
	"encoding/json"
	"testing"
)

func TestReportValidJSON(t *testing.T) {
	fileSymbols := map[string][]Symbol{
		"../../testdata/simple.ts": {
			{Name: "greet", Kind: SymbolFunction, FilePath: "../../testdata/simple.ts", StartLine: 2, EndLine: 4, ExportStatus: ExportUnexported},
			{Name: "add", Kind: SymbolFunction, FilePath: "../../testdata/simple.ts", StartLine: 7, EndLine: 9, ExportStatus: ExportNamed},
		},
	}

	report, err := GenerateReport(fileSymbols, nil)
	if err != nil {
		t.Fatalf("GenerateReport: %v", err)
	}

	data, err := SerializeReport(report, false)
	if err != nil {
		t.Fatalf("SerializeReport: %v", err)
	}

	// Verify it's valid JSON
	var parsed IndexReport
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if len(parsed.Symbols) != 2 {
		t.Errorf("expected 2 symbols, got %d", len(parsed.Symbols))
	}
}

func TestReportTokenBudget(t *testing.T) {
	// Parse all testdata fixtures
	cfg := DefaultCrawlerConfig()
	files, err := CrawlFiles("testdata", cfg)
	if err != nil {
		t.Fatal(err)
	}

	fileSymbols := make(map[string][]Symbol)
	for _, f := range files {
		results := IndexFiles([]string{f}, 1)
		if len(results) > 0 && results[0].Err == nil {
			fileSymbols[f] = results[0].Symbols
		}
	}

	report, err := GenerateReport(fileSymbols, nil)
	if err != nil {
		t.Fatal(err)
	}

	data, err := SerializeReport(report, true)
	if err != nil {
		t.Fatal(err)
	}

	// Token estimate: chars/4. For testdata/ this should be well under 50k tokens.
	tokenEstimate := len(data) / 4
	ceiling := 50000
	if tokenEstimate > ceiling {
		t.Errorf("token estimate %d exceeds ceiling %d", tokenEstimate, ceiling)
	}
}
