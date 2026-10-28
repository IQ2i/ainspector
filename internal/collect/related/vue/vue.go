// Package vue resolves the files related to a Vue component
package vue

import (
	"context"
	"path"
	"regexp"
	"strings"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/collect/related"
)

// Resolvers returns the resolvers of the vue preset
func Resolvers() []related.Resolver {
	return []related.Resolver{component{}}
}

var importRe = regexp.MustCompile(`(?:from|import)\s+['"]([^'"]+)['"]`)

// extensions tried, in order, to turn an import into a file
var candidates = []string{"", ".ts", ".js", "/index.ts", "/index.js"}

// component joins the components that use a component, and the stores and composables it imports
type component struct{}

func (component) Name() string { return "vue/component" }

func (component) Resolve(_ context.Context, u *collect.ReviewUnit, repo related.Repo) ([]collect.RelatedFile, error) {
	if !strings.HasSuffix(u.FilePath, ".vue") {
		return nil, nil
	}
	name := strings.TrimSuffix(path.Base(u.FilePath), ".vue")

	var files []collect.RelatedFile

	parents, err := repo.Grep(`from\s+['"][^'"]*/`+regexp.QuoteMeta(name)+`\.vue['"]`, "**/*.vue", "**/*.ts", "**/*.js")
	if err != nil {
		return nil, err
	}
	for _, p := range parents {
		if f, ok := related.Load(repo, p, "imports the component "+name); ok {
			files = append(files, f)
		}
	}

	source, err := repo.Read(u.FilePath)
	if err != nil {
		return files, nil
	}
	for _, m := range importRe.FindAllStringSubmatch(source, -1) {
		target := resolveImport(path.Dir(u.FilePath), m[1])
		if target == "" || !isShared(target) {
			continue
		}
		for _, ext := range candidates {
			if f, ok := related.Load(repo, target+ext, "store or composable imported by "+name); ok {
				files = append(files, f)
				break
			}
		}
	}
	return files, nil
}

// resolveImport maps a relative or aliased import to a path of the project, "" for a package
func resolveImport(dir, spec string) string {
	var p string
	switch {
	case strings.HasPrefix(spec, "./"), strings.HasPrefix(spec, "../"):
		p = path.Join(dir, spec)
	case strings.HasPrefix(spec, "@/"), strings.HasPrefix(spec, "~/"):
		p = path.Clean("src/" + spec[2:])
		// The alias stands for src/: a ".." must not climb out of it
		if !strings.HasPrefix(p, "src/") {
			return ""
		}
	default:
		return ""
	}
	p = path.Clean(p)
	if p == ".." || strings.HasPrefix(p, "../") {
		return ""
	}
	return p
}

// isShared tells whether an import is a store or a composable, by its folder or its name
func isShared(p string) bool {
	if strings.HasSuffix(p, ".vue") {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "stores" || seg == "store" || seg == "composables" {
			return true
		}
	}
	return strings.HasPrefix(path.Base(p), "use")
}
