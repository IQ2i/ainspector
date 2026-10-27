package config

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config represents the ainspector configuration
type Config struct {
	Ignore  IgnoreConfig  `yaml:"ignore"`
	Context ContextConfig `yaml:"context"`
	// Rules applies to the quality mode, before modes.quality.rules
	Rules []string    `yaml:"rules"`
	LLM   LLMConfig   `yaml:"llm"`
	Modes ModesConfig `yaml:"modes"`
}

const (
	// DefaultMaxTokensPerRun caps the tokens a run may consume, all modes together
	DefaultMaxTokensPerRun = 400000
	// DefaultContextMaxBytes caps the size of the project context sent with every request
	DefaultContextMaxBytes = 32768
)

// LLMConfig holds the settings of the LLM backend
type LLMConfig struct {
	// Provider is "anthropic" or "openai". Empty picks anthropic when ANTHROPIC_API_KEY is set.
	Provider string `yaml:"provider"`
	// MaxTokensPerRun caps the tokens consumed by a run, defaults to DefaultMaxTokensPerRun
	MaxTokensPerRun int `yaml:"max_tokens_per_run"`
	// Prices overrides the built-in price table, by model name
	Prices map[string]Price `yaml:"prices"`
}

// Price is the cost of a model in USD per million tokens. CacheRead and CacheWrite default
// to the usual multipliers of Input when left at zero.
type Price struct {
	Input      float64 `yaml:"input"`
	Output     float64 `yaml:"output"`
	CacheRead  float64 `yaml:"cache_read"`
	CacheWrite float64 `yaml:"cache_write"`
}

// TokenBudget returns the token cap of a run
func (c LLMConfig) TokenBudget() int {
	if c.MaxTokensPerRun > 0 {
		return c.MaxTokensPerRun
	}
	return DefaultMaxTokensPerRun
}

// IgnoreConfig holds what to ignore during review
type IgnoreConfig struct {
	// Paths contains glob patterns (supports ** for recursive matching)
	Paths []string `yaml:"paths"`
	// Authors skips PRs/MRs opened by these users (case-insensitive), e.g. dependency bots
	Authors []string `yaml:"authors"`
	// TitlePatterns skips PRs/MRs whose title matches one of these regular expressions
	TitlePatterns []string `yaml:"title_patterns"`
	// Labels skips PRs/MRs carrying one of these labels (case-insensitive)
	Labels []string `yaml:"labels"`
}

// ModesConfig holds the settings of each review mode
type ModesConfig struct {
	Quality  QualityConfig  `yaml:"quality"`
	Security SecurityConfig `yaml:"security"`
}

const (
	// DefaultSecurityScope is the context given to the security mode: the function, its class
	// and the files related to it
	DefaultSecurityScope       = "function+class+related"
	DefaultSecurityMaxFindings = 5
	DefaultHunkBefore          = 5
	DefaultHunkAfter           = 1
	DefaultRelatedMaxFiles     = 6
	DefaultRelatedMaxBytes     = 40000
)

// KnownRelatedPresets are the resolvers of related files that can be named in the configuration
var KnownRelatedPresets = []string{"symfony", "vue"}

// DefaultHunkExtensions are the files without functions that the security mode reviews as hunks
var DefaultHunkExtensions = []string{"twig", "yaml", "yml", "vue", "env"}

// SecurityConfig holds the settings of the security mode
type SecurityConfig struct {
	// Enabled defaults to false
	Enabled *bool `yaml:"enabled"`
	// Scope is "function", "function+class" or "function+class+related" (the default)
	Scope string `yaml:"scope"`
	Model string `yaml:"model"`
	// MaxFindings caps the findings published per run, defaults to DefaultSecurityMaxFindings
	MaxFindings   int                 `yaml:"max_findings"`
	Hunks         HunksConfig         `yaml:"hunks"`
	Related       RelatedConfig       `yaml:"related"`
	FalsePositive FalsePositiveConfig `yaml:"false_positive"`
}

// RelatedConfig tells which files related to a unit are given with it, with the scope
// function+class+related. Zero values select the defaults.
type RelatedConfig struct {
	// Presets defaults to all of KnownRelatedPresets
	Presets []string `yaml:"presets"`
	// MaxFiles and MaxBytes cap the related files of one unit
	MaxFiles int `yaml:"max_files"`
	MaxBytes int `yaml:"max_bytes"`
}

// HunksConfig tells how files without functions are reviewed: the changed lines with a few
// lines of context around them. Zero values select the defaults.
type HunksConfig struct {
	Extensions []string `yaml:"extensions"`
	Before     int      `yaml:"before"`
	After      int      `yaml:"after"`
}

// FalsePositiveConfig configures the pass that discards the false positives of a mode
type FalsePositiveConfig struct {
	// Model defaults to claude-opus-5-5 on the Anthropic backend, and to the default model otherwise
	Model string `yaml:"model"`
	// Instructions are appended to the built-in ones
	Instructions string `yaml:"instructions"`
}

// QualityConfig holds the settings of the quality mode
type QualityConfig struct {
	// Enabled defaults to true
	Enabled *bool    `yaml:"enabled"`
	Model   string   `yaml:"model"`
	Rules   []string `yaml:"rules"`
}

// ContextConfig holds patterns for files to include in project context
type ContextConfig struct {
	// Include contains glob patterns for files/folders to include in context
	Include []string `yaml:"include"`
	// Exclude contains glob patterns to exclude (takes priority over Include)
	Exclude []string `yaml:"exclude"`
	// MaxBytes caps the project context, defaults to DefaultContextMaxBytes
	MaxBytes int `yaml:"max_bytes"`
}

// Limit returns the size cap of the project context in bytes
func (c ContextConfig) Limit() int {
	if c.MaxBytes > 0 {
		return c.MaxBytes
	}
	return DefaultContextMaxBytes
}

// configFileNames lists the supported configuration file names in order of priority
var configFileNames = []string{"ainspector.yaml", "ainspector.yml"}

// Load reads the configuration from ainspector.yaml or ainspector.yml
// Returns an empty config (not an error) if no config file exists
func Load() (*Config, error) {
	for _, filename := range configFileNames {
		data, err := os.ReadFile(filename)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}

		return parse(filename, data)
	}

	// No config file found, return empty config
	return &Config{}, nil
}

// LoadFromPath reads the configuration from a specific path
func LoadFromPath(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, err
	}

	return parse(path, data)
}

func parse(source string, data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	for _, pattern := range cfg.Ignore.TitlePatterns {
		if _, err := regexp.Compile(pattern); err != nil {
			return nil, fmt.Errorf("%s: invalid ignore.title_patterns entry %q: %w", source, pattern, err)
		}
	}

	switch cfg.LLM.Provider {
	case "", "anthropic", "openai":
	default:
		return nil, fmt.Errorf("%s: invalid llm.provider %q (expected anthropic or openai)", source, cfg.LLM.Provider)
	}
	switch cfg.Modes.Security.Scope {
	case "", "function", "function+class", "function+class+related":
	default:
		return nil, fmt.Errorf("%s: invalid modes.security.scope %q (expected function, function+class or function+class+related)", source, cfg.Modes.Security.Scope)
	}
	if sec := cfg.Modes.Security; sec.MaxFindings < 0 || sec.Hunks.Before < 0 || sec.Hunks.After < 0 {
		return nil, fmt.Errorf("%s: modes.security.max_findings and modes.security.hunks must not be negative", source)
	}
	rel := cfg.Modes.Security.Related
	if rel.MaxFiles < 0 || rel.MaxBytes < 0 {
		return nil, fmt.Errorf("%s: modes.security.related.max_files and max_bytes must not be negative", source)
	}
	for _, preset := range rel.Presets {
		if !slices.Contains(KnownRelatedPresets, preset) {
			return nil, fmt.Errorf("%s: unknown modes.security.related.presets entry %q (expected one of %s)", source, preset, strings.Join(KnownRelatedPresets, ", "))
		}
	}
	if cfg.LLM.MaxTokensPerRun < 0 || cfg.Context.MaxBytes < 0 {
		return nil, fmt.Errorf("%s: llm.max_tokens_per_run and context.max_bytes must not be negative", source)
	}
	for model, p := range cfg.LLM.Prices {
		if p.Input < 0 || p.Output < 0 || p.CacheRead < 0 || p.CacheWrite < 0 {
			return nil, fmt.Errorf("%s: llm.prices.%s must not be negative", source, model)
		}
	}

	return &cfg, nil
}

// QualityEnabled tells whether the quality mode runs by default
func (c *Config) QualityEnabled() bool {
	return c.Modes.Quality.Enabled == nil || *c.Modes.Quality.Enabled
}

// QualityRules returns the project rules of the quality mode
func (c *Config) QualityRules() []string {
	rules := make([]string, 0, len(c.Rules)+len(c.Modes.Quality.Rules))
	rules = append(rules, c.Rules...)
	return append(rules, c.Modes.Quality.Rules...)
}

// SecurityEnabled tells whether the security mode runs by default
func (c *Config) SecurityEnabled() bool {
	return c.Modes.Security.Enabled != nil && *c.Modes.Security.Enabled
}

// ScopeName returns the scope of the security mode
func (c SecurityConfig) ScopeName() string {
	if c.Scope == "" {
		return DefaultSecurityScope
	}
	return c.Scope
}

// Findings returns the cap of published findings
func (c SecurityConfig) Findings() int {
	if c.MaxFindings > 0 {
		return c.MaxFindings
	}
	return DefaultSecurityMaxFindings
}

// Resolved returns the hunk settings with their defaults applied
func (h HunksConfig) Resolved() HunksConfig {
	if len(h.Extensions) == 0 {
		h.Extensions = DefaultHunkExtensions
	}
	if h.Before == 0 {
		h.Before = DefaultHunkBefore
	}
	if h.After == 0 {
		h.After = DefaultHunkAfter
	}
	return h
}

// Resolved returns the related files settings with their defaults applied
func (r RelatedConfig) Resolved() RelatedConfig {
	if len(r.Presets) == 0 {
		r.Presets = KnownRelatedPresets
	}
	if r.MaxFiles == 0 {
		r.MaxFiles = DefaultRelatedMaxFiles
	}
	if r.MaxBytes == 0 {
		r.MaxBytes = DefaultRelatedMaxBytes
	}
	return r
}

// EnabledModes returns the modes that run when none is requested explicitly
func (c *Config) EnabledModes() []string {
	var names []string
	if c.QualityEnabled() {
		names = append(names, "quality")
	}
	if c.SecurityEnabled() {
		names = append(names, "security")
	}
	return names
}
