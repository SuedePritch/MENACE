package indexer

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// DefaultExclusions are directory names skipped by default.
var DefaultExclusions = []string{
	"node_modules", "dist", "build", ".next", "coverage",
	".git", ".svn", "vendor", "__pycache__",
}

// SupportedExtensions are the file extensions the crawler collects.
var SupportedExtensions = map[string]bool{
	".ts": true, ".tsx": true, ".js": true, ".jsx": true,
}

// CrawlerConfig configures file discovery.
type CrawlerConfig struct {
	Extensions []string
	Exclusions []string
	Workers    int
}

// DefaultCrawlerConfig returns a config with sensible defaults.
func DefaultCrawlerConfig() CrawlerConfig {
	return CrawlerConfig{
		Extensions: []string{".ts", ".tsx", ".js", ".jsx"},
		Exclusions: DefaultExclusions,
		Workers:    4,
	}
}

// CrawlFiles walks rootDir and returns all matching file paths.
func CrawlFiles(rootDir string, cfg CrawlerConfig) ([]string, error) {
	exclusionSet := make(map[string]bool, len(cfg.Exclusions))
	for _, e := range cfg.Exclusions {
		exclusionSet[e] = true
	}

	extSet := make(map[string]bool, len(cfg.Extensions))
	for _, e := range cfg.Extensions {
		extSet[e] = true
	}

	seen := make(map[string]bool) // cycle detection for symlinks

	var files []string
	err := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Log and skip on permission errors etc.
			return nil
		}

		name := d.Name()

		// Skip excluded directories
		if d.IsDir() {
			if exclusionSet[name] {
				return filepath.SkipDir
			}
			// Symlink cycle detection
			real, err := filepath.EvalSymlinks(path)
			if err != nil {
				return nil // skip broken symlinks
			}
			if seen[real] {
				return filepath.SkipDir
			}
			seen[real] = true
			return nil
		}

		// Skip non-regular files
		if !d.Type().IsRegular() && d.Type()&fs.ModeSymlink == 0 {
			return nil
		}

		// Check if it's a symlink to a file (resolve it)
		if d.Type()&fs.ModeSymlink != 0 {
			info, err := os.Stat(path)
			if err != nil {
				return nil
			}
			if info.IsDir() {
				return nil
			}
		}

		ext := strings.ToLower(filepath.Ext(name))
		if extSet[ext] {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// IndexResult holds the parse result for a single file.
type IndexResult struct {
	FilePath string
	Symbols  []Symbol
	Err      error
}

// IndexFiles parses all given files using a worker pool.
func IndexFiles(files []string, workers int) []IndexResult {
	if workers < 1 {
		workers = 1
	}

	results := make([]IndexResult, len(files))
	var wg sync.WaitGroup
	ch := make(chan int, len(files))

	for i := range files {
		ch <- i
	}
	close(ch)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range ch {
				path := files[idx]
				data, err := os.ReadFile(path)
				if err != nil {
					results[idx] = IndexResult{FilePath: path, Err: err}
					continue
				}
				syms, err := ParseFile(path, data)
				results[idx] = IndexResult{FilePath: path, Symbols: syms, Err: err}
			}
		}()
	}

	wg.Wait()
	return results
}
