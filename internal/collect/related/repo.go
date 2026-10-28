package related

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/iq2i/ainspector/internal/config"
)

const (
	// maxFileBytes is the most read from a file: larger ones are not source code worth joining
	maxFileBytes = 1 << 20
	// maxCacheBytes bounds the file contents kept in memory between two reads
	maxCacheBytes = 64 << 20
)

// skippedDirs are never listed nor read: dependencies, caches, version control and build output
var skippedDirs = map[string]bool{".git": true, "vendor": true, "node_modules": true, "var": true, "dist": true}

// skippedPrefixes are folders of compiled assets
var skippedPrefixes = []string{"public/build/"}

// skippedFiles are the names, or name patterns, of generated files
var skippedFiles = []string{"*.min.js", "*.map"}

type dirRepo struct {
	root         string
	resolvedRoot string
	ignore       func(string) bool

	once  sync.Once
	files []string

	mu         sync.Mutex
	cache      map[string]string
	cacheBytes int
	greps      map[string][]string
}

// NewRepo reads the checkout under root. ignore tells the paths the project excludes from
// review (it may be nil), which are not given as context either.
func NewRepo(root string, ignore func(string) bool) Repo {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		resolved = root
	}
	return &dirRepo{root: root, resolvedRoot: resolved, ignore: ignore, cache: make(map[string]string), greps: make(map[string][]string)}
}

// excluded keeps out the paths that must never reach a prompt: dependencies, generated assets
// and the files which hold the secrets of the project
func (r *dirRepo) excluded(rel string) bool {
	for _, prefix := range skippedPrefixes {
		if strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	segments := strings.Split(rel, "/")
	for _, s := range segments[:len(segments)-1] {
		if skippedDirs[s] {
			return true
		}
	}
	base := segments[len(segments)-1]
	for _, pattern := range skippedFiles {
		if ok, _ := path.Match(pattern, base); ok {
			return true
		}
	}
	return config.IsSensitivePath(rel) || (r.ignore != nil && r.ignore(rel))
}

func (r *dirRepo) list() []string {
	r.once.Do(func() {
		_ = filepath.WalkDir(r.root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if p != r.root && skippedDirs[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			// Symbolic links and special files are left out: the checkout is the content of the PR/MR
			if !d.Type().IsRegular() {
				return nil
			}
			rel, err := filepath.Rel(r.root, p)
			if err != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			if !r.excluded(rel) {
				r.files = append(r.files, rel)
			}
			return nil
		})
		sort.Strings(r.files)
	})
	return r.files
}

func (r *dirRepo) Read(p string) (string, error) {
	rel := path.Clean(filepath.ToSlash(p))
	if rel == "." || path.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("%s: %w", p, os.ErrNotExist)
	}
	if r.excluded(rel) || r.throughLink(rel) {
		return "", fmt.Errorf("%s: %w", p, os.ErrNotExist)
	}

	r.mu.Lock()
	content, ok := r.cache[rel]
	r.mu.Unlock()
	if ok {
		return content, nil
	}

	data, err := config.ReadProjectFile(r.root, rel, maxFileBytes)
	if err != nil {
		return "", err
	}
	content = string(data)

	r.mu.Lock()
	if r.cacheBytes+len(content) <= maxCacheBytes {
		r.cache[rel] = content
		r.cacheBytes += len(content)
	}
	r.mu.Unlock()
	return content, nil
}

// throughLink tells whether reading rel goes through a symbolic link. The exclusions apply to
// the path as written, so a link named like an allowed file could otherwise lead to an excluded one.
func (r *dirRepo) throughLink(rel string) bool {
	resolved, err := filepath.EvalSymlinks(filepath.Join(r.root, rel))
	if err != nil {
		return true
	}
	inside, err := filepath.Rel(r.resolvedRoot, resolved)
	return err != nil || filepath.ToSlash(inside) != rel
}

func (r *dirRepo) Glob(pattern string) ([]string, error) {
	if !doublestar.ValidatePattern(pattern) {
		return nil, fmt.Errorf("invalid glob %q", pattern)
	}
	var matches []string
	for _, f := range r.list() {
		if ok, _ := doublestar.Match(pattern, f); ok {
			matches = append(matches, f)
		}
	}
	return matches, nil
}

func (r *dirRepo) Grep(pattern string, globs ...string) ([]string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}

	key := pattern + "\x00" + strings.Join(globs, "\x00")
	r.mu.Lock()
	memo, ok := r.greps[key]
	r.mu.Unlock()
	if ok {
		return memo, nil
	}

	var matches []string
	for _, f := range r.list() {
		if len(globs) > 0 && !matchesAny(globs, f) {
			continue
		}
		content, err := r.Read(f)
		if err != nil {
			continue
		}
		if re.MatchString(content) {
			matches = append(matches, f)
		}
	}

	r.mu.Lock()
	r.greps[key] = matches
	r.mu.Unlock()
	return matches, nil
}

func matchesAny(globs []string, f string) bool {
	for _, g := range globs {
		if ok, _ := doublestar.Match(g, f); ok {
			return true
		}
	}
	return false
}
