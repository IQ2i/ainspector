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
	"sync"
	"testing"

	"github.com/iq2i/ainspector/internal/llm"
)

var issuesSchema = &llm.Schema{
	Name: "review",
	JSON: json.RawMessage(`{"type":"object","properties":{"issues":{"type":"array","items":{"type":"object","properties":{"line":{"type":"integer"}},"required":["line"]}}},"required":["issues"],"additionalProperties":false}`),
}

// schemaServer answers each request with the next handler, and records the bodies
type schemaServer struct {
	*httptest.Server
	mu     sync.Mutex
	bodies []ChatRequest
	raw    [][]byte
}

func newSchemaServer(t *testing.T, replies ...func(w http.ResponseWriter)) *schemaServer {
	t.Helper()
	s := &schemaServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req ChatRequest
		_ = json.Unmarshal(body, &req)

		s.mu.Lock()
		n := len(s.bodies)
		s.bodies = append(s.bodies, req)
		s.raw = append(s.raw, body)
		s.mu.Unlock()

		if n >= len(replies) {
			n = len(replies) - 1
		}
		replies[n](w)
	}))
	t.Cleanup(s.Close)
	return s
}

func okReply(content, finish string) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		out, _ := json.Marshal(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": content}, "finish_reason": finish}},
			"usage": map[string]any{
				"prompt_tokens":         100,
				"completion_tokens":     20,
				"prompt_tokens_details": map[string]int{"cached_tokens": 60},
			},
		})
		_, _ = w.Write(out)
	}
}

func badRequest(w http.ResponseWriter) {
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write([]byte(`{"error":{"message":"response_format not supported"}}`))
}

func schemaRequest() llm.Request {
	return llm.Request{
		System:   []llm.Block{{Text: "system"}},
		Messages: []llm.Message{{Role: "user", Content: "review"}},
		Schema:   issuesSchema,
	}
}

func TestComplete_StrictJSONSchema(t *testing.T) {
	s := newSchemaServer(t, okReply(`{"issues":[{"line":3}]}`, "stop"))

	resp, err := New(s.URL, "k", "m").Complete(context.Background(), schemaRequest())
	if err != nil {
		t.Fatal(err)
	}

	if string(resp.JSON) != `{"issues":[{"line":3}]}` {
		t.Errorf("unexpected JSON %s", resp.JSON)
	}
	want := llm.Usage{Input: 40, Output: 20, CacheRead: 60}
	if resp.Usage != want {
		t.Errorf("expected usage %+v, got %+v", want, resp.Usage)
	}
	rf := s.bodies[0].ResponseFormat
	if rf == nil || rf.Type != "json_schema" || rf.JSONSchema == nil || !rf.JSONSchema.Strict || rf.JSONSchema.Name != "review" {
		t.Errorf("unexpected response_format %+v", rf)
	}
}

func TestComplete_WireSchemaGolden(t *testing.T) {
	s := newSchemaServer(t, okReply(`{"issues":[]}`, "stop"))
	if _, err := New(s.URL, "k", "gpt-4o").Complete(context.Background(), schemaRequest()); err != nil {
		t.Fatal(err)
	}

	golden, err := os.ReadFile(filepath.Join("testdata", "golden", "wire", "chat_request_schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(s.raw[0]) != string(golden) {
		t.Errorf("wire body changed\n--- want ---\n%s\n--- got ---\n%s", golden, s.raw[0])
	}
}

func TestComplete_FallsBackToJSONObject(t *testing.T) {
	s := newSchemaServer(t, badRequest, okReply(`{"issues":[]}`, "stop"))
	client := New(s.URL, "k", "m")

	resp, err := client.Complete(context.Background(), schemaRequest())
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.JSON) != `{"issues":[]}` {
		t.Errorf("unexpected JSON %s", resp.JSON)
	}

	second := s.bodies[1]
	if second.ResponseFormat == nil || second.ResponseFormat.Type != "json_object" {
		t.Errorf("expected json_object, got %+v", second.ResponseFormat)
	}
	if !strings.Contains(second.Messages[0].Content, `"required":["issues"]`) {
		t.Errorf("schema missing from the prompt: %q", second.Messages[0].Content)
	}

	// The fallback is remembered: the next call goes straight to json_object
	if _, err := client.Complete(context.Background(), schemaRequest()); err != nil {
		t.Fatal(err)
	}
	if len(s.bodies) != 3 || s.bodies[2].ResponseFormat == nil || s.bodies[2].ResponseFormat.Type != "json_object" {
		t.Errorf("expected the second call to skip json_schema, got %d requests", len(s.bodies))
	}
}

func TestComplete_FallsBackToTextWithBraceExtraction(t *testing.T) {
	s := newSchemaServer(t, badRequest, badRequest, okReply("Here you go:\n```json\n{\"issues\": [{\"line\": 7}]}\n```", "stop"))

	resp, err := New(s.URL, "k", "m").Complete(context.Background(), schemaRequest())
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.JSON) != `{"issues":[{"line":7}]}` {
		t.Errorf("unexpected JSON %s", resp.JSON)
	}
	if s.bodies[2].ResponseFormat != nil {
		t.Errorf("text level must not send response_format")
	}
}

func TestComplete_UnrelatedBadRequestDoesNotDegradeTheClient(t *testing.T) {
	s := newSchemaServer(t, badRequest, badRequest, badRequest, okReply(`{"issues":[]}`, "stop"))
	client := New(s.URL, "k", "m")

	if _, err := client.Complete(context.Background(), schemaRequest()); err == nil {
		t.Fatal("expected an error once every level was rejected")
	}
	if _, err := client.Complete(context.Background(), schemaRequest()); err != nil {
		t.Fatal(err)
	}
	if s.bodies[3].ResponseFormat == nil || s.bodies[3].ResponseFormat.Type != "json_schema" {
		t.Errorf("a failed run must not lower the level, got %+v", s.bodies[3].ResponseFormat)
	}
}

func TestComplete_InvalidDocumentIsAnError(t *testing.T) {
	cases := map[string]string{
		"prose":       "LGTM",
		"missing key": `{"other":[]}`,
		"wrong type":  `{"issues":[{"line":"three"}]}`,
		"bad json":    `{"issues":[`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			s := newSchemaServer(t, okReply(content, "stop"))
			if _, err := New(s.URL, "k", "m").Complete(context.Background(), schemaRequest()); err == nil {
				t.Errorf("expected an error for %q", content)
			}
		})
	}
}

func TestComplete_TruncatedReply(t *testing.T) {
	s := newSchemaServer(t, okReply(`{"issues":[{"line":`, "length"))
	resp, err := New(s.URL, "k", "m").Complete(context.Background(), schemaRequest())
	if err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Errorf("expected a truncation error, got %v", err)
	}
	if resp.Usage.Input != 40 || resp.Usage.CacheRead != 60 || resp.Usage.Output != 20 {
		t.Errorf("a failed reply was still billed, expected its usage, got %+v", resp.Usage)
	}

	plain := newSchemaServer(t, okReply("cut", "length"))
	resp, err = New(plain.URL, "k", "m").Complete(context.Background(), userRequest("hi"))
	if err != nil || !resp.Truncated {
		t.Errorf("expected a truncated plain response, got %+v / %v", resp, err)
	}
}

func TestComplete_NoSchemaNeverUsesResponseFormat(t *testing.T) {
	s := newSchemaServer(t, badRequest)
	if _, err := New(s.URL, "k", "m").Complete(context.Background(), userRequest("hi")); err == nil {
		t.Fatal("expected the 400 to be returned")
	}
	if len(s.bodies) != 1 {
		t.Errorf("a request without schema must not be retried, got %d requests", len(s.bodies))
	}
}
