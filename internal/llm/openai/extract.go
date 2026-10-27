package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// extractJSON returns the JSON document of a reply and checks it against the schema.
// Endpoints that fall back to plain text may wrap the document in prose or a code fence:
// the document is then taken between the first "{" and the last "}".
func extractJSON(text string, schema json.RawMessage) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(text)

	var doc any
	if err := json.Unmarshal([]byte(trimmed), &doc); err != nil {
		start, end := strings.Index(trimmed, "{"), strings.LastIndex(trimmed, "}")
		if start < 0 || end <= start {
			return nil, fmt.Errorf("response is not a JSON document")
		}
		trimmed = trimmed[start : end+1]
		if err := json.Unmarshal([]byte(trimmed), &doc); err != nil {
			return nil, fmt.Errorf("response is not a valid JSON document: %w", err)
		}
	}

	var s map[string]any
	if err := json.Unmarshal(schema, &s); err != nil {
		return nil, fmt.Errorf("invalid schema: %w", err)
	}
	if err := validate(doc, s, "$"); err != nil {
		return nil, fmt.Errorf("response does not match the schema: %w", err)
	}

	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(trimmed)); err != nil {
		return nil, err
	}
	return compact.Bytes(), nil
}

// validate checks the parts of a JSON schema that matter for a reply: types, required keys,
// properties and array items.
func validate(v any, schema map[string]any, path string) error {
	if t, ok := schema["type"].(string); ok && !hasType(v, t) {
		return fmt.Errorf("%s: expected %s", path, t)
	}

	switch val := v.(type) {
	case map[string]any:
		if required, ok := schema["required"].([]any); ok {
			for _, r := range required {
				key, _ := r.(string)
				if _, present := val[key]; !present {
					return fmt.Errorf("%s: missing required key %q", path, key)
				}
			}
		}
		props, _ := schema["properties"].(map[string]any)
		for key, sub := range props {
			subSchema, ok := sub.(map[string]any)
			if !ok {
				continue
			}
			if child, present := val[key]; present {
				if err := validate(child, subSchema, path+"."+key); err != nil {
					return err
				}
			}
		}
	case []any:
		if items, ok := schema["items"].(map[string]any); ok {
			for i, item := range val {
				if err := validate(item, items, fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func hasType(v any, t string) bool {
	switch t {
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "number":
		_, ok := v.(float64)
		return ok
	case "integer":
		f, ok := v.(float64)
		return ok && f == float64(int64(f))
	case "null":
		return v == nil
	}
	return true
}
