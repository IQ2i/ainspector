package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/iq2i/ainspector/internal/llm"
)

const (
	maxResponseBytes = 16 << 20
	maxErrorBytes    = 1 << 10
)

// Client is an HTTP client for OpenAI-compatible APIs.
type Client struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

// ChatMessage represents a message in the chat conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest represents a request to the chat completions endpoint.
type ChatRequest struct {
	Model     string        `json:"model"`
	Messages  []ChatMessage `json:"messages"`
	MaxTokens int           `json:"max_tokens,omitempty"`
}

// ChatResponse represents a response from the chat completions endpoint.
type ChatResponse struct {
	Choices []struct {
		Message ChatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
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

// Backend returns the name of the backend.
func (c *Client) Backend() string {
	return "openai"
}

// Complete sends a chat completion request and returns the response content.
// Schema is ignored: the reply is plain text.
func (c *Client) Complete(ctx context.Context, req llm.Request) (llm.Response, error) {
	model := req.Model
	if model == "" {
		model = c.model
	}

	chatReq := ChatRequest{Model: model, MaxTokens: req.MaxTokens}
	if len(req.System) > 0 {
		texts := make([]string, len(req.System))
		for i, b := range req.System {
			texts[i] = b.Text
		}
		chatReq.Messages = append(chatReq.Messages, ChatMessage{Role: "system", Content: strings.Join(texts, "\n\n")})
	}
	for _, m := range req.Messages {
		chatReq.Messages = append(chatReq.Messages, ChatMessage{Role: m.Role, Content: m.Content})
	}

	text, err := c.send(ctx, chatReq)
	if err != nil {
		return llm.Response{}, err
	}
	return llm.Response{Text: text, Model: model}, nil
}

func (c *Client) send(ctx context.Context, req ChatRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	url := c.baseURL + "/v1/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		msg := string(respBody)
		if len(msg) > maxErrorBytes {
			msg = msg[:maxErrorBytes] + "... (truncated)"
		}
		return "", fmt.Errorf("API error (status %d): %s", resp.StatusCode, msg)
	}

	if len(respBody) > maxResponseBytes {
		return "", fmt.Errorf("response exceeds %d bytes", maxResponseBytes)
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if chatResp.Error != nil {
		return "", fmt.Errorf("API error: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	return chatResp.Choices[0].Message.Content, nil
}
