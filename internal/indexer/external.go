package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ExternalIndexer wraps an external binary that speaks the MENACE indexer protocol.
//
// Protocol:
//   <binary> extensions          → JSON string array: [".go", ".rs"]
//   <binary> symbols <file>      → JSON array of Symbol
//   <binary> index <dir>         → JSON Report object
//   <binary> find <name> [file]  → JSON array of Symbol
//
// All commands must complete within 30 seconds and exit 0 on success.
// Non-zero exit or invalid JSON is treated as an error.
type ExternalIndexer struct {
	Binary string // path to the indexer binary
	exts   []string
}

// NewExternalIndexer creates an external indexer and queries it for extensions.
// Returns an error if the binary is missing or doesn't respond to "extensions".
func NewExternalIndexer(binary string) (*ExternalIndexer, error) {
	e := &ExternalIndexer{Binary: binary}

	// Query extensions
	out, err := e.run("extensions")
	if err != nil {
		return nil, fmt.Errorf("indexer %q failed extensions command: %w", binary, err)
	}

	var exts []string
	if err := json.Unmarshal(out, &exts); err != nil {
		return nil, fmt.Errorf("indexer %q returned invalid extensions JSON: %w", binary, err)
	}
	if len(exts) == 0 {
		return nil, fmt.Errorf("indexer %q returned no extensions", binary)
	}

	e.exts = exts
	return e, nil
}

func (e *ExternalIndexer) Extensions() []string {
	return e.exts
}

func (e *ExternalIndexer) IndexDir(dir string, workers int) (*Report, error) {
	out, err := e.run("index", dir)
	if err != nil {
		return nil, err
	}

	var report jsonReport
	if err := json.Unmarshal(out, &report); err != nil {
		return nil, fmt.Errorf("invalid index JSON from %q: %w", e.Binary, err)
	}

	return &Report{
		Symbols:       convertJSONSymbols(report.Symbols),
		TokenEstimate: report.TokenEstimate,
	}, nil
}

func (e *ExternalIndexer) SymbolsInFile(filePath string) ([]Symbol, error) {
	out, err := e.run("symbols", filePath)
	if err != nil {
		return nil, err
	}

	var syms []jsonSymbol
	if err := json.Unmarshal(out, &syms); err != nil {
		return nil, fmt.Errorf("invalid symbols JSON from %q: %w", e.Binary, err)
	}

	return convertJSONSymbols(syms), nil
}

func (e *ExternalIndexer) FindSymbol(name string, filePath string) ([]Symbol, error) {
	args := []string{"find", name}
	if filePath != "" {
		args = append(args, filePath)
	}
	out, err := e.run(args...)
	if err != nil {
		return nil, err
	}

	var syms []jsonSymbol
	if err := json.Unmarshal(out, &syms); err != nil {
		return nil, fmt.Errorf("invalid find JSON from %q: %w", e.Binary, err)
	}

	return convertJSONSymbols(syms), nil
}

func (e *ExternalIndexer) run(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, e.Binary, args...)
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%q %s timed out after 30s", e.Binary, strings.Join(args, " "))
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%q %s exited %d: %s", e.Binary, strings.Join(args, " "), exitErr.ExitCode(), string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("%q %s: %w", e.Binary, strings.Join(args, " "), err)
	}
	return out, nil
}

// JSON wire types — match the Symbol/Report structs but with json tags
type jsonSymbol struct {
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	FilePath     string   `json:"filePath"`
	StartLine    int      `json:"startLine"`
	EndLine      int      `json:"endLine"`
	Source       string   `json:"source"`
	ExportStatus string   `json:"exportStatus,omitempty"`
	Dependencies []string `json:"dependencies,omitempty"`
	Dependents   []string `json:"dependents,omitempty"`
}

type jsonReport struct {
	Symbols       []jsonSymbol `json:"symbols"`
	TokenEstimate int          `json:"tokenEstimate"`
}

func convertJSONSymbols(jSyms []jsonSymbol) []Symbol {
	syms := make([]Symbol, len(jSyms))
	for i, j := range jSyms {
		syms[i] = Symbol(j)
	}
	return syms
}
