package indexer

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

var (
	commentRe    = regexp.MustCompile(`(?s)//[^\n]*|/\*.*?\*/`)
	whitespaceRe = regexp.MustCompile(`\s+`)
)

// Normalize strips comments and collapses whitespace from source code.
func Normalize(source string) string {
	s := commentRe.ReplaceAllString(source, "")
	s = whitespaceRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// BodyHash returns a SHA-256 hash of the normalized source.
func ComputeBodyHash(source string) string {
	normalized := Normalize(source)
	h := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("%x", h[:8])
}

// StructuralHash computes a hash of the AST structure with anonymized identifiers.
// This approach uses the serialized AST with positional tokens (VAR_0, VAR_1, ...)
// replacing all user-defined identifiers. This captures structural differences
// (different statement counts, different control flow) while ignoring naming differences.
// Rationale: Normalized source text hashing would miss structural equivalence when
// formatting differs. AST-based hashing is more robust for detecting true duplicates.
func ComputeStructuralHash(source string) string {
	parser := tree_sitter.NewParser()
	defer parser.Close()

	lang := tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTypescript())
	parser.SetLanguage(lang)

	tree := parser.Parse([]byte(source), nil)
	defer tree.Close()

	root := tree.RootNode()

	// Find the function body
	body := findFunctionBody(root)
	if body == nil {
		// Fallback: hash the whole thing
		body = root
	}

	// Count parameters for structural comparison
	params := countParameters(root)

	varMap := make(map[string]string)
	varCounter := 0
	serialized := serializeNode(body, []byte(source), varMap, &varCounter)
	serialized = fmt.Sprintf("params:%d|%s", params, serialized)

	h := sha256.Sum256([]byte(serialized))
	return fmt.Sprintf("%x", h[:8])
}

func findFunctionBody(node *tree_sitter.Node) *tree_sitter.Node {
	if node.Kind() == "statement_block" {
		return node
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(uint(i))
		if child.Kind() == "function_declaration" || child.Kind() == "arrow_function" {
			body := child.ChildByFieldName("body")
			if body != nil {
				return body
			}
		}
		if result := findFunctionBody(child); result != nil {
			return result
		}
	}
	return nil
}

func countParameters(node *tree_sitter.Node) int {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(uint(i))
		if child.Kind() == "function_declaration" || child.Kind() == "arrow_function" {
			params := child.ChildByFieldName("parameters")
			if params == nil {
				return 0
			}
			count := 0
			for j := 0; j < int(params.ChildCount()); j++ {
				p := params.Child(uint(j))
				if p.Kind() == "required_parameter" || p.Kind() == "optional_parameter" || p.Kind() == "identifier" {
					count++
				}
			}
			return count
		}
		if result := countParameters(child); result > 0 {
			return result
		}
	}
	return 0
}

func serializeNode(node *tree_sitter.Node, source []byte, varMap map[string]string, counter *int) string {
	if node.ChildCount() == 0 {
		// Leaf node
		text := node.Utf8Text(source)
		kind := node.Kind()

		// Anonymize identifiers
		if kind == "identifier" || kind == "property_identifier" {
			if mapped, ok := varMap[text]; ok {
				return fmt.Sprintf("(%s %s)", kind, mapped)
			}
			token := fmt.Sprintf("VAR_%d", *counter)
			varMap[text] = token
			*counter++
			return fmt.Sprintf("(%s %s)", kind, token)
		}

		// Keep literals and operators as-is for structural comparison
		return fmt.Sprintf("(%s %s)", kind, text)
	}

	var children []string
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(uint(i))
		children = append(children, serializeNode(child, source, varMap, counter))
	}
	return fmt.Sprintf("(%s %s)", node.Kind(), strings.Join(children, " "))
}
