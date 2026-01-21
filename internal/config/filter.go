package config

import (
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
