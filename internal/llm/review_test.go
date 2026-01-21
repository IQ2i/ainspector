package llm

import (
	"context"
	"encoding/json"
	"errors"
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

	results := ReviewFunctions(ctx, client, functions)

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

	results := ReviewFunctions(ctx, client, functions)

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

	results := ReviewFunctions(ctx, client, functions)

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

	results := ReviewFunctions(ctx, client, functions)

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

	results := ReviewFunctions(ctx, client, []extractor.ExtractedFunction{})

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

	results := ReviewFunctions(ctx, client, []extractor.ExtractedFunction{originalFn})

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

	ReviewFunctions(ctx, client, functions)

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
		name           string
		response       string
		expectedCount  int
		expectedFirst  *Suggestion
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
