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
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tree_sitter_java "github.com/tree-sitter/tree-sitter-java/bindings/go"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
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
	// DocComments makes a function start at its preceding /** ... */ block, where
	// annotation-style metadata (@Route, @IsGranted) lives.
	DocComments bool
	// DecoratedKinds are wrappers holding the decorators of the declaration they contain
	// (Python): a function starts at its wrapper.
	DecoratedKinds []string
	// LeadingKinds are siblings decorating the declaration that follows them (TypeScript
	// decorators in a class body): a function starts at the first of them.
	LeadingKinds []string
	// Header tells how the header of an enclosing class is rendered, nil when unsupported
	Header *HeaderSpec
}

var (
	braceClassSpec = HeaderSpec{
		BodyField:  "body",
		Braces:     true,
		Close:      "}",
		MethodStub: " { ... }",
	}

	phpHeader = HeaderSpec{
		BodyField:         "body",
		Braces:            true,
		Close:             "}",
		MemberKinds:       []string{"property_declaration", "const_declaration", "use_declaration", "enum_case"},
		MethodKinds:       []string{"method_declaration"},
		ConstructorNames:  []string{"__construct"},
		MethodStub:        " { ... }",
		PreludeKinds:      []string{"namespace_use_declaration"},
		NamespaceKind:     "namespace_definition",
		NamespaceBodyKind: "compound_statement",
		FileNamespaceKind: "namespace_definition",
	}

	scriptHeader = withSpec(braceClassSpec, func(h *HeaderSpec) {
		h.MemberKinds = []string{"public_field_definition", "field_definition"}
		h.MethodKinds = []string{"method_definition", "abstract_method_signature"}
		h.ConstructorNames = []string{"constructor"}
		h.PreludeKinds = []string{"import_statement"}
		h.ClassWrappers = []string{"export_statement"}
	})

	pythonHeader = HeaderSpec{
		BodyField:        "body",
		MemberKinds:      []string{"expression_statement"},
		MethodKinds:      []string{"function_definition"},
		ConstructorNames: []string{"__init__"},
		MethodStub:       " ...",
		PreludeKinds:     []string{"import_statement", "import_from_statement", "future_import_statement"},
		ClassWrappers:    []string{"decorated_definition"},
	}

	javaHeader = withSpec(braceClassSpec, func(h *HeaderSpec) {
		h.MemberKinds = []string{"field_declaration", "constructor_declaration"}
		h.MethodKinds = []string{"method_declaration"}
		h.PreludeKinds = []string{"package_declaration", "import_declaration"}
	})

	csharpHeader = withSpec(braceClassSpec, func(h *HeaderSpec) {
		h.MemberKinds = []string{"field_declaration", "property_declaration", "constructor_declaration"}
		h.MethodKinds = []string{"method_declaration"}
		h.PreludeKinds = []string{"using_directive"}
		h.NamespaceKind = "namespace_declaration"
		h.NamespaceBodyKind = "declaration_list"
		h.FileNamespaceKind = "file_scoped_namespace_declaration"
	})

	rubyHeader = HeaderSpec{
		BodyField:        "body",
		Close:            "end",
		MemberKinds:      []string{"call", "assignment"},
		MethodKinds:      []string{"method", "singleton_method"},
		ConstructorNames: []string{"initialize"},
		MethodStub:       " ... end",
		PreludeKinds:     []string{"call"},
	}
)

func withSpec(base HeaderSpec, edit func(*HeaderSpec)) *HeaderSpec {
	edit(&base)
	return &base
}

// scriptFunctionQuery finds the functions of JavaScript and of both TypeScript grammars
const scriptFunctionQuery = `
	(function_declaration name: (identifier) @name) @function
	(method_definition name: (property_identifier) @name) @function
	(arrow_function) @function
	(function_expression) @function
`

// languageConfigs maps file extensions to language configurations
var languageConfigs map[string]*LanguageConfig

func init() {
	languageConfigs = make(map[string]*LanguageConfig)

	// A language that spans several extensions is configured once
	add := func(c *LanguageConfig, extensions ...string) {
		for _, ext := range extensions {
			languageConfigs[ext] = c
		}
	}

	add(&LanguageConfig{
		Name:     "go",
		Language: tree_sitter_go.Language(),
		FunctionQuery: `
			(function_declaration name: (identifier) @name) @function
			(method_declaration name: (field_identifier) @name) @function
		`,
	}, ".go")
	add(&LanguageConfig{
		Name:              "javascript",
		Language:          tree_sitter_javascript.Language(),
		FunctionQuery:     scriptFunctionQuery,
		EnclosingTypes:    []string{"class_declaration", "abstract_class_declaration"},
		EnclosingBarriers: []string{"statement_block"},
		LeadingKinds:      []string{"decorator"},
		Header:            scriptHeader,
	}, ".js", ".jsx")
	add(&LanguageConfig{
		Name:              "typescript",
		Language:          tree_sitter_typescript.LanguageTypescript(),
		FunctionQuery:     scriptFunctionQuery,
		EnclosingTypes:    []string{"class_declaration", "abstract_class_declaration"},
		EnclosingBarriers: []string{"statement_block"},
		LeadingKinds:      []string{"decorator"},
		Header:            scriptHeader,
	}, ".ts")
	add(&LanguageConfig{
		Name:              "typescript",
		Language:          tree_sitter_typescript.LanguageTSX(),
		FunctionQuery:     scriptFunctionQuery,
		EnclosingTypes:    []string{"class_declaration", "abstract_class_declaration"},
		EnclosingBarriers: []string{"statement_block"},
		LeadingKinds:      []string{"decorator"},
		Header:            scriptHeader,
	}, ".tsx")
	add(&LanguageConfig{
		Name:     "python",
		Language: tree_sitter_python.Language(),
		FunctionQuery: `
			(function_definition name: (identifier) @name) @function
		`,
		EnclosingTypes:    []string{"class_definition"},
		EnclosingBarriers: []string{"function_definition"},
		DecoratedKinds:    []string{"decorated_definition"},
		Header:            &pythonHeader,
	}, ".py")
	add(&LanguageConfig{
		Name:     "rust",
		Language: tree_sitter_rust.Language(),
		FunctionQuery: `
			(function_item name: (identifier) @name) @function
		`,
	}, ".rs")
	add(&LanguageConfig{
		Name:     "java",
		Language: tree_sitter_java.Language(),
		FunctionQuery: `
			(method_declaration name: (identifier) @name) @function
			(constructor_declaration name: (identifier) @name) @function
		`,
		EnclosingTypes:    []string{"class_declaration", "interface_declaration", "record_declaration"},
		EnclosingBarriers: []string{"object_creation_expression"},
		Header:            javaHeader,
	}, ".java")
	add(&LanguageConfig{
		Name:     "c",
		Language: tree_sitter_c.Language(),
		FunctionQuery: `
			(function_definition declarator: (function_declarator declarator: (identifier) @name)) @function
		`,
	}, ".c", ".h")
	add(&LanguageConfig{
		Name:     "cpp",
		Language: tree_sitter_cpp.Language(),
		FunctionQuery: `
			(function_definition declarator: (function_declarator declarator: (identifier) @name)) @function
			(function_definition declarator: (function_declarator declarator: (qualified_identifier) @name)) @function
		`,
	}, ".cpp", ".cc", ".hpp")
	add(&LanguageConfig{
		Name:     "csharp",
		Language: tree_sitter_csharp.Language(),
		FunctionQuery: `
			(method_declaration name: (identifier) @name) @function
			(constructor_declaration name: (identifier) @name) @function
		`,
		EnclosingTypes:    []string{"class_declaration", "struct_declaration", "interface_declaration", "record_declaration"},
		EnclosingBarriers: []string{"object_creation_expression"},
		Header:            csharpHeader,
	}, ".cs")
	add(&LanguageConfig{
		Name:     "php",
		Language: tree_sitter_php.LanguagePHP(),
		FunctionQuery: `
			(function_definition name: (name) @name) @function
			(method_declaration name: (name) @name) @function
		`,
		EnclosingTypes:    []string{"class_declaration", "enum_declaration", "trait_declaration", "interface_declaration"},
		EnclosingBarriers: []string{"anonymous_class", "compound_statement"},
		DocComments:       true,
		Header:            &phpHeader,
	}, ".php")
	add(&LanguageConfig{
		Name:     "ruby",
		Language: tree_sitter_ruby.Language(),
		FunctionQuery: `
			(method name: (identifier) @name) @function
			(singleton_method name: (identifier) @name) @function
		`,
		EnclosingTypes:    []string{"class", "module"},
		EnclosingBarriers: []string{"method", "singleton_method"},
		Header:            &rubyHeader,
	}, ".rb")
	add(&LanguageConfig{
		Name:     "bash",
		Language: tree_sitter_bash.Language(),
		FunctionQuery: `
			(function_definition name: (word) @name) @function
		`,
	}, ".sh", ".bash")
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
			start := declStart(fnNode, config, content)
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

// declStart returns where a function begins once its decorators and doc comment are included
func declStart(node *tree_sitter.Node, config *LanguageConfig, content []byte) uint {
	start := node.StartByte()
	if parent := node.Parent(); parent != nil && slices.Contains(config.DecoratedKinds, parent.Kind()) {
		node, start = parent, parent.StartByte()
	}
	for prev := node.PrevNamedSibling(); prev != nil && slices.Contains(config.LeadingKinds, prev.Kind()); prev = prev.PrevNamedSibling() {
		start = prev.StartByte()
	}
	if config.DocComments {
		start = min(start, docStart(node, content))
	}
	return start
}
