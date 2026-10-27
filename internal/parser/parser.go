package parser

import (
	"bytes"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unsafe"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_bash "github.com/tree-sitter/tree-sitter-bash/bindings/go"
	tree_sitter_csharp "github.com/tree-sitter/tree-sitter-c-sharp/bindings/go"
	tree_sitter_c "github.com/tree-sitter/tree-sitter-c/bindings/go"
	tree_sitter_cpp "github.com/tree-sitter/tree-sitter-cpp/bindings/go"
	tree_sitter_css "github.com/tree-sitter/tree-sitter-css/bindings/go"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tree_sitter_html "github.com/tree-sitter/tree-sitter-html/bindings/go"
	tree_sitter_java "github.com/tree-sitter/tree-sitter-java/bindings/go"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tree_sitter_json "github.com/tree-sitter/tree-sitter-json/bindings/go"
	tree_sitter_php "github.com/tree-sitter/tree-sitter-php/bindings/go"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tree_sitter_ruby "github.com/tree-sitter/tree-sitter-ruby/bindings/go"
	tree_sitter_rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// Function represents a function/method found in source code
type Function struct {
	Name      string
	StartLine int
	EndLine   int
	Content   string
	Enclosing *ClassRef // nil when not in a class or the language has no enclosing types configured
}

// ClassRef identifies the class-like declaration (class, enum, trait) enclosing a function
type ClassRef struct {
	Name      string
	Kind      string
	StartLine int
	EndLine   int
	StartByte uint
	EndByte   uint
}

// LanguageConfig holds configuration for a specific language
type LanguageConfig struct {
	Name          string
	Language      unsafe.Pointer
	FunctionQuery string // Tree-sitter query to find functions
	// EnclosingTypes lists the node types treated as enclosing classes. Empty for
	// languages without class header support (Go, Rust, C, Bash, and for now the others).
	EnclosingTypes []string
	// EnclosingBarriers lists node types that end the search for an enclosing type:
	// a function nested in one of them (anonymous class, method body) does not belong
	// to the class declared further up.
	EnclosingBarriers []string
}

// languageConfigs maps file extensions to language configurations
var languageConfigs map[string]*LanguageConfig

func init() {
	languageConfigs = map[string]*LanguageConfig{
		// Go
		".go": {
			Name:     "go",
			Language: tree_sitter_go.Language(),
			FunctionQuery: `
				(function_declaration name: (identifier) @name) @function
				(method_declaration name: (field_identifier) @name) @function
			`,
		},
		// JavaScript
		".js": {
			Name:     "javascript",
			Language: tree_sitter_javascript.Language(),
			FunctionQuery: `
				(function_declaration name: (identifier) @name) @function
				(method_definition name: (property_identifier) @name) @function
				(arrow_function) @function
				(function_expression) @function
			`,
		},
		".jsx": {
			Name:     "javascript",
			Language: tree_sitter_javascript.Language(),
			FunctionQuery: `
				(function_declaration name: (identifier) @name) @function
				(method_definition name: (property_identifier) @name) @function
				(arrow_function) @function
				(function_expression) @function
			`,
		},
		// TypeScript
		".ts": {
			Name:     "typescript",
			Language: tree_sitter_typescript.LanguageTypescript(),
			FunctionQuery: `
				(function_declaration name: (identifier) @name) @function
				(method_definition name: (property_identifier) @name) @function
				(arrow_function) @function
				(function_expression) @function
			`,
		},
		".tsx": {
			Name:     "typescript",
			Language: tree_sitter_typescript.LanguageTSX(),
			FunctionQuery: `
				(function_declaration name: (identifier) @name) @function
				(method_definition name: (property_identifier) @name) @function
				(arrow_function) @function
				(function_expression) @function
			`,
		},
		// Python
		".py": {
			Name:     "python",
			Language: tree_sitter_python.Language(),
			FunctionQuery: `
				(function_definition name: (identifier) @name) @function
			`,
		},
		// Rust
		".rs": {
			Name:     "rust",
			Language: tree_sitter_rust.Language(),
			FunctionQuery: `
				(function_item name: (identifier) @name) @function
			`,
		},
		// Java
		".java": {
			Name:     "java",
			Language: tree_sitter_java.Language(),
			FunctionQuery: `
				(method_declaration name: (identifier) @name) @function
				(constructor_declaration name: (identifier) @name) @function
			`,
		},
		// C
		".c": {
			Name:     "c",
			Language: tree_sitter_c.Language(),
			FunctionQuery: `
				(function_definition declarator: (function_declarator declarator: (identifier) @name)) @function
			`,
		},
		".h": {
			Name:     "c",
			Language: tree_sitter_c.Language(),
			FunctionQuery: `
				(function_definition declarator: (function_declarator declarator: (identifier) @name)) @function
			`,
		},
		// C++
		".cpp": {
			Name:     "cpp",
			Language: tree_sitter_cpp.Language(),
			FunctionQuery: `
				(function_definition declarator: (function_declarator declarator: (identifier) @name)) @function
				(function_definition declarator: (function_declarator declarator: (qualified_identifier) @name)) @function
			`,
		},
		".cc": {
			Name:     "cpp",
			Language: tree_sitter_cpp.Language(),
			FunctionQuery: `
				(function_definition declarator: (function_declarator declarator: (identifier) @name)) @function
				(function_definition declarator: (function_declarator declarator: (qualified_identifier) @name)) @function
			`,
		},
		".hpp": {
			Name:     "cpp",
			Language: tree_sitter_cpp.Language(),
			FunctionQuery: `
				(function_definition declarator: (function_declarator declarator: (identifier) @name)) @function
				(function_definition declarator: (function_declarator declarator: (qualified_identifier) @name)) @function
			`,
		},
		// C#
		".cs": {
			Name:     "csharp",
			Language: tree_sitter_csharp.Language(),
			FunctionQuery: `
				(method_declaration name: (identifier) @name) @function
				(constructor_declaration name: (identifier) @name) @function
			`,
		},
		// PHP
		".php": {
			Name:     "php",
			Language: tree_sitter_php.LanguagePHP(),
			FunctionQuery: `
				(function_definition name: (name) @name) @function
				(method_declaration name: (name) @name) @function
			`,
			EnclosingTypes:    []string{"class_declaration", "enum_declaration", "trait_declaration", "interface_declaration"},
			EnclosingBarriers: []string{"anonymous_class", "compound_statement"},
		},
		// Ruby
		".rb": {
			Name:     "ruby",
			Language: tree_sitter_ruby.Language(),
			FunctionQuery: `
				(method name: (identifier) @name) @function
				(singleton_method name: (identifier) @name) @function
			`,
		},
		// Bash
		".sh": {
			Name:     "bash",
			Language: tree_sitter_bash.Language(),
			FunctionQuery: `
				(function_definition name: (word) @name) @function
			`,
		},
		".bash": {
			Name:     "bash",
			Language: tree_sitter_bash.Language(),
			FunctionQuery: `
				(function_definition name: (word) @name) @function
			`,
		},
		// JSON (no functions, but can parse)
		".json": {
			Name:          "json",
			Language:      tree_sitter_json.Language(),
			FunctionQuery: ``,
		},
		// HTML (no functions, but can parse)
		".html": {
			Name:          "html",
			Language:      tree_sitter_html.Language(),
			FunctionQuery: ``,
		},
		".htm": {
			Name:          "html",
			Language:      tree_sitter_html.Language(),
			FunctionQuery: ``,
		},
		// CSS (no functions, but can parse)
		".css": {
			Name:          "css",
			Language:      tree_sitter_css.Language(),
			FunctionQuery: ``,
		},
	}
}

// Parser parses source code using tree-sitter
type Parser struct {
	parser *tree_sitter.Parser
}

// NewParser creates a new Parser
func NewParser() *Parser {
	return &Parser{
		parser: tree_sitter.NewParser(),
	}
}

// Close releases parser resources
func (p *Parser) Close() {
	p.parser.Close()
}

// GetConfig returns the language configuration for a file path
func GetConfig(path string) *LanguageConfig {
	ext := strings.ToLower(filepath.Ext(path))
	return languageConfigs[ext]
}

// IsSupported returns true if the file extension is supported
func IsSupported(path string) bool {
	config := GetConfig(path)
	return config != nil && config.FunctionQuery != ""
}

// SupportedExtensions returns all supported file extensions
func SupportedExtensions() []string {
	extensions := make([]string, 0, len(languageConfigs))
	for ext, config := range languageConfigs {
		if config.FunctionQuery != "" {
			extensions = append(extensions, ext)
		}
	}
	return extensions
}

// Parse parses source code and returns all functions
func (p *Parser) Parse(path string, content []byte) ([]Function, string, error) {
	config := GetConfig(path)
	if config == nil {
		return nil, "", fmt.Errorf("unsupported file type: %s", path)
	}

	if config.FunctionQuery == "" {
		return []Function{}, config.Name, nil
	}

	// Set language
	lang := tree_sitter.NewLanguage(config.Language)
	if err := p.parser.SetLanguage(lang); err != nil {
		return nil, "", fmt.Errorf("failed to set language: %w", err)
	}

	// Parse content
	tree := p.parser.Parse(content, nil)
	defer tree.Close()

	// Create query
	query, err := tree_sitter.NewQuery(lang, config.FunctionQuery)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create query: %w", err)
	}
	defer query.Close()

	// Execute query
	cursor := tree_sitter.NewQueryCursor()
	defer cursor.Close()

	matches := cursor.Matches(query, tree.RootNode(), content)

	// Collect functions
	var functions []Function
	seenFunctions := make(map[string]bool)

	for match := matches.Next(); match != nil; match = matches.Next() {
		var fnNode *tree_sitter.Node
		var nameStr string

		for _, capture := range match.Captures {
			captureName := query.CaptureNames()[capture.Index]
			switch captureName {
			case "function":
				fnNode = &capture.Node
			case "name":
				nameStr = capture.Node.Utf8Text(content)
			}
		}

		if fnNode != nil {
			start := fnNode.StartByte()
			startLine := bytes.Count(content[:start], []byte("\n")) + 1
			endLine := int(fnNode.EndPosition().Row) + 1
			key := fmt.Sprintf("%d:%d", startLine, endLine)

			if !seenFunctions[key] {
				seenFunctions[key] = true

				if nameStr == "" {
					nameStr = "<anonymous>"
				}

				functions = append(functions, Function{
					Name:      nameStr,
					StartLine: startLine,
					EndLine:   endLine,
					Content:   strings.TrimSpace(string(content[start:fnNode.EndByte()])),
					Enclosing: findEnclosing(fnNode, config, content),
				})
			}
		}
	}

	return functions, config.Name, nil
}

func findEnclosing(node *tree_sitter.Node, config *LanguageConfig, content []byte) *ClassRef {
	if len(config.EnclosingTypes) == 0 {
		return nil
	}
	for n := node.Parent(); n != nil; n = n.Parent() {
		if slices.Contains(config.EnclosingBarriers, n.Kind()) {
			return nil
		}
		if !slices.Contains(config.EnclosingTypes, n.Kind()) {
			continue
		}
		ref := &ClassRef{
			Kind:      strings.TrimSuffix(n.Kind(), "_declaration"),
			StartLine: int(n.StartPosition().Row) + 1,
			EndLine:   int(n.EndPosition().Row) + 1,
			StartByte: n.StartByte(),
			EndByte:   n.EndByte(),
		}
		if name := n.ChildByFieldName("name"); name != nil {
			ref.Name = name.Utf8Text(content)
		}
		return ref
	}
	return nil
}

// RenderClassHeader renders the context a reviewer needs to judge a method of the
// given class: namespace and imports, the class declaration with its attributes,
// properties, constants, traits, enum cases and the constructor. Other methods are
// replaced by a count.
func (p *Parser) RenderClassHeader(path string, content []byte, ref *ClassRef) (string, error) {
	config := GetConfig(path)
	if config == nil || len(config.EnclosingTypes) == 0 {
		return "", fmt.Errorf("class header not supported for file: %s", path)
	}
	if ref == nil {
		return "", fmt.Errorf("nil class reference")
	}

	lang := tree_sitter.NewLanguage(config.Language)
	if err := p.parser.SetLanguage(lang); err != nil {
		return "", fmt.Errorf("failed to set language: %w", err)
	}
	tree := p.parser.Parse(content, nil)
	defer tree.Close()

	isTarget := func(n *tree_sitter.Node) bool {
		return slices.Contains(config.EnclosingTypes, n.Kind()) && n.StartByte() == ref.StartByte && n.EndByte() == ref.EndByte
	}
	class := tree.RootNode().NamedDescendantForByteRange(ref.StartByte, ref.EndByte)
	for class != nil && !isTarget(class) {
		class = class.Parent()
	}
	if class == nil {
		return "", fmt.Errorf("no enclosing type found at bytes %d-%d", ref.StartByte, ref.EndByte)
	}
	body := class.ChildByFieldName("body")
	if body == nil {
		return "", fmt.Errorf("enclosing type %s has no body", ref.Name)
	}

	var members []string
	for i := uint(0); i < body.NamedChildCount(); i++ {
		member := body.NamedChild(i)
		start := docStart(member, content)
		switch member.Kind() {
		case "property_declaration", "const_declaration", "use_declaration", "enum_case":
			members = append(members, indentedText(start, member.EndByte(), content))
		case "method_declaration":
			name := member.ChildByFieldName("name")
			methodBody := member.ChildByFieldName("body")
			switch {
			case name != nil && strings.EqualFold(name.Utf8Text(content), "__construct"):
				members = append(members, indentedText(start, member.EndByte(), content))
			case methodBody != nil:
				members = append(members, strings.TrimRight(indentedText(start, methodBody.StartByte(), content), " \t\r\n")+" { ... }")
			default:
				members = append(members, indentedText(start, member.EndByte(), content))
			}
		}
	}

	header := string(content[docStart(class, content) : body.StartByte()+1])
	if len(members) > 0 {
		header += "\n" + strings.Join(members, "\n\n")
	}
	header += "\n}"
	if prelude := renderPrelude(class, content); prelude != "" {
		header = prelude + "\n\n" + header
	}
	return header, nil
}

// renderPrelude returns the namespace and import statements preceding the class.
// A brace-delimited namespace gets a synthesized "namespace X;" line.
func renderPrelude(class *tree_sitter.Node, content []byte) string {
	container := class.Parent()
	if container == nil {
		return ""
	}

	var lines []string
	switch {
	case container.Kind() == "program":
	case container.Kind() == "compound_statement" && container.Parent() != nil && container.Parent().Kind() == "namespace_definition":
		if name := container.Parent().ChildByFieldName("name"); name != nil {
			lines = append(lines, "namespace "+name.Utf8Text(content)+";")
		}
	default:
		return ""
	}

	for i := uint(0); i < container.NamedChildCount(); i++ {
		child := container.NamedChild(i)
		if child.StartByte() >= class.StartByte() {
			break
		}
		switch child.Kind() {
		case "namespace_definition":
			if child.ChildByFieldName("body") == nil {
				lines = []string{child.Utf8Text(content)}
			}
		case "namespace_use_declaration":
			lines = append(lines, child.Utf8Text(content))
		}
	}
	return strings.Join(lines, "\n")
}

// docStart returns where a declaration begins once its preceding /** ... */ block is
// included, or the node start when there is none.
func docStart(node *tree_sitter.Node, content []byte) uint {
	prev := node.PrevNamedSibling()
	if prev == nil || prev.Kind() != "comment" || !strings.HasPrefix(prev.Utf8Text(content), "/**") {
		return node.StartByte()
	}
	if len(bytes.TrimSpace(content[prev.EndByte():node.StartByte()])) > 0 {
		return node.StartByte()
	}
	return prev.StartByte()
}

// indentedText returns content[start:end] extended back to the beginning of its line
// when only whitespace precedes it, so that the original indentation is preserved.
func indentedText(start, end uint, content []byte) string {
	lineStart := start
	for lineStart > 0 && (content[lineStart-1] == ' ' || content[lineStart-1] == '\t') {
		lineStart--
	}
	if lineStart > 0 && content[lineStart-1] != '\n' {
		lineStart = start
	}
	return string(content[lineStart:end])
}
