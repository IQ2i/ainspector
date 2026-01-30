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

	projectContext, err := GenerateProjectContext(ctx, client, tmpDir, []string{"javascript"})
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
}

func TestGenerateProjectContext_NoFiles(t *testing.T) {
	tmpDir := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not call LLM when no files are found")
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", "gpt-4")
	ctx := context.Background()

	projectContext, err := GenerateProjectContext(ctx, client, tmpDir, []string{"go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if projectContext.Description != "" {
		t.Error("expected empty description when no files found")
	}

	if len(projectContext.Languages) != 1 || projectContext.Languages[0] != "go" {
		t.Errorf("expected languages [go], got %v", projectContext.Languages)
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
	projectContext, err := GenerateProjectContext(ctx, client, tmpDir, []string{"go"})
	if err != nil {
		t.Fatalf("should not return error on LLM failure: %v", err)
	}

	if projectContext.Description != "" {
		t.Error("expected empty description on LLM failure")
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

	prompt := buildSystemPrompt("javascript", projectContext)

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
	prompt := buildSystemPrompt("go", nil)

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

	prompt := buildSystemPrompt("go", projectContext)

	// Should NOT contain project context section when description is empty
	if strings.Contains(prompt, "PROJECT CONTEXT") {
		t.Error("should not contain PROJECT CONTEXT when description is empty")
	}
}
