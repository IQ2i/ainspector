package cmd

import (
	"fmt"
	"os"
	"strconv"

	"github.com/iq2i/ainspector/internal/config"
	"github.com/iq2i/ainspector/internal/llm"
	"github.com/iq2i/ainspector/internal/llm/anthropic"
	"github.com/iq2i/ainspector/internal/llm/openai"
)

const defaultAnthropicModel = "claude-sonnet-5-5"

// newLLMClient builds the backend named by llm.provider, or guesses it from the environment
func newLLMClient(cfg config.LLMConfig) (llm.Client, error) {
	provider := cfg.Provider
	if provider == "" {
		provider = "openai"
		if os.Getenv("ANTHROPIC_API_KEY") != "" {
			provider = "anthropic"
		}
	}

	if provider == "anthropic" {
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable is required")
		}
		return anthropic.New(apiKey, defaultAnthropicModel), nil
	}

	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("LLM_API_KEY environment variable is required")
	}

	apiURL := os.Getenv("LLM_BASE_URL")
	if apiURL == "" {
		apiURL = "https://api.openai.com"
	}

	model := os.Getenv("LLM_MODEL")
	if model == "" {
		model = "gpt-4o"
	}
	if err := config.CheckModelName(model); err != nil {
		return nil, fmt.Errorf("LLM_MODEL: %w", err)
	}

	return openai.New(apiURL, apiKey, model), nil
}

// tokenCapFromEnv reads AINSPECTOR_MAX_TOKENS_PER_RUN, a cap that ainspector.yaml can only lower:
// the file comes from the change under review. Zero means no cap.
func tokenCapFromEnv() (int, error) {
	raw := os.Getenv("AINSPECTOR_MAX_TOKENS_PER_RUN")
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("AINSPECTOR_MAX_TOKENS_PER_RUN must be a positive integer, got %q", raw)
	}
	return n, nil
}

// credentials lists the secrets of the environment, to keep them out of the logs
func credentials() []string {
	var secrets []string
	for _, name := range []string{"ANTHROPIC_API_KEY", "LLM_API_KEY", "GITLAB_TOKEN", "GITHUB_TOKEN"} {
		if v := os.Getenv(name); v != "" {
			secrets = append(secrets, v)
		}
	}
	return secrets
}
