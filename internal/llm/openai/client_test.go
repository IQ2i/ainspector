package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iq2i/ainspector/internal/llm"
)

func userRequest(content string) llm.Request {
	return llm.Request{Messages: []llm.Message{{Role: "user", Content: content}}}
}

func TestNew(t *testing.T) {
	client := New("https://api.example.com", "test-key", "gpt-4")

	if client.baseURL != "https://api.example.com" {
		t.Errorf("expected baseURL 'https://api.example.com', got %s", client.baseURL)
	}
	if client.apiKey != "test-key" {
		t.Errorf("expected apiKey 'test-key', got %s", client.apiKey)
	}
	if client.model != "gpt-4" {
		t.Errorf("expected model 'gpt-4', got %s", client.model)
	}
	if client.httpClient == nil {
		t.Error("expected httpClient to be initialized")
	}
}

func TestNew_TrimsTrailingSlash(t *testing.T) {
	client := New("https://api.example.com/", "key", "model")
	if client.baseURL != "https://api.example.com" {
		t.Errorf("expected trailing slash to be trimmed, got %s", client.baseURL)
	}
}

func TestClient_Complete_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("expected path '/v1/chat/completions', got %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type 'application/json', got %s", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Authorization") != "Bearer test-api-key" {
			t.Errorf("expected Authorization 'Bearer test-api-key', got %s", r.Header.Get("Authorization"))
		}

		// Decode request body
		var req ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req.Model != "gpt-4" {
			t.Errorf("expected model 'gpt-4', got %s", req.Model)
		}
		if len(req.Messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(req.Messages))
		}

		// Send response
		resp := ChatResponse{
			Choices: []Choice{
				{Message: ChatMessage{Role: "assistant", Content: "Hello, world!"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL, "test-api-key", "gpt-4")
	ctx := context.Background()

	result, err := client.Complete(ctx, userRequest("Say hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %s", result.Text)
	}
	if result.Model != "gpt-4" {
		t.Errorf("expected model 'gpt-4', got %s", result.Model)
	}
}

func TestClient_Complete_NoApiKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check that no Authorization header is sent when no API key
		if r.Header.Get("Authorization") != "" {
			t.Error("expected no Authorization header when API key is empty")
		}

		resp := ChatResponse{
			Choices: []Choice{
				{Message: ChatMessage{Role: "assistant", Content: "response"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL, "", "gpt-4")
	ctx := context.Background()

	_, err := client.Complete(ctx, userRequest("test"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_Complete_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": {"message": "Invalid API key"}}`))
	}))
	defer server.Close()

	client := New(server.URL, "bad-key", "gpt-4")
	ctx := context.Background()

	_, err := client.Complete(ctx, userRequest("test"))
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	if !strings.Contains(err.Error(), "API error (status 401)") {
		t.Errorf("expected API error message, got: %v", err)
	}
}

func TestClient_Complete_ErrorInResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ChatResponse{
			Error: &struct {
				Message string `json:"message"`
			}{
				Message: "Rate limit exceeded",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL, "key", "gpt-4")
	ctx := context.Background()

	_, err := client.Complete(ctx, userRequest("test"))
	if err == nil {
		t.Fatal("expected error for error in response")
	}
	if !strings.Contains(err.Error(), "Rate limit exceeded") {
		t.Errorf("expected rate limit error, got: %v", err)
	}
}

func TestClient_Complete_NoChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ChatResponse{
			Choices: []Choice{},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL, "key", "gpt-4")
	ctx := context.Background()

	_, err := client.Complete(ctx, userRequest("test"))
	if err == nil {
		t.Fatal("expected error for no choices")
	}
	if !strings.Contains(err.Error(), "no choices") {
		t.Errorf("expected 'no choices' error, got: %v", err)
	}
}

func TestClient_Complete_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not valid json`))
	}))
	defer server.Close()

	client := New(server.URL, "key", "gpt-4")
	ctx := context.Background()

	_, err := client.Complete(ctx, userRequest("test"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("expected unmarshal error, got: %v", err)
	}
}

func TestClient_Complete_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response
		time.Sleep(100 * time.Millisecond)
		resp := ChatResponse{
			Choices: []Choice{
				{Message: ChatMessage{Role: "assistant", Content: "response"}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL, "key", "gpt-4")
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	cancel()

	_, err := client.Complete(ctx, userRequest("test"))
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestClient_Complete_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`Internal Server Error`))
	}))
	defer server.Close()

	client := New(server.URL, "key", "gpt-4")
	ctx := context.Background()

	_, err := client.Complete(ctx, userRequest("test"))
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected 500 in error, got: %v", err)
	}
}

func TestClient_Complete_MultipleMessages(t *testing.T) {
	var receivedMessages []ChatMessage

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		receivedMessages = req.Messages

		resp := ChatResponse{
			Choices: []Choice{
				{Message: ChatMessage{Role: "assistant", Content: "response"}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL, "key", "gpt-4")
	ctx := context.Background()

	req := llm.Request{
		System: []llm.Block{{Text: "You are a helpful assistant"}},
		Messages: []llm.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there!"},
			{Role: "user", Content: "How are you?"},
		},
	}

	_, err := client.Complete(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(receivedMessages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(receivedMessages))
	}

	if receivedMessages[0].Role != "system" {
		t.Errorf("expected first message role 'system', got %s", receivedMessages[0].Role)
	}
}

func TestChatMessage_JSONSerialization(t *testing.T) {
	msg := ChatMessage{Role: "user", Content: "Hello"}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ChatMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Role != msg.Role {
		t.Errorf("expected role %s, got %s", msg.Role, decoded.Role)
	}
	if decoded.Content != msg.Content {
		t.Errorf("expected content %s, got %s", msg.Content, decoded.Content)
	}
}

func TestChatRequest_JSONSerialization(t *testing.T) {
	req := ChatRequest{
		Model: "gpt-4",
		Messages: []ChatMessage{
			{Role: "user", Content: "Hello"},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ChatRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Model != req.Model {
		t.Errorf("expected model %s, got %s", req.Model, decoded.Model)
	}
	if len(decoded.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(decoded.Messages))
	}
}

func TestClient_Complete_SystemBlocksAndOverrides(t *testing.T) {
	var got ChatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	client := New(server.URL, "key", "default-model")
	resp, err := client.Complete(context.Background(), llm.Request{
		Model:     "other-model",
		System:    []llm.Block{{Text: "first", Cache: true}, {Text: "second"}},
		Messages:  []llm.Message{{Role: "user", Content: "hi"}},
		MaxTokens: 256,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Model != "other-model" || resp.Model != "other-model" {
		t.Errorf("expected request model override, got %q / %q", got.Model, resp.Model)
	}
	if got.MaxTokens != 256 {
		t.Errorf("expected max_tokens 256, got %d", got.MaxTokens)
	}
	if len(got.Messages) != 2 || got.Messages[0].Role != "system" || got.Messages[0].Content != "first\n\nsecond" {
		t.Errorf("unexpected messages: %+v", got.Messages)
	}
}

func TestClient_Complete_ErrorBodyIsTruncated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(strings.Repeat("x", 100000)))
	}))
	defer server.Close()

	_, err := New(server.URL, "key", "m").Complete(context.Background(), userRequest("hi"))
	if err == nil || len(err.Error()) > 2000 || !strings.Contains(err.Error(), "truncated") {
		t.Errorf("expected a truncated error, got %d bytes", len(err.Error()))
	}
}

func TestClient_Complete_ResponseTooLarge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"`))
		chunk := []byte(strings.Repeat("a", 1<<20))
		for i := 0; i < 17; i++ {
			_, _ = w.Write(chunk)
		}
		_, _ = w.Write([]byte(`"}}]}`))
	}))
	defer server.Close()

	_, err := New(server.URL, "key", "m").Complete(context.Background(), userRequest("hi"))
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("expected a size error, got %v", err)
	}
}

func TestClient_Backend(t *testing.T) {
	if got := New("http://x", "k", "m").Backend(); got != "openai" {
		t.Errorf("expected backend 'openai', got %s", got)
	}
}

func TestClient_WireRequestGolden(t *testing.T) {
	golden, err := os.ReadFile(filepath.Join("testdata", "golden", "wire", "chat_request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var want ChatRequest
	if err := json.Unmarshal(golden, &want); err != nil {
		t.Fatal(err)
	}

	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"LGTM"}}]}`))
	}))
	defer server.Close()

	_, err = New(server.URL, "key", want.Model).Complete(context.Background(), llm.Request{
		System:   []llm.Block{{Text: want.Messages[0].Content}},
		Messages: []llm.Message{{Role: want.Messages[1].Role, Content: want.Messages[1].Content}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if string(body) != string(golden) {
		t.Errorf("wire body changed\n--- want ---\n%s\n--- got ---\n%s", golden, body)
	}
}
