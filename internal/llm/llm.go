package llm

import (
	"context"
	"encoding/json"
)

// Block is a part of the system prompt. Cache marks the end of the stable prefix.
type Block struct {
	Text  string
	Cache bool
}

// Message is a conversation turn.
type Message struct {
	Role    string
	Content string
}

// Schema constrains the output of a request to a JSON document.
type Schema struct {
	Name string
	JSON json.RawMessage
}

// Request is a backend-agnostic completion request.
type Request struct {
	Model     string
	System    []Block
	Messages  []Message
	Schema    *Schema
	MaxTokens int
}

// Usage counts the tokens consumed by a request.
type Usage struct {
	Input, Output, CacheRead, CacheWrite int
}

// Response is a backend-agnostic completion response.
type Response struct {
	Text      string
	JSON      json.RawMessage
	Usage     Usage
	Model     string
	Truncated bool
}

// Client sends completion requests to an LLM backend.
type Client interface {
	// Complete returns an error if the backend fails; with a Schema it returns valid JSON or an error.
	Complete(ctx context.Context, req Request) (Response, error)
	Backend() string
}
