package config

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// IsGeneratedPath recognizes lock files, minified assets and source maps: they are produced by
// tools, and a single dependency bump would otherwise turn them into dozens of units
func IsGeneratedPath(path string) bool {
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(path, "\\", "/")))
	switch base {
	case "pnpm-lock.yaml", "package-lock.json":
		return true
	}
	for _, suffix := range []string{".lock", ".min.js", ".min.css", ".map"} {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	return false
}

// IsVendoredPath recognizes third-party code kept in the repository, at any depth
func IsVendoredPath(path string) bool {
	for _, dir := range strings.Split(filepath.ToSlash(path), "/") {
		if dir == "vendor" || dir == "node_modules" {
			return true
		}
	}
	return false
}

// IsNestedVendoredPath tells whether a vendored path sits below another directory. Unlike the
// vendor directory at the root of the project, it may be code of the change put out of sight.
func IsNestedVendoredPath(path string) bool {
	first, _, _ := strings.Cut(filepath.ToSlash(path), "/")
	return IsVendoredPath(path) && first != "vendor" && first != "node_modules"
}

// ShouldIgnore checks if a file path should be ignored: vendored and generated files always are,
// the others according to ignore.paths
func (c *Config) ShouldIgnore(path string) bool {
	if IsGeneratedPath(path) || IsVendoredPath(path) {
		return true
	}

	// Normalize path separators
	normalizedPath := strings.ReplaceAll(path, "\\", "/")

	// Check glob patterns
	for _, pattern := range c.Ignore.Paths {
		normalizedPattern := strings.ReplaceAll(pattern, "\\", "/")

		// Handle directory patterns (ending with /)
		if strings.HasSuffix(normalizedPattern, "/") {
			dirPattern := strings.TrimSuffix(normalizedPattern, "/")
			// Check if path starts with directory or is inside directory
			if strings.HasPrefix(normalizedPath, dirPattern+"/") || normalizedPath == dirPattern {
				return true
			}
			// Also match with ** for nested paths
			if matched, _ := doublestar.Match(dirPattern+"/**", normalizedPath); matched {
				return true
			}
			continue
		}

		// Standard glob matching with doublestar support
		if matched, _ := doublestar.Match(normalizedPattern, normalizedPath); matched {
			return true
		}

		// Also check if pattern matches the basename
		basename := normalizedPath
		if idx := strings.LastIndex(normalizedPath, "/"); idx >= 0 {
			basename = normalizedPath[idx+1:]
		}
		if matched, _ := doublestar.Match(normalizedPattern, basename); matched {
			return true
		}
	}

	return false
}

// HasChangeFilters tells whether the PR/MR itself may be ignored, which needs its metadata
func (c *Config) HasChangeFilters() bool {
	return len(c.Ignore.Authors) > 0 || len(c.Ignore.TitlePatterns) > 0 || len(c.Ignore.Labels) > 0
}

// ShouldIgnoreChange checks a PR/MR against the author, title and label filters.
// It returns the reason when the PR/MR must not be reviewed.
func (c *Config) ShouldIgnoreChange(author, title string, labels []string) (bool, string) {
	for _, a := range c.Ignore.Authors {
		if strings.EqualFold(a, author) {
			return true, fmt.Sprintf("author %q is ignored", author)
		}
	}

	for _, pattern := range c.Ignore.TitlePatterns {
		// Patterns are validated when the configuration is loaded
		if re, err := regexp.Compile(pattern); err == nil && re.MatchString(title) {
			return true, fmt.Sprintf("title matches ignored pattern %q", pattern)
		}
	}

	for _, ignored := range c.Ignore.Labels {
		for _, l := range labels {
			if strings.EqualFold(ignored, l) {
				return true, fmt.Sprintf("label %q is ignored", l)
			}
		}
	}

	return false, ""
}
