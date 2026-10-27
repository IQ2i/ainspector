package openai

import (
	"encoding/json"
	"testing"
)

func TestExtractJSON(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer"},"tags":{"type":"array","items":{"type":"string"}}},"required":["n"]}`)

	valid := map[string]string{
		"plain":      `{"n": 1}`,
		"fenced":     "```json\n{\"n\": 1}\n```",
		"with prose": "Result: {\"n\": 1} hope it helps",
		"nested":     `{"n": 1, "tags": ["a"]}`,
	}
	for name, text := range valid {
		t.Run(name, func(t *testing.T) {
			if _, err := extractJSON(text, schema); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}

	invalid := map[string]string{
		"not json":      "nothing here",
		"unbalanced":    `{"n": `,
		"array":         `[1]`,
		"missing":       `{}`,
		"float":         `{"n": 1.5}`,
		"bad item type": `{"n": 1, "tags": [3]}`,
	}
	for name, text := range invalid {
		t.Run(name, func(t *testing.T) {
			if _, err := extractJSON(text, schema); err == nil {
				t.Errorf("expected an error for %q", text)
			}
		})
	}
}

func TestExtractJSON_InvalidSchema(t *testing.T) {
	if _, err := extractJSON(`{}`, json.RawMessage(`not json`)); err == nil {
		t.Error("expected an error for an invalid schema")
	}
}

func TestExtractJSON_CompactsTheDocument(t *testing.T) {
	got, err := extractJSON("{\n  \"n\": 1\n}", json.RawMessage(`{"type":"object"}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"n":1}` {
		t.Errorf("unexpected document %s", got)
	}
}
