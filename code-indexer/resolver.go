package indexer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// ImportInfo represents a single import binding.
type ImportInfo struct {
	LocalName  string // name used in this file
	SourceName string // name exported from the source (or "default")
	FromPath   string // resolved absolute file path
}

// ResolveImports parses import statements from a file and resolves them to file paths.
func ResolveImports(filePath string, source []byte) ([]ImportInfo, error) {
	parser := tree_sitter.NewParser()
	defer parser.Close()

	lang, err := languageForFile(filePath)
	if err != nil {
		return nil, err
	}
	if err := parser.SetLanguage(lang); err != nil {
		return nil, fmt.Errorf("set language: %w", err)
	}

	tree := parser.Parse(source, nil)
	defer tree.Close()

	root := tree.RootNode()
	dir := filepath.Dir(filePath)

	var imports []ImportInfo
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(uint(i))
		if child.Kind() != "import_statement" {
			continue
		}
		imports = append(imports, parseImportStatement(child, source, dir)...)
	}
	return imports, nil
}

func parseImportStatement(node *tree_sitter.Node, source []byte, dir string) []ImportInfo {
	var fromPath string
	var imports []ImportInfo

	// Find the source string
	srcNode := node.ChildByFieldName("source")
	if srcNode == nil {
		return nil
	}
	raw := srcNode.Utf8Text(source)
	raw = strings.Trim(raw, "\"'`")

	// Only resolve relative imports
	if !strings.HasPrefix(raw, ".") {
		return nil
	}

	fromPath = resolveModulePath(dir, raw)
	if fromPath == "" {
		return nil
	}

	// Walk children for import clause
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(uint(i))
		switch child.Kind() {
		case "import_clause":
			imports = append(imports, parseImportClause(child, source, fromPath)...)
		}
	}

	return imports
}

func parseImportClause(node *tree_sitter.Node, source []byte, fromPath string) []ImportInfo {
	var imports []ImportInfo
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(uint(i))
		switch child.Kind() {
		case "identifier":
			// Default import: import Foo from './bar'
			imports = append(imports, ImportInfo{
				LocalName:  child.Utf8Text(source),
				SourceName: "default",
				FromPath:   fromPath,
			})
		case "named_imports":
			for j := 0; j < int(child.ChildCount()); j++ {
				spec := child.Child(uint(j))
				if spec.Kind() == "import_specifier" {
					local, source_ := parseImportSpecifier(spec, source)
					imports = append(imports, ImportInfo{
						LocalName:  local,
						SourceName: source_,
						FromPath:   fromPath,
					})
				}
			}
		}
	}
	return imports
}

func parseImportSpecifier(node *tree_sitter.Node, source []byte) (local, sourceName string) {
	nameNode := node.ChildByFieldName("name")
	aliasNode := node.ChildByFieldName("alias")

	if nameNode == nil {
		return "", ""
	}
	sourceName = nameNode.Utf8Text(source)
	local = sourceName
	if aliasNode != nil {
		local = aliasNode.Utf8Text(source)
	}
	return local, sourceName
}

func resolveModulePath(dir, modulePath string) string {
	candidate := filepath.Join(dir, modulePath)

	// Try exact path first
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return candidate
	}

	// Try with extensions
	for _, ext := range []string{".ts", ".tsx", ".js", ".jsx"} {
		p := candidate + ext
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// Try index files
	for _, ext := range []string{".ts", ".tsx", ".js", ".jsx"} {
		p := filepath.Join(candidate, "index"+ext)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	return ""
}

// ResolveExportStatements parses re-exports like `export { foo } from './bar'`.
func ResolveExportStatements(filePath string, source []byte) ([]ImportInfo, error) {
	parser := tree_sitter.NewParser()
	defer parser.Close()

	lang := tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTypescript())
	if err := parser.SetLanguage(lang); err != nil {
		return nil, err
	}

	tree := parser.Parse(source, nil)
	defer tree.Close()

	root := tree.RootNode()
	dir := filepath.Dir(filePath)

	var reexports []ImportInfo
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(uint(i))
		if child.Kind() != "export_statement" {
			continue
		}
		// Check for a source field (re-export)
		srcNode := child.ChildByFieldName("source")
		if srcNode == nil {
			continue
		}
		raw := strings.Trim(srcNode.Utf8Text(source), "\"'`")
		if !strings.HasPrefix(raw, ".") {
			continue
		}
		fromPath := resolveModulePath(dir, raw)
		if fromPath == "" {
			continue
		}

		// Find named exports in the export statement
		for j := 0; j < int(child.ChildCount()); j++ {
			clause := child.Child(uint(j))
			if clause.Kind() == "export_clause" {
				for k := 0; k < int(clause.ChildCount()); k++ {
					spec := clause.Child(uint(k))
					if spec.Kind() == "export_specifier" {
						nameNode := spec.ChildByFieldName("name")
						aliasNode := spec.ChildByFieldName("alias")
						if nameNode != nil {
							srcName := nameNode.Utf8Text(source)
							localName := srcName
							if aliasNode != nil {
								localName = aliasNode.Utf8Text(source)
							}
							reexports = append(reexports, ImportInfo{
								LocalName:  localName,
								SourceName: srcName,
								FromPath:   fromPath,
							})
						}
					}
				}
			}
		}
	}
	return reexports, nil
}

// BuildSymbolTable maps exported symbol names to Symbol structs for a file.
func BuildSymbolTable(symbols []Symbol) map[string]*Symbol {
	table := make(map[string]*Symbol)
	for i := range symbols {
		s := &symbols[i]
		if s.ExportStatus == ExportNamed || s.ExportStatus == ExportDefault {
			table[s.Name] = s
		}
		if s.ExportStatus == ExportDefault {
			table["default"] = s
		}
	}
	return table
}

// MatchImportToSymbol resolves an import to its target symbol across file symbol tables.
func MatchImportToSymbol(imp ImportInfo, fileSymbols map[string][]Symbol) *Symbol {
	syms, ok := fileSymbols[imp.FromPath]
	if !ok {
		return nil
	}
	table := BuildSymbolTable(syms)

	if imp.SourceName == "default" {
		return table["default"]
	}
	return table[imp.SourceName]
}
