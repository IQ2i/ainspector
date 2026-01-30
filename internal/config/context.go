package config

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// CollectContextFiles collects files based on include/exclude patterns
// Returns map[relativePath]content, warnings, and error
func (c *ContextConfig) CollectContextFiles(projectRoot string) (map[string]string, []string, error) {
	if len(c.Include) == 0 {
		return nil, nil, nil
	}

	// Track files and warnings
	fileMap := make(map[string]string)
	var warnings []string
	var explicitFiles []string  // Files from direct patterns
	var directoryFiles []string // Files from directory patterns

	// Process each include pattern
	for _, pattern := range c.Include {
		files, err := collectFromPattern(projectRoot, pattern)
		if err != nil {
			// Non-existent paths generate warnings, not errors
			if os.IsNotExist(err) {
				warnings = append(warnings, fmt.Sprintf("context path not found: %s", pattern))
				continue
			}
			return nil, warnings, err
		}

		// Categorize files: explicit vs directory
		if len(files) == 1 && !isDirectory(filepath.Join(projectRoot, files[0])) {
			explicitFiles = append(explicitFiles, files[0])
		} else {
			directoryFiles = append(directoryFiles, files...)
		}
	}

	// Sort both categories alphabetically
	sort.Strings(explicitFiles)
	sort.Strings(directoryFiles)

	// Combine: explicit files first, then directory files
	allFiles := append(explicitFiles, directoryFiles...)

	// Read file contents, applying exclusions
	for _, relPath := range allFiles {
		if c.shouldExclude(relPath) {
			continue
		}

		fullPath := filepath.Join(projectRoot, relPath)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to read %s: %v", relPath, err))
			continue
		}

		fileMap[relPath] = string(content)
	}

	return fileMap, warnings, nil
}

// shouldExclude checks if a path matches any exclusion pattern
func (c *ContextConfig) shouldExclude(path string) bool {
	for _, pattern := range c.Exclude {
		matched, err := doublestar.Match(pattern, path)
		if err == nil && matched {
			return true
		}
	}
	return false
}

// collectFromPattern processes a single include pattern (file or directory)
// Returns relative paths from projectRoot
func collectFromPattern(projectRoot, pattern string) ([]string, error) {
	// Check if pattern contains glob characters
	hasGlob := strings.ContainsAny(pattern, "*?[]")

	if hasGlob {
		// Use doublestar to match glob patterns
		basePath := projectRoot
		var matches []string

		err := doublestar.GlobWalk(os.DirFS(basePath), pattern, func(path string, d fs.DirEntry) error {
			if !d.IsDir() {
				matches = append(matches, path)
			}
			return nil
		})

		if err != nil {
			return nil, err
		}

		if len(matches) == 0 {
			return nil, os.ErrNotExist
		}

		return matches, nil
	}

	// No glob - check if it's a file or directory
	fullPath := filepath.Join(projectRoot, pattern)
	info, err := os.Stat(fullPath)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		// Single file
		return []string{pattern}, nil
	}

	// Directory - walk and collect all files
	var files []string
	err = filepath.WalkDir(fullPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() {
			relPath, err := filepath.Rel(projectRoot, path)
			if err != nil {
				return err
			}
			files = append(files, relPath)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return files, nil
}

// isDirectory checks if a path is a directory
func isDirectory(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
