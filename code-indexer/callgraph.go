package indexer

import (
	"fmt"
	"os"
	"path/filepath"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// CallGraphEntry represents a resolved dependency from one symbol to another.
type CallGraphEntry struct {
	Caller string
	Callee string
}

// ExtractCallsFromSource extracts function/method call identifiers from a function body.
func ExtractCallsFromSource(source []byte, filePath string) ([]CallGraphEntry, error) {
	parser := tree_sitter.NewParser()
	defer parser.Close()

	lang, err := languageForFile(filePath)
	if err != nil {
		return nil, err
	}
	if err := parser.SetLanguage(lang); err != nil {
		return nil, err
	}

	tree := parser.Parse(source, nil)
	defer tree.Close()

	root := tree.RootNode()
	symbols, _ := ParseFile(filePath, source)

	var entries []CallGraphEntry
	for _, sym := range symbols {
		if sym.Kind != SymbolFunction && sym.Kind != SymbolMethod {
			continue
		}
		// Find this symbol's node and extract calls from its body
		calls := extractCallsFromSymbolRange(root, source, sym.StartLine, sym.EndLine)
		for _, callee := range calls {
			entries = append(entries, CallGraphEntry{Caller: sym.Name, Callee: callee})
		}
	}
	return entries, nil
}

func extractCallsFromSymbolRange(root *tree_sitter.Node, source []byte, startLine, endLine int) []string {
	var calls []string
	seen := make(map[string]bool)
	collectCalls(root, source, startLine, endLine, seen, &calls)
	return calls
}

func collectCalls(node *tree_sitter.Node, source []byte, startLine, endLine int, seen map[string]bool, calls *[]string) {
	nodeStartLine := int(node.StartPosition().Row) + 1
	nodeEndLine := int(node.EndPosition().Row) + 1

	// Skip nodes entirely outside the range, but always recurse if they span the range
	if nodeEndLine < startLine || nodeStartLine > endLine {
		return
	}

	if nodeStartLine >= startLine && nodeStartLine <= endLine && node.Kind() == "call_expression" {
		fn := node.ChildByFieldName("function")
		if fn != nil {
			name := extractCallName(fn, source)
			if name != "" && !seen[name] {
				seen[name] = true
				*calls = append(*calls, name)
			}
		}
	}

	// Also capture identifier references passed as arguments (e.g., arr.map(fn))
	if nodeStartLine >= startLine && nodeStartLine <= endLine && node.Kind() == "arguments" {
		for i := 0; i < int(node.ChildCount()); i++ {
			arg := node.Child(uint(i))
			if arg.Kind() == "identifier" {
				name := arg.Utf8Text(source)
				if name != "" && !seen[name] {
					seen[name] = true
					*calls = append(*calls, name)
				}
			}
		}
	}

	for i := 0; i < int(node.ChildCount()); i++ {
		collectCalls(node.Child(uint(i)), source, startLine, endLine, seen, calls)
	}
}

func extractCallName(node *tree_sitter.Node, source []byte) string {
	switch node.Kind() {
	case "identifier":
		return node.Utf8Text(source)
	case "member_expression":
		obj := node.ChildByFieldName("object")
		prop := node.ChildByFieldName("property")
		if obj != nil && prop != nil {
			return obj.Utf8Text(source) + "." + prop.Utf8Text(source)
		}
	}
	return ""
}

// BuildCallGraph builds the full dependency graph across all files.
// It populates Dependencies and Dependents on each Symbol.
func BuildCallGraph(fileSymbols map[string][]Symbol, fileImports map[string][]ImportInfo) {
	// Build a global symbol lookup: name -> []*Symbol
	globalSyms := make(map[string][]*Symbol)
	for path := range fileSymbols {
		for i := range fileSymbols[path] {
			s := &fileSymbols[path][i]
			globalSyms[s.Name] = append(globalSyms[s.Name], s)
		}
	}

	// Track existing edges with sets for O(1) dedup instead of O(n) scans.
	depSets := make(map[*Symbol]map[string]struct{})
	dependentSets := make(map[*Symbol]map[string]struct{})

	for filePath, syms := range fileSymbols {
		source, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		entries, err := ExtractCallsFromSource(source, filePath)
		if err != nil {
			continue
		}

		// Build local import mapping: localName -> resolved symbol name
		importMap := make(map[string]string)
		for _, imp := range fileImports[filePath] {
			if targetSyms, ok := fileSymbols[imp.FromPath]; ok {
				for _, ts := range targetSyms {
					if imp.SourceName == "default" && ts.ExportStatus == ExportDefault {
						importMap[imp.LocalName] = ts.Name
					} else if ts.Name == imp.SourceName {
						importMap[imp.LocalName] = ts.Name
					}
				}
			}
		}

		symMap := make(map[string]*Symbol)
		for i := range syms {
			symMap[syms[i].Name] = &fileSymbols[filePath][i]
		}

		for _, entry := range entries {
			caller := symMap[entry.Caller]
			if caller == nil {
				continue
			}

			calleeName := entry.Callee
			if resolved, ok := importMap[calleeName]; ok {
				calleeName = resolved
			}

			if calleeName == caller.Name {
				continue
			}

			// Add dependency (O(1) check)
			if depSets[caller] == nil {
				depSets[caller] = make(map[string]struct{})
			}
			if _, exists := depSets[caller][calleeName]; !exists {
				depSets[caller][calleeName] = struct{}{}
				caller.Dependencies = append(caller.Dependencies, calleeName)
			}

			// Back-fill dependents (O(1) check)
			if targets, ok := globalSyms[calleeName]; ok {
				for _, target := range targets {
					if dependentSets[target] == nil {
						dependentSets[target] = make(map[string]struct{})
					}
					if _, exists := dependentSets[target][caller.Name]; !exists {
						dependentSets[target][caller.Name] = struct{}{}
						target.Dependents = append(target.Dependents, caller.Name)
					}
				}
			}
		}
	}
}

// ImpactChain represents a chain of affected symbols.
type ImpactChain struct {
	Symbol string
	Depth  int
}

// AnalyzeImpact walks the dependents graph recursively to find all affected symbols.
func AnalyzeImpact(symbolName string, fileSymbols map[string][]Symbol, maxDepth int) []ImpactChain {
	// Build global dependent lookup
	dependents := make(map[string][]string)
	for _, syms := range fileSymbols {
		for _, s := range syms {
			for _, dep := range s.Dependencies {
				dependents[dep] = append(dependents[dep], s.Name)
			}
		}
	}

	var chain []ImpactChain
	visited := make(map[string]bool)
	walkImpact(symbolName, dependents, 1, maxDepth, visited, &chain)
	return chain
}

func walkImpact(name string, dependents map[string][]string, depth, maxDepth int, visited map[string]bool, chain *[]ImpactChain) {
	if depth > maxDepth || visited[name] {
		return
	}
	visited[name] = true

	for _, dep := range dependents[name] {
		if !visited[dep] {
			*chain = append(*chain, ImpactChain{Symbol: dep, Depth: depth})
			walkImpact(dep, dependents, depth+1, maxDepth, visited, chain)
		}
	}
}

// FindPotentiallyUnused returns symbols that are unexported and have no dependents.
func FindPotentiallyUnused(symbols []Symbol) []string {
	var unused []string
	for _, s := range symbols {
		if s.ExportStatus == ExportUnexported && len(s.Dependents) == 0 {
			if s.Kind == SymbolFunction || s.Kind == SymbolMethod {
				unused = append(unused, s.Name)
			}
		}
	}
	return unused
}

// FindDuplicateGroups groups symbols by structural hash.
func FindDuplicateGroups(allSymbols []Symbol) []DuplicationGroup {
	groups := make(map[string][]string)
	for _, s := range allSymbols {
		if s.StructuralHash != "" {
			groups[s.StructuralHash] = append(groups[s.StructuralHash], fmt.Sprintf("%s:%s", filepath.Base(s.FilePath), s.Name))
		}
	}

	var result []DuplicationGroup
	for hash, members := range groups {
		if len(members) >= 2 {
			result = append(result, DuplicationGroup{
				StructuralHash: hash,
				Symbols:        members,
			})
		}
	}
	return result
}
