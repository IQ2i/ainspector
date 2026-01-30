package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the ainspector configuration
type Config struct {
	Ignore  IgnoreConfig  `yaml:"ignore"`
	Context ContextConfig `yaml:"context"`
	Rules   []string      `yaml:"rules"`
}

// IgnoreConfig holds patterns for files to ignore during review
type IgnoreConfig struct {
	// Paths contains glob patterns (supports ** for recursive matching)
	Paths []string `yaml:"paths"`
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

		var cfg Config
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, err
		}

		return &cfg, nil
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

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
