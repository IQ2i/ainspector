package config

import (
	"os"
	"path/filepath"
	"strings"
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
			return
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

	t.Run("loads rules from config", func(t *testing.T) {
		tmpDir := t.TempDir()
		_ = os.Chdir(tmpDir)

		content := `rules:
  - Exceptions must not be used for flow control
  - All database access must go through repositories
  - console.log must not be used in production code
`
		_ = os.WriteFile("ainspector.yaml", []byte(content), 0644)

		cfg, err := Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cfg.Rules) != 3 {
			t.Errorf("expected 3 rules, got %d", len(cfg.Rules))
		}
		if cfg.Rules[0] != "Exceptions must not be used for flow control" {
			t.Errorf("unexpected first rule: %s", cfg.Rules[0])
		}
	})

	t.Run("handles missing rules section", func(t *testing.T) {
		tmpDir := t.TempDir()
		_ = os.Chdir(tmpDir)

		content := `ignore:
  paths:
    - vendor/
`
		_ = os.WriteFile("ainspector.yaml", []byte(content), 0644)

		cfg, err := Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cfg.Rules) != 0 {
			t.Errorf("expected 0 rules, got %d", len(cfg.Rules))
		}
	})

	t.Run("handles empty rules list", func(t *testing.T) {
		tmpDir := t.TempDir()
		_ = os.Chdir(tmpDir)

		content := `rules: []
`
		_ = os.WriteFile("ainspector.yaml", []byte(content), 0644)

		cfg, err := Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cfg.Rules) != 0 {
			t.Errorf("expected 0 rules, got %d", len(cfg.Rules))
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

func TestParse_LegacyConfig(t *testing.T) {
	cfg, err := parse("ainspector.yaml", []byte(`ignore:
  paths:
    - vendor/
context:
  include:
    - README.md
rules:
  - Keep functions small
`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := cfg.QualityRules(); len(got) != 1 || got[0] != "Keep functions small" {
		t.Errorf("top-level rules must still apply to quality, got %v", got)
	}
	if !cfg.QualityEnabled() || len(cfg.EnabledModes()) != 1 || cfg.EnabledModes()[0] != "quality" {
		t.Errorf("quality must be enabled by default, got %v", cfg.EnabledModes())
	}
	if cfg.HasChangeFilters() {
		t.Error("a legacy config has no change filters")
	}
	if ignored, _ := cfg.ShouldIgnoreChange("renovate-bot", "chore", []string{"x"}); ignored {
		t.Error("nothing is ignored without filters")
	}
}

func TestParse_FullConfig(t *testing.T) {
	cfg, err := parse("ainspector.yaml", []byte(`ignore:
  paths: [vendor/, node_modules/]
  authors: [renovate-bot, dependabot]
  title_patterns: ["^\\[skip-review\\]"]
  labels: [no-ai-review]
context:
  include: [README.md]
  exclude: [docs/private/]
  max_bytes: 32768
rules:
  - Top level rule
llm:
  provider: anthropic
  max_tokens_per_run: 400000
modes:
  quality:
    enabled: true
    scope: function
    model: claude-sonnet-5-5
    rules:
      - Mode level rule
  security:
    enabled: false
    scope: function+class+related
    max_findings: 5
publish:
  code_quality: gl-code-quality-report.json
`))
	if err != nil {
		t.Fatalf("sections of later phases must not break loading: %v", err)
	}

	if got := cfg.QualityRules(); len(got) != 2 || got[0] != "Top level rule" || got[1] != "Mode level rule" {
		t.Errorf("expected top-level then mode rules, got %v", got)
	}
	if cfg.Modes.Quality.Model != "claude-sonnet-5-5" {
		t.Errorf("unexpected model %q", cfg.Modes.Quality.Model)
	}
	if len(cfg.Ignore.Authors) != 2 || len(cfg.Ignore.Labels) != 1 || len(cfg.Ignore.TitlePatterns) != 1 || len(cfg.Ignore.Paths) != 2 {
		t.Errorf("unexpected ignore section: %+v", cfg.Ignore)
	}
	if !cfg.HasChangeFilters() {
		t.Error("change filters expected")
	}
}

func TestParse_QualityDisabled(t *testing.T) {
	cfg, err := parse("ainspector.yaml", []byte("modes:\n  quality:\n    enabled: false\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.QualityEnabled() || len(cfg.EnabledModes()) != 0 {
		t.Errorf("quality must be disabled, got %v", cfg.EnabledModes())
	}
}

func TestParse_InvalidTitlePattern(t *testing.T) {
	_, err := parse("ainspector.yaml", []byte("ignore:\n  title_patterns:\n    - \"([\"\n"))
	if err == nil || !strings.Contains(err.Error(), "title_patterns") {
		t.Errorf("expected an error naming title_patterns, got %v", err)
	}
}

func TestLoad_InvalidTitlePattern(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ainspector.yaml")
	_ = os.WriteFile(path, []byte("ignore:\n  title_patterns: [\"(\"]\n"), 0644)

	if _, err := LoadFromPath(path); err == nil {
		t.Error("LoadFromPath must reject an invalid pattern")
	}
}

func TestShouldIgnoreChange(t *testing.T) {
	cfg := &Config{Ignore: IgnoreConfig{
		Authors:       []string{"Renovate-Bot"},
		TitlePatterns: []string{`^\[skip-review\]`, `(`},
		Labels:        []string{"no-ai-review"},
	}}

	tests := []struct {
		name          string
		author, title string
		labels        []string
		want          bool
	}{
		{"author, other case", "renovate-bot", "Update", nil, true},
		{"title matches", "alice", "[skip-review] wip", nil, true},
		{"title in the middle does not match", "alice", "fix [skip-review]", nil, false},
		{"label, other case", "alice", "Fix", []string{"bug", "No-AI-Review"}, true},
		{"similar author", "renovate-bot2", "Fix", nil, false},
		{"nothing matches", "alice", "Fix", []string{"bug"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := cfg.ShouldIgnoreChange(tt.author, tt.title, tt.labels)
			if got != tt.want {
				t.Errorf("ShouldIgnoreChange = %v, want %v", got, tt.want)
			}
			if got && reason == "" {
				t.Error("a reason is expected when a change is ignored")
			}
		})
	}
}
