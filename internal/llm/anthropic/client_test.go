package anthropic

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/v2/option"

	"github.com/iq2i/ainspector/internal/llm"
)

var update = flag.Bool("update", false, "regenerate golden files")

type recorded struct {
	body    []byte
	headers http.Header
}

func newServer(t *testing.T, status int, reply string) (*Client, *recorded) {
	t.Helper()
	rec := &recorded{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.body, _ = io.ReadAll(r.Body)
		rec.headers = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(server.Close)
	return New("test-key", "claude-sonnet-5-5", option.WithBaseURL(server.URL), option.WithMaxRetries(0)), rec
}

func message(stopReason, content string) string {
	return `{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5-5","stop_reason":"` + stopReason + `",` +
		`"content":` + content + `,` +
		`"usage":{"input_tokens":120,"output_tokens":30,"cache_read_input_tokens":800,"cache_creation_input_tokens":50}}`
}

func textBlocks(text string) string {
	b, _ := json.Marshal([]map[string]string{{"type": "thinking", "thinking": "", "signature": "s"}, {"type": "text", "text": text}})
	return string(b)
}

var schema = &llm.Schema{
	Name: "review",
	JSON: json.RawMessage(`{"type":"object","properties":{"issues":{"type":"array","items":{"type":"string"}}},"required":["issues"],"additionalProperties":false}`),
}

func fullRequest() llm.Request {
	return llm.Request{
		System: []llm.Block{
			{Text: "stable prefix", Cache: true},
			{Text: "language rules"},
		},
		Messages: []llm.Message{{Role: "user", Content: "review this"}},
		Schema:   schema,
	}
}

func TestComplete_RequestGolden(t *testing.T) {
	client, rec := newServer(t, 200, message("end_turn", textBlocks(`{"issues":[]}`)))
	if _, err := client.Complete(context.Background(), fullRequest()); err != nil {
		t.Fatal(err)
	}

	var pretty any
	_ = json.Unmarshal(rec.body, &pretty)
	got, _ := json.MarshalIndent(pretty, "", "  ")
	got = append(got, '\n')

	path := filepath.Join("testdata", "request.json")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden (run with -update): %v", err)
	}
	if string(want) != string(got) {
		t.Errorf("request body changed\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestComplete_Headers(t *testing.T) {
	client, rec := newServer(t, 200, message("end_turn", textBlocks(`{"issues":[]}`)))
	if _, err := client.Complete(context.Background(), fullRequest()); err != nil {
		t.Fatal(err)
	}

	if rec.headers.Get("Authorization") != "Bearer test-key" {
		t.Errorf("api key not sent")
	}
	if !strings.Contains(rec.headers.Get("Anthropic-Beta"), "server-side-fallback-2026-07-01") {
		t.Errorf("fallback beta missing: %q", rec.headers.Get("Anthropic-Beta"))
	}
}

func TestComplete_Response(t *testing.T) {
	client, _ := newServer(t, 200, message("end_turn", textBlocks(`{"issues":["a"]}`)))
	resp, err := client.Complete(context.Background(), fullRequest())
	if err != nil {
		t.Fatal(err)
	}

	if string(resp.JSON) != `{"issues":["a"]}` || resp.Text != `{"issues":["a"]}` {
		t.Errorf("thinking blocks must be ignored, got text %q json %s", resp.Text, resp.JSON)
	}
	if resp.Model != "claude-sonnet-5-5" || resp.Truncated {
		t.Errorf("unexpected response %+v", resp)
	}
	want := llm.Usage{Input: 120, Output: 30, CacheRead: 800, CacheWrite: 50}
	if resp.Usage != want {
		t.Errorf("expected usage %+v, got %+v", want, resp.Usage)
	}
}

func TestComplete_WithoutSchemaLeavesJSONEmpty(t *testing.T) {
	client, rec := newServer(t, 200, message("end_turn", textBlocks("hello")))
	req := llm.Request{Messages: []llm.Message{{Role: "user", Content: "hi"}}, MaxTokens: 256, Model: "claude-opus-5-5"}
	resp, err := client.Complete(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.JSON != nil || resp.Text != "hello" {
		t.Errorf("unexpected response %+v", resp)
	}

	var body map[string]any
	_ = json.Unmarshal(rec.body, &body)
	if body["model"] != "claude-opus-5-5" || body["max_tokens"] != float64(256) {
		t.Errorf("request overrides not applied: %v", body)
	}
	if out, _ := body["output_config"].(map[string]any); out["format"] != nil {
		t.Errorf("no format expected without schema: %v", out)
	}
}

func TestComplete_AssistantMessage(t *testing.T) {
	client, rec := newServer(t, 200, message("end_turn", textBlocks("ok")))
	_, err := client.Complete(context.Background(), llm.Request{Messages: []llm.Message{
		{Role: "user", Content: "q"}, {Role: "assistant", Content: "a"}, {Role: "user", Content: "q2"},
	}})
	if err != nil {
		t.Fatal(err)
	}

	var body struct {
		Messages []struct{ Role string } `json:"messages"`
	}
	_ = json.Unmarshal(rec.body, &body)
	if len(body.Messages) != 3 || body.Messages[1].Role != "assistant" {
		t.Errorf("unexpected roles %+v", body.Messages)
	}
}

func TestComplete_Refusal(t *testing.T) {
	reply := `{"id":"m","type":"message","role":"assistant","model":"claude-sonnet-5-5","stop_reason":"refusal","content":[],` +
		`"stop_details":{"type":"refusal","category":"cyber","explanation":"declined"},` +
		`"usage":{"input_tokens":1,"output_tokens":0}}`
	client, _ := newServer(t, 200, reply)

	_, err := client.Complete(context.Background(), fullRequest())
	if err == nil || !strings.Contains(err.Error(), "cyber") || !strings.Contains(err.Error(), "declined") {
		t.Errorf("expected a refusal error with its category, got %v", err)
	}
}

func TestComplete_Truncated(t *testing.T) {
	client, _ := newServer(t, 200, message("max_tokens", textBlocks(`{"issues":[`)))

	_, err := client.Complete(context.Background(), fullRequest())
	if err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Errorf("expected a truncation error with a schema, got %v", err)
	}

	resp, err := client.Complete(context.Background(), llm.Request{Messages: []llm.Message{{Role: "user", Content: "hi"}}})
	if err != nil || !resp.Truncated {
		t.Errorf("expected a truncated response without schema, got %+v / %v", resp, err)
	}
}

func TestComplete_InvalidJSON(t *testing.T) {
	client, _ := newServer(t, 200, message("end_turn", textBlocks("LGTM")))
	if _, err := client.Complete(context.Background(), fullRequest()); err == nil {
		t.Error("expected an error for a non JSON reply with a schema")
	}
}

func TestComplete_APIError(t *testing.T) {
	client, _ := newServer(t, 400, `{"type":"error","error":{"type":"invalid_request_error","message":"bad"}}`)
	_, err := client.Complete(context.Background(), fullRequest())
	if err == nil || !strings.Contains(err.Error(), "bad") {
		t.Errorf("expected the API error, got %v", err)
	}
	if strings.Contains(err.Error(), "test-key") {
		t.Error("the API key must never appear in errors")
	}
}

func TestComplete_NoModel(t *testing.T) {
	client := New("k", "")
	if _, err := client.Complete(context.Background(), llm.Request{}); err == nil {
		t.Error("expected an error without model")
	}
}

func TestBackend(t *testing.T) {
	if got := New("k", "m").Backend(); got != "anthropic" {
		t.Errorf("expected backend 'anthropic', got %s", got)
	}
}

func TestComplete_FallbackKeepsTheServedAnswerOnly(t *testing.T) {
	content := `[` +
		`{"type":"text","text":"{\"issues\":[\"par"},` +
		`{"type":"fallback","from":{"model":"claude-sonnet-5-5"},"to":{"model":"claude-opus-5-5"},"trigger":{"type":"refusal"}},` +
		`{"type":"text","text":"{\"issues\":[\"b\"]}"}]`
	reply := `{"id":"m","type":"message","role":"assistant","model":"claude-opus-5-5","stop_reason":"end_turn","content":` + content + `,` +
		`"usage":{"input_tokens":200,"output_tokens":40,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"iterations":[` +
		`{"type":"message","model":"claude-sonnet-5-5","input_tokens":100,"output_tokens":10,"cache_read_input_tokens":800,"cache_creation_input_tokens":0},` +
		`{"type":"fallback_message","model":"claude-opus-5-5","input_tokens":200,"output_tokens":40,"cache_read_input_tokens":0,"cache_creation_input_tokens":50}]}}`
	client, _ := newServer(t, 200, reply)

	resp, err := client.Complete(context.Background(), fullRequest())
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.JSON) != `{"issues":["b"]}` {
		t.Errorf("the output of the model that declined must be dropped, got %s", resp.JSON)
	}
	if resp.Model != "claude-opus-5-5" || len(resp.Attempts) != 2 || resp.Attempts[0].Model != "claude-sonnet-5-5" {
		t.Errorf("expected the two attempts, got %+v", resp)
	}
	if want := (llm.Usage{Input: 300, Output: 50, CacheRead: 800, CacheWrite: 50}); resp.Usage != want {
		t.Errorf("usage must sum the attempts, want %+v, got %+v", want, resp.Usage)
	}
}

func TestComplete_FailuresStillReportUsage(t *testing.T) {
	refusal := `{"id":"m","type":"message","role":"assistant","model":"claude-sonnet-5-5","stop_reason":"refusal","content":[],` +
		`"stop_details":{"type":"refusal","category":"cyber","explanation":"declined"},` +
		`"usage":{"input_tokens":7,"output_tokens":3}}`

	cases := map[string]string{
		"refusal":   refusal,
		"truncated": message("max_tokens", textBlocks(`{"issues":[`)),
		"not json":  message("end_turn", textBlocks("LGTM")),
	}
	for name, reply := range cases {
		t.Run(name, func(t *testing.T) {
			client, _ := newServer(t, 200, reply)
			resp, err := client.Complete(context.Background(), fullRequest())
			if err == nil {
				t.Fatal("expected an error")
			}
			if resp.Usage.Input == 0 || resp.Model == "" {
				t.Errorf("the tokens were billed, the response must carry them, got %+v", resp)
			}
		})
	}
}
