package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// maxContextFiles caps the files read for the project context
const maxContextFiles = 200

// errEnough stops a walk once a pattern matched more files than can be read
var errEnough = errors.New("enough files")

// CheckContextPattern rejects an include or exclude pattern that leaves the project: the
// patterns come from the configuration of the change under review
func CheckContextPattern(pattern string) error {
	if pattern == "" {
		return errors.New("empty pattern")
	}
	if filepath.IsAbs(pattern) || strings.HasPrefix(pattern, "/") || strings.HasPrefix(pattern, `\`) || filepath.VolumeName(pattern) != "" {
		return fmt.Errorf("%q: must be relative", pattern)
	}
	for _, segment := range strings.FieldsFunc(pattern, func(r rune) bool { return r == '/' || r == '\\' }) {
		if segment == ".." {
			return fmt.Errorf("%q: must not contain a parent directory segment", pattern)
		}
	}
	return nil
}

// ContextFile is a file given as project context
type ContextFile struct {
	Path    string
	Content string
}

// CollectContextFiles collects files based on include/exclude patterns, up to maxContextFiles
// files and until their size reaches the limit of the context. The files named one by one come
// first, then those found in directories and globs, each group in alphabetical order: this is
// the order in which the limit of the context cuts.
// Returns the files in that order, warnings, and error
func (c *ContextConfig) CollectContextFiles(projectRoot string) ([]ContextFile, []string, error) {
	if len(c.Include) == 0 {
		return nil, nil, nil
	}

	// Track files and warnings
	var collected []ContextFile
	var warnings []string
	var explicitFiles []string  // Files from direct patterns
	var directoryFiles []string // Files from directory patterns

	// Process each include pattern
	for _, pattern := range c.Include {
		if err := CheckContextPattern(pattern); err != nil {
			warnings = append(warnings, fmt.Sprintf("context pattern ignored: %v", err))
			continue
		}
		files, err := collectFromPattern(projectRoot, pattern)
		if err != nil {
			// Non-existent paths generate warnings, not errors
			if os.IsNotExist(err) {
				warnings = append(warnings, fmt.Sprintf("context path not found: %q", pattern))
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
	total := 0
	for _, relPath := range allFiles {
		if c.shouldExclude(relPath) {
			continue
		}
		if IsSensitivePath(filepath.ToSlash(relPath)) {
			warnings = append(warnings, fmt.Sprintf("context file ignored, it may hold secrets: %q", relPath))
			continue
		}
		if len(collected) >= maxContextFiles || total >= c.Limit() {
			warnings = append(warnings, fmt.Sprintf("context files beyond %d files or %d bytes were left out", maxContextFiles, c.Limit()))
			break
		}

		content, err := ReadProjectFile(projectRoot, relPath, c.Limit())
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to read %q: %v", relPath, err))
			continue
		}

		collected = append(collected, ContextFile{Path: relPath, Content: string(content)})
		total += len(content)
	}

	return collected, warnings, nil
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
				if len(matches) > maxContextFiles {
					return errEnough
				}
			}
			return nil
		}, doublestar.WithNoFollow())

		if err != nil && !errors.Is(err, errEnough) {
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
			if len(files) > maxContextFiles {
				return errEnough
			}
		}

		return nil
	})

	if err != nil && !errors.Is(err, errEnough) {
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
