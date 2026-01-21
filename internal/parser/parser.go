package parser

import (
	"fmt"
	"path/filepath"
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
}

// LanguageConfig holds configuration for a specific language
type LanguageConfig struct {
	Name          string
	Language      unsafe.Pointer
	FunctionQuery string // Tree-sitter query to find functions
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
			startLine := int(fnNode.StartPosition().Row) + 1
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
					Content:   strings.TrimSpace(fnNode.Utf8Text(content)),
				})
			}
		}
	}

	return functions, config.Name, nil
}
