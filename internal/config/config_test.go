package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	// Save current directory and restore after test
	originalDir, _ := os.Getwd()
	defer func() { _ = os.Chdir(originalDir) }()

	t.Run("returns empty config when no file exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		_ = os.Chdir(tmpDir)

		cfg, err := Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg == nil {
			t.Fatal("expected non-nil config")
		}
		if len(cfg.Ignore.Paths) != 0 {
			t.Errorf("expected empty paths, got %v", cfg.Ignore.Paths)
		}
	})

	t.Run("loads ainspector.yaml", func(t *testing.T) {
		tmpDir := t.TempDir()
		_ = os.Chdir(tmpDir)

		content := `ignore:
  paths:
    - vendor/
    - "*.test.go"
`
		_ = os.WriteFile("ainspector.yaml", []byte(content), 0644)

		cfg, err := Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cfg.Ignore.Paths) != 2 {
			t.Errorf("expected 2 paths, got %d", len(cfg.Ignore.Paths))
		}
	})

	t.Run("loads ainspector.yml as fallback", func(t *testing.T) {
		tmpDir := t.TempDir()
		_ = os.Chdir(tmpDir)

		content := `ignore:
  paths:
    - node_modules/
`
		_ = os.WriteFile("ainspector.yml", []byte(content), 0644)

		cfg, err := Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cfg.Ignore.Paths) != 1 {
			t.Errorf("expected 1 path, got %d", len(cfg.Ignore.Paths))
		}
		if cfg.Ignore.Paths[0] != "node_modules/" {
			t.Errorf("expected node_modules/, got %s", cfg.Ignore.Paths[0])
		}
	})

	t.Run("prefers ainspector.yaml over ainspector.yml", func(t *testing.T) {
		tmpDir := t.TempDir()
		_ = os.Chdir(tmpDir)

		_ = os.WriteFile("ainspector.yaml", []byte("ignore:\n  paths:\n    - from_yaml/\n"), 0644)
		_ = os.WriteFile("ainspector.yml", []byte("ignore:\n  paths:\n    - from_yml/\n"), 0644)

		cfg, err := Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Ignore.Paths[0] != "from_yaml/" {
			t.Errorf("expected from_yaml/, got %s", cfg.Ignore.Paths[0])
		}
	})
}

func TestLoadFromPath(t *testing.T) {
	t.Run("loads from specific path", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "custom-config.yaml")

		content := `ignore:
  paths:
    - custom/
`
		_ = os.WriteFile(configPath, []byte(content), 0644)

		cfg, err := LoadFromPath(configPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Ignore.Paths[0] != "custom/" {
			t.Errorf("expected custom/, got %s", cfg.Ignore.Paths[0])
		}
	})

	t.Run("returns empty config for non-existent path", func(t *testing.T) {
		cfg, err := LoadFromPath("/nonexistent/path.yaml")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cfg.Ignore.Paths) != 0 {
			t.Errorf("expected empty paths")
		}
	})
}

func TestShouldIgnore(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		path     string
		expected bool
	}{
		// Directory patterns
		{
			name: "matches directory pattern",
			config: Config{Ignore: IgnoreConfig{
				Paths: []string{"vendor/"},
			}},
			path:     "vendor/github.com/pkg/errors/errors.go",
			expected: true,
		},
		{
			name: "matches nested directory",
			config: Config{Ignore: IgnoreConfig{
				Paths: []string{"node_modules/"},
			}},
			path:     "frontend/node_modules/lodash/index.js",
			expected: false, // node_modules/ only matches at the start
		},

		// Glob patterns
		{
			name: "matches simple glob",
			config: Config{Ignore: IgnoreConfig{
				Paths: []string{"*.test.go"},
			}},
			path:     "pkg/utils/helper_test.go",
			expected: false, // *.test.go doesn't match _test.go
		},
		{
			name: "matches test files with correct pattern",
			config: Config{Ignore: IgnoreConfig{
				Paths: []string{"*_test.go"},
			}},
			path:     "pkg/utils/helper_test.go",
			expected: true,
		},
		{
			name: "matches doublestar pattern",
			config: Config{Ignore: IgnoreConfig{
				Paths: []string{"**/*_test.go"},
			}},
			path:     "internal/pkg/utils/helper_test.go",
			expected: true,
		},
		{
			name: "matches generated files",
			config: Config{Ignore: IgnoreConfig{
				Paths: []string{"*.generated.go"},
			}},
			path:     "api/models.generated.go",
			expected: true,
		},

		// Non-matching cases
		{
			name: "does not match unrelated files",
			config: Config{Ignore: IgnoreConfig{
				Paths: []string{"vendor/", "*.test.go"},
			}},
			path:     "internal/service/handler.go",
			expected: false,
		},

		// Empty config
		{
			name:     "empty config ignores nothing",
			config:   Config{},
			path:     "any/file/path.go",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.ShouldIgnore(tt.path)
			if result != tt.expected {
				t.Errorf("ShouldIgnore(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}
