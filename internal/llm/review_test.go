package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iq2i/ainspector/internal/extractor"
)

func TestReviewFunctions_SingleFunction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ChatResponse{
			Choices: []struct {
				Message ChatMessage `json:"message"`
			}{
				{Message: ChatMessage{Role: "assistant", Content: "LGTM"}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "key", "gpt-4")
	ctx := context.Background()

	functions := []extractor.ExtractedFunction{
		{
			Name:       "hello",
			StartLine:  1,
			EndLine:    5,
			Content:    "func hello() {\n  println(\"hello\")\n}",
			Diff:       "+println(\"hello\")",
			FilePath:   "main.go",
			Language:   "go",
			ChangeType: "modified",
		},
	}

	results := ReviewFunctions(ctx, client, functions, nil, nil)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Error != nil {
		t.Errorf("unexpected error: %v", results[0].Error)
	}
	if results[0].RawReview != "LGTM" {
		t.Errorf("expected raw review 'LGTM', got %s", results[0].RawReview)
	}
	if results[0].Function.Name != "hello" {
		t.Errorf("expected function name 'hello', got %s", results[0].Function.Name)
	}
}

func TestReviewFunctions_WithIssues(t *testing.T) {
	jsonResponse := `{"issues":[{"line":10,"description":"Potential null pointer","suggestion":"if ptr != nil { ... }"}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ChatResponse{
			Choices: []struct {
				Message ChatMessage `json:"message"`
			}{
				{Message: ChatMessage{Role: "assistant", Content: jsonResponse}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "key", "gpt-4")
	ctx := context.Background()

	functions := []extractor.ExtractedFunction{
		{Name: "test", FilePath: "test.go", Language: "go"},
	}

	results := ReviewFunctions(ctx, client, functions, nil, nil)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if len(results[0].Suggestions) != 1 {
		t.Fatalf("expected 1 suggestion, got %d", len(results[0].Suggestions))
	}
	if results[0].Suggestions[0].Line != 10 {
		t.Errorf("expected line 10, got %d", results[0].Suggestions[0].Line)
	}
	if results[0].Suggestions[0].Description != "Potential null pointer" {
		t.Errorf("unexpected description: %s", results[0].Suggestions[0].Description)
	}
	if results[0].Suggestions[0].Code != "if ptr != nil { ... }" {
		t.Errorf("unexpected suggestion code: %s", results[0].Suggestions[0].Code)
	}
}

func TestReviewFunctions_MultipleFunctions(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := ChatResponse{
			Choices: []struct {
				Message ChatMessage `json:"message"`
			}{
				{Message: ChatMessage{Role: "assistant", Content: "LGTM"}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "key", "gpt-4")
	ctx := context.Background()

	functions := []extractor.ExtractedFunction{
		{Name: "func1", FilePath: "a.go", Language: "go", ChangeType: "modified"},
		{Name: "func2", FilePath: "b.go", Language: "go", ChangeType: "added"},
		{Name: "func3", FilePath: "c.go", Language: "go", ChangeType: "modified"},
	}

	results := ReviewFunctions(ctx, client, functions, nil, nil)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// All should succeed
	for i, result := range results {
		if result.Error != nil {
			t.Errorf("result %d: unexpected error: %v", i, result.Error)
		}
	}

	// API should be called 3 times
	if callCount != 3 {
		t.Errorf("expected 3 API calls, got %d", callCount)
	}
}

func TestReviewFunctions_MixedSuccessAndFailure(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 2 {
			// Second call fails
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Server error"))
			return
		}
		resp := ChatResponse{
			Choices: []struct {
				Message ChatMessage `json:"message"`
			}{
				{Message: ChatMessage{Role: "assistant", Content: "LGTM"}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "key", "gpt-4")
	ctx := context.Background()

	functions := []extractor.ExtractedFunction{
		{Name: "func1"},
		{Name: "func2"},
		{Name: "func3"},
	}

	results := ReviewFunctions(ctx, client, functions, nil, nil)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// First and third should succeed
	if results[0].Error != nil {
		t.Errorf("result 0: unexpected error: %v", results[0].Error)
	}
	if results[0].RawReview != "LGTM" {
		t.Errorf("result 0: expected 'LGTM', got %s", results[0].RawReview)
	}

	// Second should fail
	if results[1].Error == nil {
		t.Error("result 1: expected error")
	}
	if results[1].RawReview != "" {
		t.Errorf("result 1: expected empty raw review on error, got %s", results[1].RawReview)
	}

	// Third should succeed (doesn't stop on error)
	if results[2].Error != nil {
		t.Errorf("result 2: unexpected error: %v", results[2].Error)
	}
}

func TestReviewFunctions_EmptyList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("API should not be called for empty list")
	}))
	defer server.Close()

	client := NewClient(server.URL, "key", "gpt-4")
	ctx := context.Background()

	results := ReviewFunctions(ctx, client, []extractor.ExtractedFunction{}, nil, nil)

	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestReviewFunctions_PreservesFunction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ChatResponse{
			Choices: []struct {
				Message ChatMessage `json:"message"`
			}{
				{Message: ChatMessage{Role: "assistant", Content: "LGTM"}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "key", "gpt-4")
	ctx := context.Background()

	originalFn := extractor.ExtractedFunction{
		Name:       "testFunc",
		StartLine:  10,
		EndLine:    20,
		Content:    "func testFunc() {}",
		Diff:       "+new line",
		FilePath:   "test.go",
		Language:   "go",
		ChangeType: "modified",
	}

	results := ReviewFunctions(ctx, client, []extractor.ExtractedFunction{originalFn}, nil, nil)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	fn := results[0].Function
	if fn.Name != originalFn.Name {
		t.Errorf("name mismatch: got %s", fn.Name)
	}
	if fn.StartLine != originalFn.StartLine {
		t.Errorf("start line mismatch: got %d", fn.StartLine)
	}
	if fn.EndLine != originalFn.EndLine {
		t.Errorf("end line mismatch: got %d", fn.EndLine)
	}
	if fn.Content != originalFn.Content {
		t.Errorf("content mismatch")
	}
	if fn.Diff != originalFn.Diff {
		t.Errorf("diff mismatch")
	}
	if fn.FilePath != originalFn.FilePath {
		t.Errorf("file path mismatch: got %s", fn.FilePath)
	}
	if fn.Language != originalFn.Language {
		t.Errorf("language mismatch: got %s", fn.Language)
	}
	if fn.ChangeType != originalFn.ChangeType {
		t.Errorf("change type mismatch: got %s", fn.ChangeType)
	}
}

func TestBuildUserPrompt_WithDiff(t *testing.T) {
	fn := &extractor.ExtractedFunction{
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
	fn := &extractor.ExtractedFunction{
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
	fn := &extractor.ExtractedFunction{
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
	fn := &extractor.ExtractedFunction{
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

func TestReviewFunctions_VerifiesPromptStructure(t *testing.T) {
	var receivedMessages []ChatMessage

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		receivedMessages = req.Messages

		resp := ChatResponse{
			Choices: []struct {
				Message ChatMessage `json:"message"`
			}{
				{Message: ChatMessage{Role: "assistant", Content: "LGTM"}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "key", "gpt-4")
	ctx := context.Background()

	functions := []extractor.ExtractedFunction{
		{Name: "test", Language: "go", FilePath: "test.go"},
	}

	ReviewFunctions(ctx, client, functions, nil, nil)

	// Should have 2 messages: system and user
	if len(receivedMessages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(receivedMessages))
	}

	// First should be system prompt
	if receivedMessages[0].Role != "system" {
		t.Errorf("expected first message role 'system', got %s", receivedMessages[0].Role)
	}
	if !strings.Contains(receivedMessages[0].Content, "code reviewer") {
		t.Error("system prompt should mention code reviewer")
	}

	// Second should be user prompt
	if receivedMessages[1].Role != "user" {
		t.Errorf("expected second message role 'user', got %s", receivedMessages[1].Role)
	}
	if !strings.Contains(receivedMessages[1].Content, "test.go") {
		t.Error("user prompt should contain file path")
	}
}

func TestReviewResult_Error(t *testing.T) {
	result := ReviewResult{
		Function: extractor.ExtractedFunction{Name: "test"},
		Error:    errors.New("API error"),
	}

	if result.Error == nil {
		t.Error("expected error to be set")
	}
	if result.RawReview != "" {
		t.Error("expected empty raw review when there's an error")
	}
}

func TestReviewResult_HasIssues(t *testing.T) {
	tests := []struct {
		name     string
		result   ReviewResult
		expected bool
	}{
		{
			name: "has issues when suggestions present",
			result: ReviewResult{
				Suggestions: []Suggestion{
					{Line: 10, Description: "Bug", Code: "fix"},
				},
			},
			expected: true,
		},
		{
			name: "no issues when suggestions empty",
			result: ReviewResult{
				Suggestions: []Suggestion{},
			},
			expected: false,
		},
		{
			name: "no issues when suggestions nil",
			result: ReviewResult{
				Suggestions: nil,
			},
			expected: false,
		},
		{
			name: "no issues when error present",
			result: ReviewResult{
				Suggestions: []Suggestion{
					{Line: 10, Description: "Bug", Code: "fix"},
				},
				Error: errors.New("API error"),
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.HasIssues(); got != tt.expected {
				t.Errorf("HasIssues() = %v, want %v", got, tt.expected)
			}
		})
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

func TestReviewFunctions_WithProjectRules(t *testing.T) {
	var receivedMessages []ChatMessage

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		receivedMessages = req.Messages

		resp := ChatResponse{
			Choices: []struct {
				Message ChatMessage `json:"message"`
			}{
				{Message: ChatMessage{Role: "assistant", Content: "LGTM"}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "key", "gpt-4")
	ctx := context.Background()

	functions := []extractor.ExtractedFunction{
		{Name: "test", Language: "go", FilePath: "test.go"},
	}

	rules := []string{
		"No console.log in production",
		"All functions must have tests",
	}

	results := ReviewFunctions(ctx, client, functions, nil, rules)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// Verify system prompt contains rules
	if len(receivedMessages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(receivedMessages))
	}

	systemPrompt := receivedMessages[0].Content
	if !strings.Contains(systemPrompt, "PROJECT RULES") {
		t.Error("system prompt should contain project rules section")
	}
	if !strings.Contains(systemPrompt, "No console.log in production") {
		t.Error("system prompt should contain first rule")
	}
	if !strings.Contains(systemPrompt, "All functions must have tests") {
		t.Error("system prompt should contain second rule")
	}
}
