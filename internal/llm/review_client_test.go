package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/llm"
	"github.com/iq2i/ainspector/internal/llm/openai"
)

func TestReviewFunctions_SingleFunction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openai.ChatResponse{
			Choices: []struct {
				Message openai.ChatMessage `json:"message"`
			}{
				{Message: openai.ChatMessage{Role: "assistant", Content: "LGTM"}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := openai.New(server.URL, "key", "gpt-4")
	ctx := context.Background()

	functions := []collect.ReviewUnit{
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

	results := llm.ReviewFunctions(ctx, client, functions, nil, nil)

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
		resp := openai.ChatResponse{
			Choices: []struct {
				Message openai.ChatMessage `json:"message"`
			}{
				{Message: openai.ChatMessage{Role: "assistant", Content: jsonResponse}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := openai.New(server.URL, "key", "gpt-4")
	ctx := context.Background()

	functions := []collect.ReviewUnit{
		{Name: "test", FilePath: "test.go", Language: "go"},
	}

	results := llm.ReviewFunctions(ctx, client, functions, nil, nil)

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
		resp := openai.ChatResponse{
			Choices: []struct {
				Message openai.ChatMessage `json:"message"`
			}{
				{Message: openai.ChatMessage{Role: "assistant", Content: "LGTM"}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := openai.New(server.URL, "key", "gpt-4")
	ctx := context.Background()

	functions := []collect.ReviewUnit{
		{Name: "func1", FilePath: "a.go", Language: "go", ChangeType: "modified"},
		{Name: "func2", FilePath: "b.go", Language: "go", ChangeType: "added"},
		{Name: "func3", FilePath: "c.go", Language: "go", ChangeType: "modified"},
	}

	results := llm.ReviewFunctions(ctx, client, functions, nil, nil)

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
		resp := openai.ChatResponse{
			Choices: []struct {
				Message openai.ChatMessage `json:"message"`
			}{
				{Message: openai.ChatMessage{Role: "assistant", Content: "LGTM"}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := openai.New(server.URL, "key", "gpt-4")
	ctx := context.Background()

	functions := []collect.ReviewUnit{
		{Name: "func1"},
		{Name: "func2"},
		{Name: "func3"},
	}

	results := llm.ReviewFunctions(ctx, client, functions, nil, nil)

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

	client := openai.New(server.URL, "key", "gpt-4")
	ctx := context.Background()

	results := llm.ReviewFunctions(ctx, client, []collect.ReviewUnit{}, nil, nil)

	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestReviewFunctions_PreservesFunction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openai.ChatResponse{
			Choices: []struct {
				Message openai.ChatMessage `json:"message"`
			}{
				{Message: openai.ChatMessage{Role: "assistant", Content: "LGTM"}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := openai.New(server.URL, "key", "gpt-4")
	ctx := context.Background()

	originalFn := collect.ReviewUnit{
		Name:       "testFunc",
		StartLine:  10,
		EndLine:    20,
		Content:    "func testFunc() {}",
		Diff:       "+new line",
		FilePath:   "test.go",
		Language:   "go",
		ChangeType: "modified",
	}

	results := llm.ReviewFunctions(ctx, client, []collect.ReviewUnit{originalFn}, nil, nil)

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

func TestReviewFunctions_VerifiesPromptStructure(t *testing.T) {
	var receivedMessages []openai.ChatMessage

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req openai.ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		receivedMessages = req.Messages

		resp := openai.ChatResponse{
			Choices: []struct {
				Message openai.ChatMessage `json:"message"`
			}{
				{Message: openai.ChatMessage{Role: "assistant", Content: "LGTM"}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := openai.New(server.URL, "key", "gpt-4")
	ctx := context.Background()

	functions := []collect.ReviewUnit{
		{Name: "test", Language: "go", FilePath: "test.go"},
	}

	llm.ReviewFunctions(ctx, client, functions, nil, nil)

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

func TestReviewFunctions_WithProjectRules(t *testing.T) {
	var receivedMessages []openai.ChatMessage

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req openai.ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		receivedMessages = req.Messages

		resp := openai.ChatResponse{
			Choices: []struct {
				Message openai.ChatMessage `json:"message"`
			}{
				{Message: openai.ChatMessage{Role: "assistant", Content: "LGTM"}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := openai.New(server.URL, "key", "gpt-4")
	ctx := context.Background()

	functions := []collect.ReviewUnit{
		{Name: "test", Language: "go", FilePath: "test.go"},
	}

	rules := []string{
		"No console.log in production",
		"All functions must have tests",
	}

	results := llm.ReviewFunctions(ctx, client, functions, nil, rules)

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
