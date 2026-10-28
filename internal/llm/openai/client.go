package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/iq2i/ainspector/internal/llm"
)

const (
	maxResponseBytes = 16 << 20
	maxErrorBytes    = 1 << 10
)

// Structured output levels, from the strictest to the most widely supported
const (
	levelJSONSchema int32 = iota
	levelJSONObject
	levelText
)

// Client is an HTTP client for OpenAI-compatible APIs.
type Client struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
	// schemaLevel is the strictest structured output level the endpoint accepted so far
	schemaLevel atomic.Int32
}

// ChatMessage represents a message in the chat conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ResponseFormat asks the endpoint for a JSON reply.
type ResponseFormat struct {
	Type       string      `json:"type"`
	JSONSchema *JSONSchema `json:"json_schema,omitempty"`
}

// JSONSchema is the schema of a "json_schema" response format.
type JSONSchema struct {
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
	Strict bool            `json:"strict"`
}

// ChatRequest represents a request to the chat completions endpoint.
type ChatRequest struct {
	Model          string          `json:"model"`
	Messages       []ChatMessage   `json:"messages"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
}

// Choice is one completion of a chat response.
type Choice struct {
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason,omitempty"`
}

// ChatUsage counts the tokens of a chat response.
type ChatUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
}

// ChatResponse represents a response from the chat completions endpoint.
type ChatResponse struct {
	Choices []Choice   `json:"choices"`
	Usage   *ChatUsage `json:"usage,omitempty"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// apiError is a non-200 answer of the endpoint.
type apiError struct {
	status int
	body   string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("API error (status %d): %s", e.status, e.body)
}

// New creates a new OpenAI-compatible client.
func New(baseURL, apiKey, model string) *Client {
	// Ensure baseURL doesn't end with a slash
	baseURL = strings.TrimSuffix(baseURL, "/")

	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// Complete sends a chat completion request and returns the response content.
// With a Schema, the strictest structured output mode the endpoint accepts is used and
// Response.JSON holds a document that satisfies the schema.
func (c *Client) Complete(ctx context.Context, req llm.Request) (llm.Response, error) {
	model := req.Model
	if model == "" {
		model = c.model
	}

	level := levelJSONSchema
	if req.Schema != nil {
		level = c.schemaLevel.Load()
	}

	for {
		chatResp, err := c.send(ctx, buildRequest(model, req, level))

		var apiErr *apiError
		if err != nil && req.Schema != nil && errors.As(err, &apiErr) && apiErr.status == http.StatusBadRequest && level < levelText {
			level++
			continue
		}
		if err != nil {
			return llm.Response{}, err
		}
		if req.Schema != nil {
			c.raiseSchemaLevel(level)
		}
		return toResponse(model, req.Schema, chatResp)
	}
}

// raiseSchemaLevel remembers a fallback so that later requests skip the modes the endpoint rejects.
// It is only called once a request succeeded: a 400 unrelated to the format must not degrade the run.
func (c *Client) raiseSchemaLevel(level int32) {
	for {
		current := c.schemaLevel.Load()
		if current >= level || c.schemaLevel.CompareAndSwap(current, level) {
			return
		}
	}
}

func buildRequest(model string, req llm.Request, level int32) ChatRequest {
	texts := make([]string, 0, len(req.System)+1)
	for _, b := range req.System {
		texts = append(texts, b.Text)
	}

	chatReq := ChatRequest{Model: model, MaxTokens: req.MaxTokens}
	if req.Schema != nil {
		switch level {
		case levelJSONSchema:
			chatReq.ResponseFormat = &ResponseFormat{
				Type:       "json_schema",
				JSONSchema: &JSONSchema{Name: req.Schema.Name, Schema: req.Schema.JSON, Strict: true},
			}
		case levelJSONObject:
			chatReq.ResponseFormat = &ResponseFormat{Type: "json_object"}
			texts = append(texts, schemaInstruction(req.Schema))
		default:
			texts = append(texts, schemaInstruction(req.Schema))
		}
	}

	if len(texts) > 0 {
		chatReq.Messages = append(chatReq.Messages, ChatMessage{Role: "system", Content: strings.Join(texts, "\n\n")})
	}
	for _, m := range req.Messages {
		chatReq.Messages = append(chatReq.Messages, ChatMessage{Role: m.Role, Content: m.Content})
	}
	return chatReq
}

func schemaInstruction(s *llm.Schema) string {
	return "Respond with a single JSON object that conforms to this JSON schema, and nothing else:\n" + string(s.JSON)
}

func toResponse(model string, schema *llm.Schema, chatResp ChatResponse) (llm.Response, error) {
	choice := chatResp.Choices[0]
	resp := llm.Response{
		Text:      choice.Message.Content,
		Model:     model,
		Truncated: choice.FinishReason == "length",
	}
	if u := chatResp.Usage; u != nil {
		resp.Usage = llm.Usage{Input: u.PromptTokens, Output: u.CompletionTokens}
		if u.PromptTokensDetails != nil {
			// Like the Anthropic API, Input does not include the tokens read from the cache
			resp.Usage.CacheRead = u.PromptTokensDetails.CachedTokens
			resp.Usage.Input -= u.PromptTokensDetails.CachedTokens
		}
	}

	if schema != nil {
		doc, err := extractJSON(choice.Message.Content, schema.JSON)
		if err != nil {
			// The tokens were billed even though the reply is unusable
			if resp.Truncated {
				return resp, fmt.Errorf("response truncated before the JSON document was complete: %w", err)
			}
			return resp, err
		}
		resp.JSON = doc
	}
	return resp, nil
}

func (c *Client) send(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := c.baseURL + "/v1/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		msg := string(respBody)
		if len(msg) > maxErrorBytes {
			msg = msg[:maxErrorBytes] + "... (truncated)"
		}
		return ChatResponse{}, &apiError{status: resp.StatusCode, body: msg}
	}

	if len(respBody) > maxResponseBytes {
		return ChatResponse{}, fmt.Errorf("response exceeds %d bytes", maxResponseBytes)
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return ChatResponse{}, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if chatResp.Error != nil {
		return ChatResponse{}, fmt.Errorf("API error: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return ChatResponse{}, fmt.Errorf("no choices in response")
	}

	return chatResp, nil
}
