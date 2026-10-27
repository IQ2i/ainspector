package config

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// ShouldIgnore checks if a file path should be ignored based on the configuration
func (c *Config) ShouldIgnore(path string) bool {
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
			return true, fmt.Sprintf("author %s is ignored", author)
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
				return true, fmt.Sprintf("label %s is ignored", l)
			}
		}
	}

	return false, ""
}
