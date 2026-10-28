package cmd

import (
	"strings"
	"testing"

	"github.com/iq2i/ainspector/internal/config"
	"github.com/iq2i/ainspector/internal/llm/anthropic"
	"github.com/iq2i/ainspector/internal/llm/openai"
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
		{"model name with a newline", "", map[string]string{"LLM_API_KEY": "b", "LLM_MODEL": "gpt\n/close"}, "", "LLM_MODEL"},
		{"model name with spaces", "", map[string]string{"LLM_API_KEY": "b", "LLM_MODEL": "my model"}, "", "LLM_MODEL"},
		{"local model name", "", map[string]string{"LLM_API_KEY": "b", "LLM_MODEL": "llama3.1:8b"}, "openai", ""},
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
			var got string
			switch client.(type) {
			case *anthropic.Client:
				got = "anthropic"
			case *openai.Client:
				got = "openai"
			}
			if got != tt.backend {
				t.Errorf("expected backend %s, got %s", tt.backend, got)
			}
		})
	}
}

func TestTokenCapFromEnv(t *testing.T) {
	tests := []struct {
		value   string
		want    int
		wantErr bool
	}{
		{"", 0, false},
		{"150000", 150000, false},
		{"0", 0, true},
		{"-5", 0, true},
		{"lots", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			t.Setenv("AINSPECTOR_MAX_TOKENS_PER_RUN", tt.value)
			got, err := tokenCapFromEnv()
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("got %d, %v", got, err)
			}
		})
	}
}

func TestCredentials(t *testing.T) {
	for _, k := range []string{"ANTHROPIC_API_KEY", "LLM_API_KEY", "GITLAB_TOKEN", "GITHUB_TOKEN"} {
		t.Setenv(k, "")
	}
	t.Setenv("GITLAB_TOKEN", "glpat-abcdefgh")
	t.Setenv("LLM_API_KEY", "sk-abcdefgh")
	got := credentials()
	if len(got) != 2 || got[0] != "sk-abcdefgh" || got[1] != "glpat-abcdefgh" {
		t.Errorf("got %v", got)
	}
}

func TestDefaultFPModel(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	if got := defaultFPModel(config.LLMConfig{}); got != "" {
		t.Errorf("the openai-compatible backend keeps its default model, got %q", got)
	}
	if got := defaultFPModel(config.LLMConfig{Provider: "anthropic"}); got != "claude-opus-5-5" {
		t.Errorf("the anthropic backend judges with the stronger model, got %q", got)
	}
	t.Setenv("ANTHROPIC_API_KEY", "k")
	if got := defaultFPModel(config.LLMConfig{}); got != "claude-opus-5-5" {
		t.Errorf("the backend is guessed from the key, got %q", got)
	}
	if got := defaultFPModel(config.LLMConfig{Provider: "openai"}); got != "" {
		t.Errorf("an explicit provider wins over the key, got %q", got)
	}
}
