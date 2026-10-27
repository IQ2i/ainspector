package llm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindContextFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create various config files
	files := map[string]string{
		"package.json":  `{"name": "test"}`,
		"README.md":     "# Test",
		"go.mod":        "module test",
		"composer.json": `{"name": "test"}`,
		"CLAUDE.md":     "Claude instructions",
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

func TestBuildSystemPrompt_RawVsLegacy(t *testing.T) {
	tests := []struct {
		name           string
		projectContext *ProjectContext
		expectHeader   string
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
