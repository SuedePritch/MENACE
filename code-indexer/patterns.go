package indexer

import (
	"crypto/sha256"
	"fmt"
	"os"
	"sort"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// PatternConfig controls sub-function pattern duplicate detection.
type PatternConfig struct {
	MinStatements     int  // minimum window size (statements)
	MaxStatements     int  // maximum window size (statements)
	MinNodeCount      int  // minimum AST nodes for a window to be non-trivial
	MinOccurrences    int  // minimum occurrences to report
	CrossFunctionOnly bool // drop patterns where all occurrences are in the same function
}

// DefaultPatternConfig returns sensible defaults for pattern detection.
func DefaultPatternConfig() PatternConfig {
	return PatternConfig{
		MinStatements:     3,
		MaxStatements:     8,
		MinNodeCount:      15,
		MinOccurrences:    2,
		CrossFunctionOnly: true,
	}
}

// windowOccurrence tracks a single sliding-window match before grouping.
type windowOccurrence struct {
	hash           string
	funcName       string
	filePath       string
	startLine      int
	endLine        int
	statementCount int
	nodeCount      int
}

// FindPatternDuplicates scans all function/method bodies for repeated sub-function
// AST patterns using a sliding window approach.
func FindPatternDuplicates(fileSymbols map[string][]Symbol, cfg PatternConfig) []PatternGroup {
	// Collect all window hashes across every function.
	hashToOccurrences := make(map[string][]windowOccurrence)

	for filePath, syms := range fileSymbols {
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		lang, err := languageForFile(filePath)
		if err != nil {
			continue
		}

		parser := tree_sitter.NewParser()
		if err := parser.SetLanguage(lang); err != nil {
			parser.Close()
			continue
		}

		lines := strings.Split(string(data), "\n")

		for _, sym := range syms {
			if sym.Kind != SymbolFunction && sym.Kind != SymbolMethod {
				continue
			}

			startIdx := sym.StartLine - 1
			endIdx := sym.EndLine
			if startIdx < 0 {
				startIdx = 0
			}
			if endIdx > len(lines) {
				endIdx = len(lines)
			}
			funcSource := strings.Join(lines[startIdx:endIdx], "\n")
			funcBytes := []byte(funcSource)

			tree := parser.Parse(funcBytes, nil)
			root := tree.RootNode()

			body := findFunctionBody(root)
			if body == nil {
				tree.Close()
				continue
			}

			blocks := collectStatementBlocks(body)

			for _, block := range blocks {
				occs := extractWindowHashes(block, funcBytes, sym.Name, filePath, sym.StartLine, cfg)
				for _, occ := range occs {
					hashToOccurrences[occ.hash] = append(hashToOccurrences[occ.hash], occ)
				}
			}

			tree.Close()
		}

		parser.Close()
	}

	// Filter and build PatternGroups.
	var groups []PatternGroup
	for hash, occs := range hashToOccurrences {
		if len(occs) < cfg.MinOccurrences {
			continue
		}

		// CrossFunctionOnly: skip if all occurrences are in the same function+file
		if cfg.CrossFunctionOnly {
			allSame := true
			first := occs[0].funcName + "\x00" + occs[0].filePath
			for _, o := range occs[1:] {
				if o.funcName+"\x00"+o.filePath != first {
					allSame = false
					break
				}
			}
			if allSame {
				continue
			}
		}

		pg := PatternGroup{
			StructuralHash: hash,
			StatementCount: occs[0].statementCount,
		}
		for _, o := range occs {
			pg.Occurrences = append(pg.Occurrences, PatternOccurrence{
				FunctionName:   o.funcName,
				FilePath:       o.filePath,
				StartLine:      o.startLine,
				EndLine:        o.endLine,
				StatementCount: o.statementCount,
			})
		}
		groups = append(groups, pg)
	}

	groups = filterSubsumedPatterns(groups)

	// Sort: most occurrences first, then largest patterns, then by hash for stability.
	sort.Slice(groups, func(i, j int) bool {
		if len(groups[i].Occurrences) != len(groups[j].Occurrences) {
			return len(groups[i].Occurrences) > len(groups[j].Occurrences)
		}
		if groups[i].StatementCount != groups[j].StatementCount {
			return groups[i].StatementCount > groups[j].StatementCount
		}
		return groups[i].StructuralHash < groups[j].StructuralHash
	})

	// Populate example snippets from first occurrence.
	for i := range groups {
		if len(groups[i].Occurrences) > 0 {
			occ := groups[i].Occurrences[0]
			data, err := os.ReadFile(occ.FilePath)
			if err == nil {
				groups[i].ExampleSnippet = extractSnippet(string(data), occ.StartLine, occ.EndLine)
			}
		}
	}

	return groups
}

// collectStatementBlocks recursively finds all statement_block nodes,
// including the root block itself and any nested ones (if, for, try, etc.).
func collectStatementBlocks(node *tree_sitter.Node) []*tree_sitter.Node {
	var blocks []*tree_sitter.Node
	if node.Kind() == "statement_block" {
		blocks = append(blocks, node)
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(uint(i))
		blocks = append(blocks, collectStatementBlocks(child)...)
	}
	return blocks
}

// extractWindowHashes slides windows of size minStmt..maxStmt over the direct
// statement children of a block, serializes each window with anonymized identifiers,
// and returns all occurrences that pass the minimum node count threshold.
func extractWindowHashes(block *tree_sitter.Node, source []byte, funcName, filePath string, funcStartLine int, cfg PatternConfig) []windowOccurrence {
	// Collect the direct statement children (skip punctuation like { and }).
	var stmts []*tree_sitter.Node
	for i := 0; i < int(block.ChildCount()); i++ {
		child := block.Child(uint(i))
		kind := child.Kind()
		if kind == "{" || kind == "}" || kind == "comment" {
			continue
		}
		stmts = append(stmts, child)
	}

	var results []windowOccurrence

	for windowSize := cfg.MinStatements; windowSize <= cfg.MaxStatements; windowSize++ {
		if windowSize > len(stmts) {
			break
		}
		for start := 0; start+windowSize <= len(stmts); start++ {
			window := stmts[start : start+windowSize]

			// Count total AST nodes in this window
			totalNodes := 0
			for _, s := range window {
				totalNodes += countNodes(s)
			}
			if totalNodes < cfg.MinNodeCount {
				continue
			}

			// Serialize with fresh varMap for anonymization
			varMap := make(map[string]string)
			varCounter := 0
			var parts []string
			for _, s := range window {
				parts = append(parts, serializeNode(s, source, varMap, &varCounter))
			}
			serialized := strings.Join(parts, "|")

			h := sha256.Sum256([]byte(serialized))
			hash := fmt.Sprintf("%x", h[:8])

			// Line numbers: offset from the function's start
			windowStartLine := funcStartLine + int(window[0].StartPosition().Row)
			windowEndLine := funcStartLine + int(window[len(window)-1].EndPosition().Row)

			results = append(results, windowOccurrence{
				hash:           hash,
				funcName:       funcName,
				filePath:       filePath,
				startLine:      windowStartLine,
				endLine:        windowEndLine,
				statementCount: windowSize,
				nodeCount:      totalNodes,
			})
		}
	}

	return results
}

// countNodes recursively counts the total number of AST nodes in a subtree.
func countNodes(node *tree_sitter.Node) int {
	count := 1
	for i := 0; i < int(node.ChildCount()); i++ {
		count += countNodes(node.Child(uint(i)))
	}
	return count
}

// filterSubsumedPatterns removes smaller patterns that are fully contained within
// larger matched patterns at the same locations.
func filterSubsumedPatterns(groups []PatternGroup) []PatternGroup {
	// Build a set of all (file, funcName, line) ranges covered by each group.
	type locKey struct {
		filePath string
		funcName string
	}
	type lineRange struct {
		start int
		end   int
	}

	// For each group, compute the set of (locKey, lineRange) pairs.
	type groupCoverage struct {
		locs map[locKey][]lineRange
	}

	coverages := make([]groupCoverage, len(groups))
	for i, g := range groups {
		coverages[i].locs = make(map[locKey][]lineRange)
		for _, occ := range g.Occurrences {
			key := locKey{occ.FilePath, occ.FunctionName}
			coverages[i].locs[key] = append(coverages[i].locs[key], lineRange{occ.StartLine, occ.EndLine})
		}
	}

	// A group is subsumed if for every occurrence, there exists a larger group
	// with an occurrence that covers the same line range.
	subsumed := make([]bool, len(groups))
	for i, gi := range groups {
		for j, gj := range groups {
			if i == j {
				continue
			}
			if gj.StatementCount <= gi.StatementCount {
				continue
			}
			// Check if gi is subsumed by gj: every occurrence of gi must be
			// covered by some occurrence of gj.
			allCovered := true
			for _, occI := range gi.Occurrences {
				keyI := locKey{occI.FilePath, occI.FunctionName}
				covered := false
				if ranges, ok := coverages[j].locs[keyI]; ok {
					for _, r := range ranges {
						if r.start <= occI.StartLine && r.end >= occI.EndLine {
							covered = true
							break
						}
					}
				}
				if !covered {
					allCovered = false
					break
				}
			}
			if allCovered {
				subsumed[i] = true
				break
			}
		}
	}

	var result []PatternGroup
	for i, g := range groups {
		if !subsumed[i] {
			result = append(result, g)
		}
	}
	return result
}
