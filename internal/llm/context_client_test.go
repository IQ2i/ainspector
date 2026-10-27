package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iq2i/ainspector/internal/config"
	"github.com/iq2i/ainspector/internal/llm"
	"github.com/iq2i/ainspector/internal/llm/openai"
)

func TestGenerateProjectContext_WithFiles(t *testing.T) {
	// Create a temporary project directory
	tmpDir := t.TempDir()

	// Create sample files
	packageJSON := `{
  "name": "test-project",
  "dependencies": {
    "react": "^18.0.0"
  }
}`
	if err := os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(packageJSON), 0644); err != nil {
		t.Fatal(err)
	}

	readme := "# Test Project\nA simple React application"
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte(readme), 0644); err != nil {
		t.Fatal(err)
	}

	// Mock LLM server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openai.ChatResponse{
			Choices: []struct {
				Message openai.ChatMessage `json:"message"`
			}{
				{Message: openai.ChatMessage{
					Role:    "assistant",
					Content: "A React web application using modern JavaScript",
				}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := openai.New(server.URL, "test-key", "gpt-4")
	ctx := context.Background()

	projectContext, err := llm.GenerateProjectContext(ctx, client, tmpDir, []string{"javascript"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if projectContext.Description == "" {
		t.Error("expected non-empty description")
	}

	if !strings.Contains(projectContext.Description, "React") {
		t.Error("expected description to mention React")
	}

	if len(projectContext.Languages) != 1 || projectContext.Languages[0] != "javascript" {
		t.Errorf("expected languages [javascript], got %v", projectContext.Languages)
	}

	if projectContext.IsRaw {
		t.Error("expected IsRaw to be false for legacy mode")
	}
}

func TestGenerateProjectContext_NoFiles(t *testing.T) {
	tmpDir := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not call LLM when no files are found")
	}))
	defer server.Close()

	client := openai.New(server.URL, "test-key", "gpt-4")
	ctx := context.Background()

	projectContext, err := llm.GenerateProjectContext(ctx, client, tmpDir, []string{"go"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if projectContext.Description != "" {
		t.Error("expected empty description when no files found")
	}

	if len(projectContext.Languages) != 1 || projectContext.Languages[0] != "go" {
		t.Errorf("expected languages [go], got %v", projectContext.Languages)
	}

	if projectContext.IsRaw {
		t.Error("expected IsRaw to be false for legacy mode")
	}
}

func TestGenerateProjectContext_LLMFailure(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file to trigger LLM call
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	// Mock failing LLM server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := openai.New(server.URL, "test-key", "gpt-4")
	ctx := context.Background()

	// Should not return error, just empty context
	projectContext, err := llm.GenerateProjectContext(ctx, client, tmpDir, []string{"go"}, nil)
	if err != nil {
		t.Fatalf("should not return error on LLM failure: %v", err)
	}

	if projectContext.Description != "" {
		t.Error("expected empty description on LLM failure")
	}

	if projectContext.IsRaw {
		t.Error("expected IsRaw to be false for legacy mode")
	}
}

func TestGenerateProjectContext_WithConfig(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Test Project"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(tmpDir, "docs"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "docs/guide.md"), []byte("# Guide"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create config with include patterns
	contextConfig := &config.ContextConfig{
		Include: []string{"README.md", "docs/guide.md"},
	}

	// Mock LLM server (should NOT be called)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("LLM should not be called when using config-based context")
	}))
	defer server.Close()

	client := openai.New(server.URL, "test-key", "gpt-4")
	ctx := context.Background()

	projectContext, err := llm.GenerateProjectContext(ctx, client, tmpDir, []string{"go"}, contextConfig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if projectContext.Description == "" {
		t.Error("expected non-empty description")
	}

	if !projectContext.IsRaw {
		t.Error("expected IsRaw to be true for config-based context")
	}

	// Should contain both files
	if !strings.Contains(projectContext.Description, "=== README.md ===") {
		t.Error("expected description to contain README.md")
	}

	if !strings.Contains(projectContext.Description, "=== docs/guide.md ===") {
		t.Error("expected description to contain docs/guide.md")
	}

	if !strings.Contains(projectContext.Description, "# Test Project") {
		t.Error("expected description to contain README content")
	}
}

func TestGenerateProjectContext_BackwardCompatibility(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file that would be picked up by legacy mode
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Test"), 0644); err != nil {
		t.Fatal(err)
	}

	// Mock LLM server (should be called in legacy mode)
	llmCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmCalled = true
		resp := openai.ChatResponse{
			Choices: []struct {
				Message openai.ChatMessage `json:"message"`
			}{
				{Message: openai.ChatMessage{
					Role:    "assistant",
					Content: "A test project",
				}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := openai.New(server.URL, "test-key", "gpt-4")
	ctx := context.Background()

	tests := []struct {
		name          string
		contextConfig *config.ContextConfig
		expectLLMCall bool
		expectIsRaw   bool
	}{
		{
			name:          "nil config - use legacy",
			contextConfig: nil,
			expectLLMCall: true,
			expectIsRaw:   false,
		},
		{
			name:          "empty config - use legacy",
			contextConfig: &config.ContextConfig{},
			expectLLMCall: true,
			expectIsRaw:   false,
		},
		{
			name: "config with patterns - no LLM",
			contextConfig: &config.ContextConfig{
				Include: []string{"README.md"},
			},
			expectLLMCall: false,
			expectIsRaw:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			llmCalled = false

			projectContext, err := llm.GenerateProjectContext(ctx, client, tmpDir, []string{"go"}, tt.contextConfig)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if llmCalled != tt.expectLLMCall {
				t.Errorf("LLM called = %v, want %v", llmCalled, tt.expectLLMCall)
			}

			if projectContext.IsRaw != tt.expectIsRaw {
				t.Errorf("IsRaw = %v, want %v", projectContext.IsRaw, tt.expectIsRaw)
			}
		})
	}
}
