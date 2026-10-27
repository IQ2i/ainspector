package config

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

// Config represents the ainspector configuration
type Config struct {
	Ignore  IgnoreConfig  `yaml:"ignore"`
	Context ContextConfig `yaml:"context"`
	// Rules applies to the quality mode, before modes.quality.rules
	Rules []string    `yaml:"rules"`
	Modes ModesConfig `yaml:"modes"`
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
	Quality QualityConfig `yaml:"quality"`
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

// EnabledModes returns the modes that run when none is requested explicitly
func (c *Config) EnabledModes() []string {
	var names []string
	if c.QualityEnabled() {
		names = append(names, "quality")
	}
	return names
}
