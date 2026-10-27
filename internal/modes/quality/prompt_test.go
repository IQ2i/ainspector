package quality

import (
	"fmt"
	"strings"
	"testing"

	"github.com/iq2i/ainspector/internal/collect"
)

func TestBuildUserPrompt_WithDiff(t *testing.T) {
	fn := &collect.ReviewUnit{
		Name:       "hello",
		StartLine:  10,
		EndLine:    15,
		Content:    "func hello() {\n  println(\"hello\")\n}",
		Diff:       "+println(\"hello\")",
		FilePath:   "main.go",
		Language:   "go",
		ChangeType: "modified",
	}

	prompt := buildUserPrompt(fn)

	// Check that all required information is included
	if !strings.Contains(prompt, "go") {
		t.Error("prompt should contain language")
	}
	if !strings.Contains(prompt, "main.go") {
		t.Error("prompt should contain file path")
	}
	if !strings.Contains(prompt, "hello") {
		t.Error("prompt should contain function name")
	}
	if !strings.Contains(prompt, "10") {
		t.Error("prompt should contain start line")
	}
	if !strings.Contains(prompt, "15") {
		t.Error("prompt should contain end line")
	}
	if !strings.Contains(prompt, "modified") {
		t.Error("prompt should contain change type")
	}
	if !strings.Contains(prompt, "+println(\"hello\")") {
		t.Error("prompt should contain diff")
	}
	if !strings.Contains(prompt, "func hello()") {
		t.Error("prompt should contain function content")
	}
	if !strings.Contains(prompt, "```diff") {
		t.Error("prompt should have diff code block")
	}
}

func TestBuildUserPrompt_NoDiff(t *testing.T) {
	fn := &collect.ReviewUnit{
		Name:       "hello",
		StartLine:  1,
		EndLine:    3,
		Content:    "func hello() {}",
		Diff:       "",
		FilePath:   "main.go",
		Language:   "go",
		ChangeType: "added",
	}

	prompt := buildUserPrompt(fn)

	// Should not have diff section when diff is empty
	if strings.Contains(prompt, "```diff") {
		t.Error("prompt should not have diff code block when diff is empty")
	}
	if strings.Contains(prompt, "## Changes") {
		t.Error("prompt should not have Changes section when diff is empty")
	}

	// Should still have the function content
	if !strings.Contains(prompt, "func hello()") {
		t.Error("prompt should contain function content")
	}
}

func TestBuildUserPrompt_AddedFunction(t *testing.T) {
	fn := &collect.ReviewUnit{
		Name:       "newFunc",
		ChangeType: "added",
		Language:   "python",
		FilePath:   "app.py",
	}

	prompt := buildUserPrompt(fn)

	if !strings.Contains(prompt, "added") {
		t.Error("prompt should contain change type 'added'")
	}
	if !strings.Contains(prompt, "python") {
		t.Error("prompt should contain language 'python'")
	}
}

func TestBuildUserPrompt_SpecialCharacters(t *testing.T) {
	fn := &collect.ReviewUnit{
		Name:     "test<script>alert('xss')</script>",
		Content:  "func test() { return \"<script>\" }",
		Diff:     "+<script>alert('xss')</script>",
		FilePath: "test.go",
		Language: "go",
	}

	prompt := buildUserPrompt(fn)

	// Should contain the special characters (not escape them)
	if !strings.Contains(prompt, "<script>") {
		t.Error("prompt should contain special characters as-is")
	}
}

func TestParseReviewResponse(t *testing.T) {
	tests := []struct {
		name          string
		response      string
		expectedCount int
		expectedFirst *Suggestion
	}{
		{
			name:          "LGTM response",
			response:      "LGTM",
			expectedCount: 0,
		},
		{
			name:          "LGTM with whitespace",
			response:      "  LGTM  \n",
			expectedCount: 0,
		},
		{
			name:          "valid JSON response",
			response:      `{"issues":[{"line":10,"description":"Bug","suggestion":"fix"}]}`,
			expectedCount: 1,
			expectedFirst: &Suggestion{Line: 10, Description: "Bug", Code: "fix"},
		},
		{
			name:          "JSON with extra text",
			response:      `Here's my review:\n{"issues":[{"line":5,"description":"Issue","suggestion":""}]}`,
			expectedCount: 1,
			expectedFirst: &Suggestion{Line: 5, Description: "Issue", Code: ""},
		},
		{
			name:          "multiple issues",
			response:      `{"issues":[{"line":1,"description":"A","suggestion":"a"},{"line":2,"description":"B","suggestion":"b"}]}`,
			expectedCount: 2,
		},
		{
			name:          "invalid JSON",
			response:      "This is not JSON",
			expectedCount: 0,
		},
		{
			name:          "empty issues array",
			response:      `{"issues":[]}`,
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggestions := parseReviewResponse(tt.response)
			if len(suggestions) != tt.expectedCount {
				t.Errorf("expected %d suggestions, got %d", tt.expectedCount, len(suggestions))
			}
			if tt.expectedFirst != nil && len(suggestions) > 0 {
				if suggestions[0].Line != tt.expectedFirst.Line {
					t.Errorf("expected line %d, got %d", tt.expectedFirst.Line, suggestions[0].Line)
				}
				if suggestions[0].Description != tt.expectedFirst.Description {
					t.Errorf("expected description %q, got %q", tt.expectedFirst.Description, suggestions[0].Description)
				}
				if suggestions[0].Code != tt.expectedFirst.Code {
					t.Errorf("expected code %q, got %q", tt.expectedFirst.Code, suggestions[0].Code)
				}
			}
		})
	}
}

func TestBuildSystemPrompt_LanguageSpecific(t *testing.T) {
	tests := []struct {
		name             string
		language         string
		expectedContains []string
	}{
		{
			name:     "Go language includes error handling checks",
			language: "go",
			expectedContains: []string{
				"LANGUAGE-SPECIFIC CHECKS FOR GO",
				"errors are properly handled",
				"nil pointer dereferences",
				"goroutines",
			},
		},
		{
			name:     "JavaScript includes async/await checks",
			language: "javascript",
			expectedContains: []string{
				"LANGUAGE-SPECIFIC CHECKS FOR JAVASCRIPT",
				"async functions properly await",
				"promise rejections",
			},
		},
		{
			name:     "TypeScript includes type checks",
			language: "typescript",
			expectedContains: []string{
				"LANGUAGE-SPECIFIC CHECKS FOR TYPESCRIPT",
				"TypeScript types",
				"any' type",
			},
		},
		{
			name:     "Python includes exception handling checks",
			language: "python",
			expectedContains: []string{
				"LANGUAGE-SPECIFIC CHECKS FOR PYTHON",
				"exception handling",
				"context managers",
			},
		},
		{
			name:     "Rust includes Result type checks",
			language: "rust",
			expectedContains: []string{
				"LANGUAGE-SPECIFIC CHECKS FOR RUST",
				"Result type",
				"unwrap",
			},
		},
		{
			name:     "PHP includes SQL injection checks",
			language: "php",
			expectedContains: []string{
				"LANGUAGE-SPECIFIC CHECKS FOR PHP",
				"SQL injection",
				"prepared statements",
				"XSS vulnerabilities",
			},
		},
		{
			name:     "Unknown language uses base prompt only",
			language: "unknown-lang",
			expectedContains: []string{
				"You are an expert code reviewer",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := buildSystemPrompt(tt.language, nil, nil)

			// Should always contain base prompt
			if !strings.Contains(prompt, "You are an expert code reviewer") {
				t.Error("prompt should contain base system prompt")
			}

			// Check for language-specific content
			for _, expected := range tt.expectedContains {
				if !strings.Contains(prompt, expected) {
					t.Errorf("prompt for language %q should contain %q", tt.language, expected)
				}
			}
		})
	}
}

func TestBuildSystemPrompt_BasePromptAlwaysPresent(t *testing.T) {
	languages := []string{"go", "javascript", "python", "rust", "unknown"}

	for _, lang := range languages {
		prompt := buildSystemPrompt(lang, nil, nil)

		// All prompts should contain essential base instructions
		essentials := []string{
			"You are an expert code reviewer",
			"RESPONSE FORMAT",
			"LGTM",
			"JSON object",
		}

		for _, essential := range essentials {
			if !strings.Contains(prompt, essential) {
				t.Errorf("prompt for %q should contain %q", lang, essential)
			}
		}
	}
}

func TestBuildSystemPrompt_WithProjectRules(t *testing.T) {
	rules := []string{
		"Exceptions must not be used for flow control",
		"All database access must go through repositories",
		"console.log must not be used in production code",
	}

	prompt := buildSystemPrompt("go", nil, rules)

	// Should contain the rules section header
	if !strings.Contains(prompt, "=== PROJECT RULES (MUST BE ENFORCED) ===") {
		t.Error("prompt should contain project rules header")
	}

	// Should contain enforcement language
	if !strings.Contains(prompt, "MUST be enforced") {
		t.Error("prompt should contain enforcement language")
	}

	// Should contain all rules with numbering
	for i, rule := range rules {
		expected := fmt.Sprintf("%d. %s", i+1, rule)
		if !strings.Contains(prompt, expected) {
			t.Errorf("prompt should contain numbered rule: %q", expected)
		}
	}

	// Should still contain base prompt
	if !strings.Contains(prompt, "You are an expert code reviewer") {
		t.Error("prompt should still contain base prompt")
	}

	// Should still contain language-specific rules
	if !strings.Contains(prompt, "LANGUAGE-SPECIFIC CHECKS FOR GO") {
		t.Error("prompt should still contain language-specific rules")
	}
}

func TestBuildSystemPrompt_WithoutProjectRules(t *testing.T) {
	// Test with nil rules
	promptNil := buildSystemPrompt("go", nil, nil)
	if strings.Contains(promptNil, "PROJECT RULES") {
		t.Error("prompt should not contain project rules section when rules are nil")
	}

	// Test with empty rules
	promptEmpty := buildSystemPrompt("go", nil, []string{})
	if strings.Contains(promptEmpty, "PROJECT RULES") {
		t.Error("prompt should not contain project rules section when rules are empty")
	}

	// Both should contain base prompt
	if !strings.Contains(promptNil, "You are an expert code reviewer") {
		t.Error("prompt with nil rules should contain base prompt")
	}
	if !strings.Contains(promptEmpty, "You are an expert code reviewer") {
		t.Error("prompt with empty rules should contain base prompt")
	}
}

func TestBuildSystemPrompt_RulesPlacement(t *testing.T) {
	rules := []string{"Test rule"}
	prompt := buildSystemPrompt("go", nil, rules)

	// Rules should come after base prompt but before language-specific rules
	baseIdx := strings.Index(prompt, "You are an expert code reviewer")
	rulesIdx := strings.Index(prompt, "PROJECT RULES")
	langIdx := strings.Index(prompt, "LANGUAGE-SPECIFIC CHECKS")

	if baseIdx == -1 || rulesIdx == -1 || langIdx == -1 {
		t.Fatal("prompt should contain all sections")
	}

	if baseIdx >= rulesIdx {
		t.Error("base prompt should come before project rules")
	}

	if rulesIdx >= langIdx {
		t.Error("project rules should come before language-specific rules")
	}
}

func TestBuildSystemPrompt_WithProjectContext(t *testing.T) {
	projectContext := &collect.ProjectContext{
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
	projectContext := &collect.ProjectContext{
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
		projectContext *collect.ProjectContext
		expectHeader   string
	}{
		{
			name: "raw context",
			projectContext: &collect.ProjectContext{
				Description: "=== README.md ===\nContent",
				Languages:   []string{"go"},
				IsRaw:       true,
			},
			expectHeader: "=== PROJECT CONTEXT ===",
		},
		{
			name: "legacy context",
			projectContext: &collect.ProjectContext{
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
