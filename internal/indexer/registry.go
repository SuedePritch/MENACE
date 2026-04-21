package indexer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// IndexerStatus tracks a registered indexer and its health.
type IndexerStatus struct {
	Indexer    Indexer
	Name       string   // human-readable name
	Extensions []string
	Healthy    bool
	Error      string // empty if healthy
}

var (
	mu       sync.RWMutex
	registry = map[string]Indexer{}       // extension → indexer
	statuses = map[Indexer]*IndexerStatus{} // indexer → status
)

// Register adds an indexer for its declared extensions.
// Validates the indexer by checking Extensions() is non-empty and
// running a smoke test with SymbolsInFile on a temp file.
func Register(idx Indexer) error {
	exts := idx.Extensions()
	if len(exts) == 0 {
		return fmt.Errorf("indexer declares no extensions")
	}

	// Validate extensions are properly formatted
	for _, ext := range exts {
		if len(ext) < 2 || ext[0] != '.' {
			return fmt.Errorf("invalid extension %q: must start with '.'", ext)
		}
	}

	// Determine name
	name := strings.Join(exts, ", ")
	if ext, ok := idx.(*ExternalIndexer); ok {
		name = filepath.Base(ext.Binary) + " " + name
	} else {
		name = "built-in " + name
	}

	// Smoke test: create a temp file and try to index it
	status := &IndexerStatus{
		Indexer:    idx,
		Name:       name,
		Extensions: exts,
		Healthy:    true,
	}

	tmpFile, err := os.CreateTemp("", "menace-indexer-test-*"+exts[0])
	if err == nil {
		testContent := "// test\nfunction test() { return 1; }\n"
		_, writeErr := tmpFile.WriteString(testContent)
		tmpFile.Close()
		if writeErr != nil {
			status.Healthy = false
			status.Error = fmt.Sprintf("write temp file: %v", writeErr)
		} else {
			_, idxErr := idx.SymbolsInFile(tmpFile.Name())
			if idxErr != nil {
				status.Healthy = false
				status.Error = idxErr.Error()
			}
		}
		os.Remove(tmpFile.Name())
	}

	mu.Lock()
	defer mu.Unlock()
	for _, ext := range exts {
		registry[ext] = idx
	}
	statuses[idx] = status
	return nil
}

// ForFile returns the indexer for a file's extension, or nil if none registered.
func ForFile(filePath string) Indexer {
	mu.RLock()
	defer mu.RUnlock()
	ext := filepath.Ext(filePath)
	idx := registry[ext]
	if idx == nil {
		return nil
	}
	// Only return healthy indexers
	if s, ok := statuses[idx]; ok && !s.Healthy {
		return nil
	}
	return idx
}

// All returns all unique registered indexers.
func All() []Indexer {
	mu.RLock()
	defer mu.RUnlock()
	seen := map[Indexer]bool{}
	var result []Indexer
	for _, idx := range registry {
		if !seen[idx] {
			seen[idx] = true
			result = append(result, idx)
		}
	}
	return result
}

// Reset clears all registered indexers. Used in tests.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	registry = map[string]Indexer{}
	statuses = map[Indexer]*IndexerStatus{}
}

// Statuses returns the health status of all registered indexers.
func Statuses() []IndexerStatus {
	mu.RLock()
	defer mu.RUnlock()
	var result []IndexerStatus
	for _, s := range statuses {
		result = append(result, *s)
	}
	return result
}
