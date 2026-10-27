package parser

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// HeaderSpec describes, for one grammar, how the header of a class is rendered: the
// declaration, the members that matter to judge a method, and signatures for the others.
type HeaderSpec struct {
	// BodyField is the field of the class node holding its members
	BodyField string
	// Braces tells that the body opens with a brace, which then ends the declaration line
	Braces bool
	// Close ends the rendered class ("}" or "end"), empty for indentation-based languages
	Close string
	// MemberKinds are rendered in full: fields, constants, constructors, class-level calls
	MemberKinds []string
	// MethodKinds are reduced to their signature, except constructors
	MethodKinds []string
	// ConstructorNames are the method names rendered in full, compared without case
	ConstructorNames []string
	// MethodStub replaces the body of a method
	MethodStub string
	// PreludeKinds are the imports preceding the class (use, import, using, require)
	PreludeKinds []string
	// ClassWrappers are parents of a class node that carry its decorators or export keyword
	ClassWrappers []string
	// Namespace describes the namespace statements put in the prelude, none when empty
	NamespaceKind, NamespaceBodyKind, FileNamespaceKind string
}

// RenderClassHeader renders the context a reviewer needs to judge a method of the
// given class: imports, the class declaration with its attributes, properties,
// constants, constructor, and the signatures of the other methods.
func (p *Parser) RenderClassHeader(path string, content []byte, ref *ClassRef) (string, error) {
	config := GetConfig(path)
	if config == nil || config.Header == nil {
		return "", fmt.Errorf("class header not supported for file: %s", path)
	}
	if ref == nil {
		return "", fmt.Errorf("nil class reference")
	}
	spec := config.Header

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
	body := class.ChildByFieldName(spec.BodyField)
	if body == nil {
		return "", fmt.Errorf("enclosing type %s has no body", ref.Name)
	}

	// outer is the node the declaration starts with: the class, or what wraps it
	outer := class
	if parent := class.Parent(); parent != nil && slices.Contains(spec.ClassWrappers, parent.Kind()) {
		outer = parent
	}

	var members []string
	pending := -1
	for i := uint(0); i < body.NamedChildCount(); i++ {
		member := body.NamedChild(i)
		if slices.Contains(config.LeadingKinds, member.Kind()) {
			if pending < 0 {
				pending = int(member.StartByte())
			}
			continue
		}

		start := docStart(member, content)
		if pending >= 0 {
			start = uint(pending)
			pending = -1
		}
		node := member
		if slices.Contains(config.DecoratedKinds, member.Kind()) {
			if def := member.ChildByFieldName("definition"); def != nil {
				node = def
			}
		}

		switch {
		case slices.Contains(spec.MemberKinds, node.Kind()):
			members = append(members, indentedText(start, withSemicolon(member.EndByte(), content), content))
		case slices.Contains(spec.MethodKinds, node.Kind()):
			members = append(members, renderMethod(spec, node, member, start, content))
		}
	}

	headerEnd := body.StartByte()
	if spec.Braces {
		headerEnd++
	}
	header := strings.TrimRight(string(content[docStart(outer, content):headerEnd]), " \t\r\n")
	if len(members) > 0 {
		header += "\n" + strings.Join(members, "\n\n")
	}
	if spec.Close != "" {
		header += "\n" + spec.Close
	}
	if prelude := renderPrelude(spec, outer, content); prelude != "" {
		header = prelude + "\n\n" + header
	}
	return header, nil
}

// renderMethod renders a constructor in full and any other method as its signature.
// node is the method itself, member what the class body holds (the method or its decorated wrapper).
func renderMethod(spec *HeaderSpec, node, member *tree_sitter.Node, start uint, content []byte) string {
	name := node.ChildByFieldName("name")
	body := node.ChildByFieldName("body")
	switch {
	case name != nil && slices.ContainsFunc(spec.ConstructorNames, func(c string) bool { return strings.EqualFold(c, name.Utf8Text(content)) }):
		return indentedText(start, member.EndByte(), content)
	case body != nil:
		return strings.TrimRight(indentedText(start, body.StartByte(), content), " \t\r\n") + spec.MethodStub
	}
	return indentedText(start, member.EndByte(), content)
}

// renderPrelude returns the namespace and import statements preceding the class.
// A brace-delimited namespace gets a synthesized "namespace X;" line.
func renderPrelude(spec *HeaderSpec, outer *tree_sitter.Node, content []byte) string {
	container := outer.Parent()
	if container == nil {
		return ""
	}

	var lines []string
	collect := func(parent *tree_sitter.Node, before uint) {
		for i := uint(0); i < parent.NamedChildCount(); i++ {
			child := parent.NamedChild(i)
			if child.StartByte() >= before {
				break
			}
			switch {
			case spec.FileNamespaceKind != "" && child.Kind() == spec.FileNamespaceKind && child.ChildByFieldName("body") == nil:
				lines = append(lines, child.Utf8Text(content))
			case slices.Contains(spec.PreludeKinds, child.Kind()):
				lines = append(lines, child.Utf8Text(content))
			}
		}
	}

	switch {
	case container.Parent() == nil:
		collect(container, outer.StartByte())
	case spec.NamespaceBodyKind != "" && container.Kind() == spec.NamespaceBodyKind && container.Parent().Kind() == spec.NamespaceKind:
		namespace := container.Parent()
		if root := namespace.Parent(); root != nil {
			collect(root, namespace.StartByte())
		}
		if name := namespace.ChildByFieldName("name"); name != nil {
			lines = append(lines, "namespace "+name.Utf8Text(content)+";")
		}
		collect(container, outer.StartByte())
	default:
		return ""
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

// withSemicolon extends an end offset over the semicolon that follows a TypeScript or
// JavaScript field, which the grammar leaves out of the node.
func withSemicolon(end uint, content []byte) uint {
	if end < uint(len(content)) && content[end] == ';' {
		return end + 1
	}
	return end
}
