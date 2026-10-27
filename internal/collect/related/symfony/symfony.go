// Package symfony resolves the files related to the code of a Symfony application
package symfony

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/collect/related"
)

const securityConfig = "config/packages/security.yaml"

// Resolvers returns the resolvers of the symfony preset
func Resolvers() []related.Resolver {
	return []related.Resolver{controller{}, entity{}, form{}, voter{}, template{}}
}

var (
	grantRe    = regexp.MustCompile(`(?:IsGranted|isGranted|denyAccessUnlessGranted|isGrantedOrThrow)\(\s*(?:['"]([^'"]+)['"]|([A-Z]\w*)::\w+)`)
	renderRe   = regexp.MustCompile(`(?:render|renderView|renderBlock|renderForm|stream)\(\s*['"]([^'"]+\.twig)['"]|Template\(\s*['"]([^'"]+)['"]`)
	repoRe     = regexp.MustCompile(`repositoryClass:\s*\\?(?:[\w\\]*\\)?(\w+)::class`)
	dataClsRe  = regexp.MustCompile(`data_class['"]?\s*=>\s*(\\?[\w\\]+)::class`)
	constRe    = regexp.MustCompile(`const\s+\w+\s*=\s*['"]([^'"]+)['"]`)
	paramTypRe = regexp.MustCompile(`\b([A-Z]\w*)\s+&?\$\w+`)
	// identRe is what a class or file name must be to go into a glob
	identRe = regexp.MustCompile(`^\w+$`)
)

func matches(pattern, p string) bool {
	ok, _ := doublestar.Match(pattern, p)
	return ok
}

// controller joins the voters, the security configuration and the templates of an action
type controller struct{}

func (controller) Name() string { return "symfony/controller" }

func (controller) Resolve(_ context.Context, u *collect.ReviewUnit, repo related.Repo) ([]collect.RelatedFile, error) {
	if !matches("src/Controller/**/*.php", u.FilePath) {
		return nil, nil
	}

	text := u.Content
	if u.Enclosing != nil {
		text += "\n" + u.Enclosing.Rendered
	}

	var files []collect.RelatedFile
	add := func(p, reason string) {
		if f, ok := related.Load(repo, p, reason); ok {
			files = append(files, f)
		}
	}

	// A voter named after the entity the action receives
	for _, name := range entityTypes(u.Content) {
		paths, err := repo.Glob("src/**/" + name + "Voter.php")
		if err != nil {
			return nil, err
		}
		for _, p := range paths {
			add(p, fmt.Sprintf("voter named after %s, which %s receives", name, u.Name))
		}
	}

	// The voters that handle the attributes checked by the action or its class
	for _, m := range grantRe.FindAllStringSubmatch(text, -1) {
		var paths []string
		var err error
		switch {
		case m[1] != "":
			paths, err = repo.Grep(`['"]`+regexp.QuoteMeta(m[1])+`['"]`, "src/**/*Voter.php")
		case strings.HasSuffix(m[2], "Voter"):
			paths, err = repo.Glob("src/**/" + m[2] + ".php")
		default:
			paths, err = repo.Grep(`\b`+regexp.QuoteMeta(m[2])+`::`, "src/**/*Voter.php")
		}
		if err != nil {
			return nil, err
		}
		for _, p := range paths {
			add(p, "voter handling an attribute checked by "+u.Name)
		}
	}

	if f, ok := accessRules(repo); ok {
		files = append(files, f)
	}

	for _, m := range renderRe.FindAllStringSubmatch(u.Content, -1) {
		name := m[1] + m[2]
		if strings.HasPrefix(name, "@") || strings.Contains(name, "..") {
			continue
		}
		add("templates/"+strings.TrimPrefix(name, "/"), "template rendered by "+u.Name)
	}
	return files, nil
}

// entityTypes lists the class names typed in the signature of the first function of s
func entityTypes(s string) []string {
	i := strings.Index(s, "function")
	if i < 0 {
		return nil
	}
	sig := s[i:]
	if j := strings.Index(sig, "{"); j >= 0 {
		sig = sig[:j]
	}

	var names []string
	for _, m := range paramTypRe.FindAllStringSubmatch(sig, -1) {
		names = append(names, m[1])
	}
	return names
}

// accessRules returns the firewalls and access_control of the security configuration, in the
// order of the file, since the first match wins: the rest is of no use to judge an action. It
// fails closed, a file that cannot be narrowed down is not given at all, and the secrets that
// firewalls may hold are dropped.
func accessRules(repo related.Repo) (collect.RelatedFile, bool) {
	f, ok := related.Load(repo, securityConfig, "firewalls and access_control of the security configuration")
	if !ok {
		return f, false
	}

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(f.Content), &doc); err != nil || doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return f, false
	}
	security := mappingValue(doc.Content[0], "security")
	if security == nil {
		return f, false
	}

	kept := &yaml.Node{Kind: yaml.MappingNode}
	for i := 0; i+1 < len(security.Content); i += 2 {
		if key := security.Content[i].Value; key == "firewalls" || key == "access_control" {
			dropSecrets(security.Content[i+1])
			kept.Content = append(kept.Content, security.Content[i], security.Content[i+1])
		}
	}
	if len(kept.Content) == 0 {
		return f, false
	}

	out, err := yaml.Marshal(&yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{{Kind: yaml.ScalarNode, Value: "security"}, kept}})
	if err != nil {
		return f, false
	}
	f.Content = string(out)
	return f, true
}

// mappingValue returns the value of a key of a mapping node, nil when there is none
func mappingValue(n *yaml.Node, key string) *yaml.Node {
	if n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

// dropSecrets removes the "secret" entries of a tree, e.g. the one of remember_me
func dropSecrets(n *yaml.Node) {
	if n.Kind == yaml.MappingNode {
		kept := n.Content[:0]
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value != "secret" {
				kept = append(kept, n.Content[i], n.Content[i+1])
			}
		}
		n.Content = kept
	}
	for _, c := range n.Content {
		dropSecrets(c)
	}
}

// entity joins the repository and the serialization configuration of an entity
type entity struct{}

func (entity) Name() string { return "symfony/entity" }

func (entity) Resolve(_ context.Context, u *collect.ReviewUnit, repo related.Repo) ([]collect.RelatedFile, error) {
	if !matches("src/Entity/**/*.php", u.FilePath) {
		return nil, nil
	}
	source, err := repo.Read(u.FilePath)
	if err != nil {
		return nil, nil
	}
	name := strings.TrimSuffix(path.Base(u.FilePath), ".php")
	if !identRe.MatchString(name) {
		return nil, nil
	}

	var files []collect.RelatedFile
	repoName := name + "Repository"
	if m := repoRe.FindStringSubmatch(source); m != nil {
		repoName = m[1]
	}
	paths, err := repo.Glob("src/**/" + repoName + ".php")
	if err != nil {
		return nil, err
	}
	for _, p := range paths {
		if f, ok := related.Load(repo, p, "repository of "+name); ok {
			files = append(files, f)
		}
	}

	if !strings.Contains(source, "Groups") {
		paths, err := repo.Glob("config/serializer/**/" + name + ".{yaml,yml,xml}")
		if err != nil {
			return nil, err
		}
		for _, p := range paths {
			if f, ok := related.Load(repo, p, "serialization configuration of "+name); ok {
				files = append(files, f)
			}
		}
	}
	return files, nil
}

// form joins the entity a form type is mapped to
type form struct{}

func (form) Name() string { return "symfony/form" }

func (form) Resolve(_ context.Context, u *collect.ReviewUnit, repo related.Repo) ([]collect.RelatedFile, error) {
	if !matches("src/Form/**/*.php", u.FilePath) {
		return nil, nil
	}
	source, err := repo.Read(u.FilePath)
	if err != nil {
		return nil, nil
	}
	m := dataClsRe.FindStringSubmatch(source)
	if m == nil {
		return nil, nil
	}

	class := strings.TrimPrefix(m[1], `\`)
	short := class[strings.LastIndex(class, `\`)+1:]
	if !strings.Contains(class, `\`) {
		if use := regexp.MustCompile(`use\s+([\w\\]+\\` + regexp.QuoteMeta(short) + `)\s*;`).FindStringSubmatch(source); use != nil {
			class = use[1]
		}
	}

	reason := fmt.Sprintf("entity mapped by %s (data_class)", strings.TrimSuffix(path.Base(u.FilePath), ".php"))
	if p := classPath(class); p != "" {
		if f, ok := related.Load(repo, p, reason); ok {
			return []collect.RelatedFile{f}, nil
		}
	}
	paths, err := repo.Glob("src/**/" + short + ".php")
	if err != nil || len(paths) != 1 {
		return nil, err
	}
	if f, ok := related.Load(repo, paths[0], reason); ok {
		return []collect.RelatedFile{f}, nil
	}
	return nil, nil
}

// classPath maps a class of the App namespace to its file, following the PSR-4 layout of Symfony
func classPath(class string) string {
	rest, ok := strings.CutPrefix(class, `App\`)
	if !ok {
		return ""
	}
	return "src/" + strings.ReplaceAll(rest, `\`, "/") + ".php"
}

// voter joins the controllers that invoke it
type voter struct{}

func (voter) Name() string { return "symfony/voter" }

func (voter) Resolve(_ context.Context, u *collect.ReviewUnit, repo related.Repo) ([]collect.RelatedFile, error) {
	if !strings.HasSuffix(u.FilePath, "Voter.php") {
		return nil, nil
	}
	source, err := repo.Read(u.FilePath)
	if err != nil {
		return nil, nil
	}
	class := strings.TrimSuffix(path.Base(u.FilePath), ".php")

	patterns := []string{`\b` + regexp.QuoteMeta(class) + `::`}
	var values []string
	for _, m := range constRe.FindAllStringSubmatch(source, -1) {
		values = append(values, regexp.QuoteMeta(m[1]))
	}
	if len(values) > 0 {
		patterns = append(patterns, `(?:IsGranted|isGranted|denyAccessUnlessGranted|isGrantedOrThrow)\(\s*['"](?:`+strings.Join(values, "|")+`)['"]`)
	}

	paths, err := repo.Grep("(?:"+strings.Join(patterns, ")|(?:")+")", "src/Controller/**/*.php")
	if err != nil {
		return nil, err
	}
	var files []collect.RelatedFile
	for _, p := range paths {
		if f, ok := related.Load(repo, p, "controller invoking "+class); ok {
			files = append(files, f)
		}
	}
	return files, nil
}

// template joins the code that renders it
type template struct{}

func (template) Name() string { return "symfony/template" }

func (template) Resolve(_ context.Context, u *collect.ReviewUnit, repo related.Repo) ([]collect.RelatedFile, error) {
	name, ok := strings.CutPrefix(u.FilePath, "templates/")
	if !ok || !strings.HasSuffix(name, ".twig") {
		return nil, nil
	}

	paths, err := repo.Grep(`['"]`+regexp.QuoteMeta(name)+`['"]`, "src/**/*.php")
	if err != nil {
		return nil, err
	}
	var files []collect.RelatedFile
	for _, p := range paths {
		if f, ok := related.Load(repo, p, "code rendering "+name); ok {
			files = append(files, f)
		}
	}
	return files, nil
}
