package cmd

import (
	"strings"
	"testing"

	"github.com/iq2i/ainspector/internal/config"
)

func TestNewLLMClient(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		env      map[string]string
		backend  string
		errPart  string
	}{
		{"anthropic key picks anthropic", "", map[string]string{"ANTHROPIC_API_KEY": "a", "LLM_API_KEY": "b"}, "anthropic", ""},
		{"llm key alone picks openai", "", map[string]string{"LLM_API_KEY": "b"}, "openai", ""},
		{"explicit openai wins over anthropic key", "openai", map[string]string{"ANTHROPIC_API_KEY": "a", "LLM_API_KEY": "b"}, "openai", ""},
		{"explicit anthropic", "anthropic", map[string]string{"ANTHROPIC_API_KEY": "a"}, "anthropic", ""},
		{"explicit anthropic without key", "anthropic", map[string]string{"LLM_API_KEY": "b"}, "", "ANTHROPIC_API_KEY"},
		{"no key at all", "", nil, "", "LLM_API_KEY"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, k := range []string{"ANTHROPIC_API_KEY", "LLM_API_KEY", "LLM_BASE_URL", "LLM_MODEL"} {
				t.Setenv(k, "")
			}
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			client, err := newLLMClient(config.LLMConfig{Provider: tt.provider})
			if tt.errPart != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errPart) {
					t.Fatalf("expected an error mentioning %s, got %v", tt.errPart, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if client.Backend() != tt.backend {
				t.Errorf("expected backend %s, got %s", tt.backend, client.Backend())
			}
		})
	}
}
