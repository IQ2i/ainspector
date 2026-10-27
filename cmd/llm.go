package cmd

import (
	"fmt"
	"os"

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

	return openai.New(apiURL, apiKey, model), nil
}
