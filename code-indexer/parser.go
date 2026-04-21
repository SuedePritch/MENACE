package indexer

import (
	"fmt"
	"path/filepath"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// ParseFile parses a TypeScript/TSX file and returns all discovered symbols.
func ParseFile(filePath string, source []byte) ([]Symbol, error) {
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
	var symbols []Symbol
	extractSymbols(root, source, filePath, &symbols)
	return symbols, nil
}

// ParseSource parses inline TypeScript source (for tests). Uses .ts grammar.
func ParseSource(source string) ([]Symbol, error) {
	return ParseFile("test.ts", []byte(source))
}

func languageForFile(filePath string) (*tree_sitter.Language, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".tsx", ".jsx":
		return tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTSX()), nil
	case ".ts", ".js":
		return tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTypescript()), nil
	default:
		return nil, fmt.Errorf("unsupported file extension: %s", ext)
	}
}

func extractSymbols(node *tree_sitter.Node, source []byte, filePath string, symbols *[]Symbol) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(uint(i))
		switch child.Kind() {
		case "function_declaration":
			if sym, ok := extractFunction(child, source, filePath); ok {
				*symbols = append(*symbols, sym)
			}
		case "export_statement":
			extractExportStatement(child, source, filePath, symbols)
		case "lexical_declaration":
			extractArrowsFromDeclaration(child, source, filePath, ExportUnexported, symbols)
		case "class_declaration":
			extractClass(child, source, filePath, ExportUnexported, symbols)
		case "interface_declaration":
			if sym, ok := extractInterfaceOrType(child, source, filePath, SymbolInterface, ExportUnexported); ok {
				*symbols = append(*symbols, sym)
			}
		case "type_alias_declaration":
			if sym, ok := extractInterfaceOrType(child, source, filePath, SymbolType, ExportUnexported); ok {
				*symbols = append(*symbols, sym)
			}
		case "enum_declaration":
			if sym, ok := extractInterfaceOrType(child, source, filePath, SymbolEnum, ExportUnexported); ok {
				*symbols = append(*symbols, sym)
			}
		}
	}
}

func extractExportStatement(node *tree_sitter.Node, source []byte, filePath string, symbols *[]Symbol) {
	isDefault := false
	for i := 0; i < int(node.ChildCount()); i++ {
		c := node.Child(uint(i))
		if c.Kind() == "default" {
			isDefault = true
		}
	}

	exportStatus := ExportNamed
	if isDefault {
		exportStatus = ExportDefault
	}

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(uint(i))
		switch child.Kind() {
		case "function_declaration":
			if sym, ok := extractFunction(child, source, filePath); ok {
				sym.ExportStatus = exportStatus
				*symbols = append(*symbols, sym)
			}
		case "lexical_declaration":
			extractArrowsFromDeclaration(child, source, filePath, exportStatus, symbols)
		case "class_declaration":
			extractClass(child, source, filePath, exportStatus, symbols)
		case "interface_declaration":
			if sym, ok := extractInterfaceOrType(child, source, filePath, SymbolInterface, exportStatus); ok {
				*symbols = append(*symbols, sym)
			}
		case "type_alias_declaration":
			if sym, ok := extractInterfaceOrType(child, source, filePath, SymbolType, exportStatus); ok {
				*symbols = append(*symbols, sym)
			}
		case "enum_declaration":
			if sym, ok := extractInterfaceOrType(child, source, filePath, SymbolEnum, exportStatus); ok {
				*symbols = append(*symbols, sym)
			}
		}
	}
}

func extractFunction(node *tree_sitter.Node, source []byte, filePath string) (Symbol, bool) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return Symbol{}, false
	}
	name := nameNode.Utf8Text(source)
	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	return Symbol{
		Name:         name,
		Kind:         SymbolFunction,
		FilePath:     filePath,
		StartLine:    startLine,
		EndLine:      endLine,
		ExportStatus: ExportUnexported,
	}, true
}

func extractArrowsFromDeclaration(node *tree_sitter.Node, source []byte, filePath string, exportStatus ExportStatus, symbols *[]Symbol) {
	for i := 0; i < int(node.ChildCount()); i++ {
		declarator := node.Child(uint(i))
		if declarator.Kind() != "variable_declarator" {
			continue
		}
		nameNode := declarator.ChildByFieldName("name")
		valueNode := declarator.ChildByFieldName("value")
		if nameNode == nil || valueNode == nil {
			continue
		}
		// The value could be the arrow_function directly or wrapped in a type annotation
		if isArrowFunction(valueNode) {
			name := nameNode.Utf8Text(source)
			// Use the full declarator's parent (lexical_declaration or export_statement) for line range
			parent := node
			startLine := int(parent.StartPosition().Row) + 1
			endLine := int(parent.EndPosition().Row) + 1
			*symbols = append(*symbols, Symbol{
				Name:         name,
				Kind:         SymbolFunction,
				FilePath:     filePath,
				StartLine:    startLine,
				EndLine:      endLine,
				ExportStatus: exportStatus,
			})
		}
	}
}

func isArrowFunction(node *tree_sitter.Node) bool {
	if node.Kind() == "arrow_function" {
		return true
	}
	// Check for `async () => {}` which wraps in an expression
	if node.Kind() == "as_expression" || node.Kind() == "satisfies_expression" {
		for i := 0; i < int(node.ChildCount()); i++ {
			if isArrowFunction(node.Child(uint(i))) {
				return true
			}
		}
	}
	return false
}

func extractClass(node *tree_sitter.Node, source []byte, filePath string, exportStatus ExportStatus, symbols *[]Symbol) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	className := nameNode.Utf8Text(source)

	// Add the class itself
	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1
	*symbols = append(*symbols, Symbol{
		Name:         className,
		Kind:         SymbolClass,
		FilePath:     filePath,
		StartLine:    startLine,
		EndLine:      endLine,
		ExportStatus: exportStatus,
	})

	// Extract methods from class body
	body := node.ChildByFieldName("body")
	if body == nil {
		return
	}
	for i := 0; i < int(body.ChildCount()); i++ {
		member := body.Child(uint(i))
		if member.Kind() == "method_definition" {
			extractMethod(member, source, filePath, className, symbols)
		}
	}
}

func extractMethod(node *tree_sitter.Node, source []byte, filePath string, className string, symbols *[]Symbol) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	methodName := nameNode.Utf8Text(source)
	fullName := className + "." + methodName

	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	*symbols = append(*symbols, Symbol{
		Name:         fullName,
		Kind:         SymbolMethod,
		FilePath:     filePath,
		StartLine:    startLine,
		EndLine:      endLine,
		ExportStatus: ExportUnexported,
	})
}

func extractInterfaceOrType(node *tree_sitter.Node, source []byte, filePath string, kind SymbolKind, exportStatus ExportStatus) (Symbol, bool) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return Symbol{}, false
	}
	name := nameNode.Utf8Text(source)
	startLine := int(node.StartPosition().Row) + 1
	endLine := int(node.EndPosition().Row) + 1

	return Symbol{
		Name:         name,
		Kind:         kind,
		FilePath:     filePath,
		StartLine:    startLine,
		EndLine:      endLine,
		ExportStatus: exportStatus,
	}, true
}
