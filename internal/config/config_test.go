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
	if cfg.SecurityEnabled() || cfg.Modes.Security.Scope != "function+class+related" || cfg.Modes.Security.Findings() != 5 {
		t.Errorf("unexpected security section: %+v", cfg.Modes.Security)
	}
}

func TestParse_SecurityDefaults(t *testing.T) {
	cfg, err := parse("ainspector.yaml", []byte(""))
	if err != nil {
		t.Fatal(err)
	}
	sec := cfg.Modes.Security
	if cfg.SecurityEnabled() || sec.ScopeName() != "function+class+related" || sec.Findings() != 5 {
		t.Errorf("unexpected defaults: %+v", sec)
	}
	if r := sec.Related.Resolved(); len(r.Presets) != 2 || r.MaxFiles != 6 || r.MaxBytes != 40000 {
		t.Errorf("unexpected related defaults: %+v", r)
	}
	h := sec.Hunks.Resolved()
	if h.Before != 5 || h.After != 1 || len(h.Extensions) != 5 || h.Extensions[0] != "twig" {
		t.Errorf("unexpected hunk defaults: %+v", h)
	}
	if got := cfg.EnabledModes(); len(got) != 1 || got[0] != "quality" {
		t.Errorf("only quality runs by default, got %v", got)
	}
}

func TestParse_SecuritySettings(t *testing.T) {
	cfg, err := parse("ainspector.yaml", []byte(`modes:
  security:
    enabled: true
    scope: function
    model: some-model
    max_findings: 3
    hunks: {extensions: [twig], before: 2, after: 4}
    false_positive: {model: big-model, instructions: "Ignore legacy/"}
`))
	if err != nil {
		t.Fatal(err)
	}
	sec := cfg.Modes.Security
	if !cfg.SecurityEnabled() || sec.ScopeName() != "function" || sec.Model != "some-model" || sec.Findings() != 3 {
		t.Errorf("unexpected security section: %+v", sec)
	}
	if h := sec.Hunks.Resolved(); h.Before != 2 || h.After != 4 || len(h.Extensions) != 1 {
		t.Errorf("unexpected hunks: %+v", h)
	}
	if sec.FalsePositive.Model != "big-model" || sec.FalsePositive.Instructions != "Ignore legacy/" {
		t.Errorf("unexpected false positive section: %+v", sec.FalsePositive)
	}
	if got := cfg.EnabledModes(); len(got) != 2 || got[1] != "security" {
		t.Errorf("security must run when enabled, got %v", got)
	}
}

func TestParse_InvalidSecuritySettings(t *testing.T) {
	for name, yaml := range map[string]string{
		"scope":        "modes:\n  security:\n    scope: everything\n",
		"max findings": "modes:\n  security:\n    max_findings: -1\n",
		"hunk before":  "modes:\n  security:\n    hunks: {before: -1}\n",
		"hunk after":   "modes:\n  security:\n    hunks: {after: -1}\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parse("ainspector.yaml", []byte(yaml)); err == nil {
				t.Errorf("expected an error for %q", yaml)
			}
		})
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

func TestParse_LLMAndContextLimits(t *testing.T) {
	cfg, err := parse("ainspector.yaml", []byte(`context:
  max_bytes: 1000
llm:
  provider: openai
  max_tokens_per_run: 5000
  prices:
    my-model: {input: 1.5, output: 6}
`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Context.Limit() != 1000 || cfg.LLM.TokenBudget() != 5000 || cfg.LLM.Provider != "openai" {
		t.Errorf("unexpected values: %+v / %+v", cfg.Context, cfg.LLM)
	}
	if p := cfg.LLM.Prices["my-model"]; p.Input != 1.5 || p.Output != 6 {
		t.Errorf("unexpected price %+v", p)
	}
}

func TestParse_LLMDefaults(t *testing.T) {
	cfg, err := parse("ainspector.yaml", []byte(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Context.Limit() != DefaultContextMaxBytes || cfg.LLM.TokenBudget() != DefaultMaxTokensPerRun {
		t.Errorf("unexpected defaults: %d / %d", cfg.Context.Limit(), cfg.LLM.TokenBudget())
	}
}

func TestParse_InvalidLLMSettings(t *testing.T) {
	for name, yaml := range map[string]string{
		"provider":        "llm:\n  provider: gemini\n",
		"negative tokens": "llm:\n  max_tokens_per_run: -1\n",
		"negative bytes":  "context:\n  max_bytes: -1\n",
		"negative price":  "llm:\n  prices:\n    m: {input: -1}\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parse("ainspector.yaml", []byte(yaml)); err == nil {
				t.Errorf("expected an error for %q", yaml)
			}
		})
	}
}

func TestParse_SecurityRelated(t *testing.T) {
	cfg, err := parse("ainspector.yaml", []byte(`modes:
  security:
    related: {presets: [symfony], max_files: 3, max_bytes: 1000}
`))
	if err != nil {
		t.Fatal(err)
	}
	r := cfg.Modes.Security.Related.Resolved()
	if len(r.Presets) != 1 || r.Presets[0] != "symfony" || r.MaxFiles != 3 || r.MaxBytes != 1000 {
		t.Errorf("unexpected related settings: %+v", r)
	}
}

func TestParse_SecurityRelatedInvalid(t *testing.T) {
	for name, doc := range map[string]string{
		"unknown preset":     "modes:\n  security:\n    related: {presets: [laravel]}\n",
		"negative max files": "modes:\n  security:\n    related: {max_files: -1}\n",
		"negative max bytes": "modes:\n  security:\n    related: {max_bytes: -1}\n",
	} {
		if _, err := parse("ainspector.yaml", []byte(doc)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestCheckOutputPath(t *testing.T) {
	for _, ok := range []string{"report.json", "reports/gl-code-quality.json", "./report.json", "a..b/c.json"} {
		if err := CheckOutputPath(ok); err != nil {
			t.Errorf("%q should be accepted: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "/etc/passwd", `\evil`, "../x.json", "a/../../x.json", `a\..\x.json`, "a/.."} {
		if err := CheckOutputPath(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}
