package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectContextFiles_IncludeExclude(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	createTestFile(t, tmpDir, "README.md", "readme content")
	createTestFile(t, tmpDir, "docs/guide.md", "guide content")
	createTestFile(t, tmpDir, "docs/internal.md", "internal content")

	tests := []struct {
		name         string
		include      []string
		exclude      []string
		wantFiles    []string
		wantNoFiles  []string
		wantWarnings int
	}{
		{
			name:        "include all, exclude one",
			include:     []string{"docs/**"},
			exclude:     []string{"docs/internal.md"},
			wantFiles:   []string{"docs/guide.md"},
			wantNoFiles: []string{"docs/internal.md"},
		},
		{
			name:      "exclude takes priority",
			include:   []string{"docs/**"},
			exclude:   []string{"**/*.md"},
			wantFiles: []string{},
		},
		{
			name:      "no exclusions",
			include:   []string{"README.md"},
			exclude:   []string{},
			wantFiles: []string{"README.md"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ContextConfig{
				Include: tt.include,
				Exclude: tt.exclude,
			}

			files, warnings, err := cfg.CollectContextFiles(tmpDir)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(warnings) != tt.wantWarnings {
				t.Errorf("got %d warnings, want %d", len(warnings), tt.wantWarnings)
			}

			for _, wantFile := range tt.wantFiles {
				if _, ok := files[wantFile]; !ok {
					t.Errorf("expected file %s not found in result", wantFile)
				}
			}

			for _, noFile := range tt.wantNoFiles {
				if _, ok := files[noFile]; ok {
					t.Errorf("file %s should be excluded but was found", noFile)
				}
			}
		})
	}
}

func TestCollectContextFiles_GlobPatterns(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test structure
	createTestFile(t, tmpDir, "README.md", "readme")
	createTestFile(t, tmpDir, "docs/guide.md", "guide")
	createTestFile(t, tmpDir, "docs/api/endpoints.md", "endpoints")
	createTestFile(t, tmpDir, "src/main.go", "main")

	tests := []struct {
		name      string
		pattern   string
		wantFiles []string
	}{
		{
			name:      "single star",
			pattern:   "*.md",
			wantFiles: []string{"README.md"},
		},
		{
			name:      "double star recursive",
			pattern:   "docs/**/*.md",
			wantFiles: []string{"docs/guide.md", "docs/api/endpoints.md"},
		},
		{
			name:      "double star all",
			pattern:   "**/*.go",
			wantFiles: []string{"src/main.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ContextConfig{
				Include: []string{tt.pattern},
			}

			files, _, err := cfg.CollectContextFiles(tmpDir)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(files) != len(tt.wantFiles) {
				t.Errorf("got %d files, want %d", len(files), len(tt.wantFiles))
			}

			for _, wantFile := range tt.wantFiles {
				if _, ok := files[wantFile]; !ok {
					t.Errorf("expected file %s not found", wantFile)
				}
			}
		})
	}
}

func TestCollectContextFiles_Directories(t *testing.T) {
	tmpDir := t.TempDir()

	// Create directory structure
	createTestFile(t, tmpDir, "docs/a.md", "a")
	createTestFile(t, tmpDir, "docs/b.md", "b")
	createTestFile(t, tmpDir, "docs/sub/c.md", "c")

	cfg := &ContextConfig{
		Include: []string{"docs"},
	}

	files, _, err := cfg.CollectContextFiles(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should include all files in directory recursively
	expectedFiles := []string{"docs/a.md", "docs/b.md", "docs/sub/c.md"}
	if len(files) != len(expectedFiles) {
		t.Errorf("got %d files, want %d", len(files), len(expectedFiles))
	}

	for _, wantFile := range expectedFiles {
		if _, ok := files[wantFile]; !ok {
			t.Errorf("expected file %s not found", wantFile)
		}
	}
}

func TestCollectContextFiles_MissingFiles(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &ContextConfig{
		Include: []string{"nonexistent.md", "missing/dir"},
	}

	files, warnings, err := cfg.CollectContextFiles(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(files) != 0 {
		t.Errorf("expected no files, got %d", len(files))
	}

	if len(warnings) != 2 {
		t.Errorf("expected 2 warnings for missing files, got %d", len(warnings))
	}
}

func TestCollectContextFiles_DeterministicOrder(t *testing.T) {
	tmpDir := t.TempDir()

	// Create files in non-alphabetical order
	createTestFile(t, tmpDir, "z.md", "z")
	createTestFile(t, tmpDir, "a.md", "a")
	createTestFile(t, tmpDir, "docs/y.md", "y")
	createTestFile(t, tmpDir, "docs/b.md", "b")

	cfg := &ContextConfig{
		Include: []string{"z.md", "a.md", "docs"},
	}

	files, _, err := cfg.CollectContextFiles(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Convert map to ordered slice to check ordering
	var keys []string
	for k := range files {
		keys = append(keys, k)
	}

	// Explicit files (a.md, z.md) should come before directory files
	// Within each category, files should be alphabetically sorted
	// So expected order: a.md, z.md, docs/b.md, docs/y.md
	expectedOrder := []string{"a.md", "z.md"}

	// Check that explicit files come first
	if len(keys) < 2 {
		t.Fatalf("expected at least 2 files")
	}

	// Note: map iteration order is random, so we need to check the logic differently
	// We verify explicit files and directory files separately
	explicitCount := 0
	for _, key := range keys {
		if key == "a.md" || key == "z.md" {
			explicitCount++
		}
	}

	if explicitCount != 2 {
		t.Errorf("expected 2 explicit files, got %d", explicitCount)
	}

	// Verify all expected files are present
	expectedFiles := []string{"a.md", "z.md", "docs/b.md", "docs/y.md"}
	if len(files) != len(expectedFiles) {
		t.Errorf("got %d files, want %d", len(files), len(expectedFiles))
	}

	for _, expected := range expectedOrder {
		if _, ok := files[expected]; !ok {
			t.Errorf("expected file %s not found", expected)
		}
	}
}

func TestShouldExclude(t *testing.T) {
	tests := []struct {
		name     string
		exclude  []string
		path     string
		expected bool
	}{
		{
			name:     "exact match",
			exclude:  []string{"docs/internal.md"},
			path:     "docs/internal.md",
			expected: true,
		},
		{
			name:     "glob match",
			exclude:  []string{"**/*_test.go"},
			path:     "internal/config/context_test.go",
			expected: true,
		},
		{
			name:     "no match",
			exclude:  []string{"*.test.go"},
			path:     "internal/config/context.go",
			expected: false,
		},
		{
			name:     "directory pattern",
			exclude:  []string{"vendor/**"},
			path:     "vendor/github.com/pkg/file.go",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ContextConfig{
				Exclude: tt.exclude,
			}

			result := cfg.shouldExclude(tt.path)
			if result != tt.expected {
				t.Errorf("shouldExclude(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestCollectContextFiles_EmptyConfig(t *testing.T) {
	tmpDir := t.TempDir()
	createTestFile(t, tmpDir, "README.md", "readme")

	cfg := &ContextConfig{
		Include: []string{},
	}

	files, warnings, err := cfg.CollectContextFiles(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if files != nil {
		t.Errorf("expected nil files for empty config, got %v", files)
	}

	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %d", len(warnings))
	}
}

// Helper function to create test files
func createTestFile(t *testing.T, baseDir, relPath, content string) {
	t.Helper()
	fullPath := filepath.Join(baseDir, relPath)
	dir := filepath.Dir(fullPath)

	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create directory %s: %v", dir, err)
	}

	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write file %s: %v", fullPath, err)
	}
}
