package llm

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
		resp := ChatResponse{
			Choices: []struct {
				Message ChatMessage `json:"message"`
			}{
				{Message: ChatMessage{
					Role:    "assistant",
					Content: "A React web application using modern JavaScript",
				}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", "gpt-4")
	ctx := context.Background()

	projectContext, err := GenerateProjectContext(ctx, client, tmpDir, []string{"javascript"}, nil)
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

	client := NewClient(server.URL, "test-key", "gpt-4")
	ctx := context.Background()

	projectContext, err := GenerateProjectContext(ctx, client, tmpDir, []string{"go"}, nil)
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

	client := NewClient(server.URL, "test-key", "gpt-4")
	ctx := context.Background()

	// Should not return error, just empty context
	projectContext, err := GenerateProjectContext(ctx, client, tmpDir, []string{"go"}, nil)
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

func TestFindContextFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create various config files
	files := map[string]string{
		"package.json":   `{"name": "test"}`,
		"README.md":      "# Test",
		"go.mod":         "module test",
		"composer.json":  `{"name": "test"}`,
		"CLAUDE.md":      "Claude instructions",
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	found, err := findContextFiles(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(found) != len(files) {
		t.Errorf("expected %d files, got %d", len(files), len(found))
	}

	for name := range files {
		if _, ok := found[name]; !ok {
			t.Errorf("expected to find %s", name)
		}
	}
}

func TestFindContextFiles_GlobPattern(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a .csproj file (matches *.csproj pattern)
	csprojContent := `<Project Sdk="Microsoft.NET.Sdk"></Project>`
	if err := os.WriteFile(filepath.Join(tmpDir, "MyProject.csproj"), []byte(csprojContent), 0644); err != nil {
		t.Fatal(err)
	}

	found, err := findContextFiles(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should find the .csproj file via glob pattern
	foundCsproj := false
	for file := range found {
		if strings.HasSuffix(file, ".csproj") {
			foundCsproj = true
			break
		}
	}

	if !foundCsproj {
		t.Error("expected to find .csproj file via glob pattern")
	}
}

func TestBuildContextPrompt(t *testing.T) {
	files := map[string]string{
		"README.md":    "# Test Project",
		"package.json": `{"name": "test"}`,
		"CLAUDE.md":    "AI instructions",
	}

	languages := []string{"javascript", "typescript"}

	prompt := buildContextPrompt(files, languages)

	// Check that prompt contains all expected elements
	if !strings.Contains(prompt, "Analyze this project") {
		t.Error("prompt should contain instruction")
	}

	if !strings.Contains(prompt, "javascript") {
		t.Error("prompt should contain detected languages")
	}

	if !strings.Contains(prompt, "README.md") {
		t.Error("prompt should contain README.md")
	}

	if !strings.Contains(prompt, "package.json") {
		t.Error("prompt should contain package.json")
	}

	if !strings.Contains(prompt, "CLAUDE.md") {
		t.Error("prompt should contain CLAUDE.md")
	}
}

func TestBuildContextPrompt_TruncatesLongContent(t *testing.T) {
	// Create a very long file content
	longContent := strings.Repeat("x", 5000)

	files := map[string]string{
		"README.md": longContent,
	}

	prompt := buildContextPrompt(files, []string{"go"})

	// Prompt should be truncated
	if strings.Count(prompt, "x") == 5000 {
		t.Error("expected content to be truncated")
	}

	if !strings.Contains(prompt, "truncated") {
		t.Error("expected truncation marker")
	}
}

func TestBuildSystemPrompt_WithProjectContext(t *testing.T) {
	projectContext := &ProjectContext{
		Description: "A web application using React and TypeScript",
		Languages:   []string{"javascript", "typescript"},
	}

	prompt := buildSystemPrompt("javascript", projectContext, nil)

	// Should contain base prompt
	if !strings.Contains(prompt, "You are an expert code reviewer") {
		t.Error("should contain base prompt")
	}

	// Should contain project context
	if !strings.Contains(prompt, "PROJECT CONTEXT") {
		t.Error("should contain PROJECT CONTEXT header")
	}

	if !strings.Contains(prompt, "React and TypeScript") {
		t.Error("should contain project context description")
	}

	// Should contain language-specific rules
	if !strings.Contains(prompt, "LANGUAGE-SPECIFIC CHECKS FOR JAVASCRIPT") {
		t.Error("should contain language-specific rules")
	}
}

func TestBuildSystemPrompt_WithoutProjectContext(t *testing.T) {
	prompt := buildSystemPrompt("go", nil, nil)

	// Should contain base prompt
	if !strings.Contains(prompt, "You are an expert code reviewer") {
		t.Error("should contain base prompt")
	}

	// Should NOT contain project context
	if strings.Contains(prompt, "PROJECT CONTEXT") {
		t.Error("should not contain PROJECT CONTEXT when context is nil")
	}

	// Should contain language-specific rules
	if !strings.Contains(prompt, "LANGUAGE-SPECIFIC CHECKS FOR GO") {
		t.Error("should contain language-specific rules")
	}
}

func TestBuildSystemPrompt_WithEmptyProjectContext(t *testing.T) {
	projectContext := &ProjectContext{
		Description: "",
		Languages:   []string{"go"},
	}

	prompt := buildSystemPrompt("go", projectContext, nil)

	// Should NOT contain project context section when description is empty
	if strings.Contains(prompt, "PROJECT CONTEXT") {
		t.Error("should not contain PROJECT CONTEXT when description is empty")
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

	client := NewClient(server.URL, "test-key", "gpt-4")
	ctx := context.Background()

	projectContext, err := GenerateProjectContext(ctx, client, tmpDir, []string{"go"}, contextConfig)
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
		resp := ChatResponse{
			Choices: []struct {
				Message ChatMessage `json:"message"`
			}{
				{Message: ChatMessage{
					Role:    "assistant",
					Content: "A test project",
				}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", "gpt-4")
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

			projectContext, err := GenerateProjectContext(ctx, client, tmpDir, []string{"go"}, tt.contextConfig)
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

func TestBuildSystemPrompt_RawVsLegacy(t *testing.T) {
	tests := []struct {
		name            string
		projectContext  *ProjectContext
		expectHeader    string
	}{
		{
			name: "raw context",
			projectContext: &ProjectContext{
				Description: "=== README.md ===\nContent",
				Languages:   []string{"go"},
				IsRaw:       true,
			},
			expectHeader: "=== PROJECT CONTEXT ===",
		},
		{
			name: "legacy context",
			projectContext: &ProjectContext{
				Description: "A Go project",
				Languages:   []string{"go"},
				IsRaw:       false,
			},
			expectHeader: "PROJECT CONTEXT:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := buildSystemPrompt("go", tt.projectContext, nil)

			if !strings.Contains(prompt, tt.expectHeader) {
				t.Errorf("expected prompt to contain %q", tt.expectHeader)
			}

			if !strings.Contains(prompt, tt.projectContext.Description) {
				t.Error("expected prompt to contain description")
			}
		})
	}
}
