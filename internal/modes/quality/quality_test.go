package quality

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/llm"
)

func TestMode_Identity(t *testing.T) {
	m := New("some-model", []string{"r1"})

	if m.Name() != "quality" || m.Model() != "some-model" || m.Version() != "quality/2" {
		t.Errorf("unexpected identity: %s %s %s", m.Name(), m.Model(), m.Version())
	}
	if len(m.Rules()) != 1 || m.Rules()[0] != "r1" {
		t.Errorf("unexpected rules: %v", m.Rules())
	}
	if m.Scope() != collect.ScopeFunction {
		t.Errorf("expected function scope, got %s", m.Scope())
	}
	if !m.PublishPolicy().Inline {
		t.Error("quality must publish inline comments")
	}
	if p := m.FilterPolicy(); p.MaxPublished != 0 || p.MinConfidence != 0 || p.MinSeverity != "" {
		t.Errorf("quality must not filter findings, got %+v", p)
	}
}

func TestMode_Parse(t *testing.T) {
	m := New("", nil)
	u := collect.ReviewUnit{FilePath: "pkg/a.go"}
	doc := `{"issues":[{"line":7,"description":"Nil deref","suggestion":"check it"},{"line":9,"description":"Leak","suggestion":""}]}`

	got, err := m.Parse(llm.Response{JSON: json.RawMessage(doc)}, u)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(got))
	}
	if got[0].File != "pkg/a.go" || got[0].StartLine != 7 || got[0].EndLine != 7 || got[0].Explanation != "Nil deref" || got[0].Suggestion != "check it" || got[0].Mode != "quality" {
		t.Errorf("unexpected first finding: %+v", got[0])
	}
	if got[1].Suggestion != "" {
		t.Errorf("expected empty suggestion, got %q", got[1].Suggestion)
	}
}

func TestMode_ParseNoIssues(t *testing.T) {
	got, err := New("", nil).Parse(llm.Response{JSON: json.RawMessage(`{"issues":[]}`)}, collect.ReviewUnit{})
	if err != nil || len(got) != 0 {
		t.Errorf("expected no findings, got %v / %v", got, err)
	}
}

func TestMode_ParseStructuredOutputIsNotCleaned(t *testing.T) {
	m := New("", nil)

	// A document that does not have the shape of the schema is an error, not something to salvage
	for _, doc := range []string{
		`Here it is: {"issues":[]}`,
		`{"issues":"none"}`,
		`{"issues":[{"line":"3","description":"d","suggestion":""}]}`,
	} {
		if _, err := m.Parse(llm.Response{JSON: json.RawMessage(doc)}, collect.ReviewUnit{}); err == nil {
			t.Errorf("expected an error for %q", doc)
		}
	}
}

// Backends without strict schemas may add fields: dropping the unit would fail it again at every run
func TestMode_ParseIgnoresUnknownFields(t *testing.T) {
	doc := `{"issues":[{"line":3,"description":"Bug","suggestion":"","severity":"high"}],"summary":"x"}`
	got, err := New("", nil).Parse(llm.Response{JSON: json.RawMessage(doc)}, collect.ReviewUnit{FilePath: "a.go"})
	if err != nil || len(got) != 1 || got[0].StartLine != 3 {
		t.Errorf("expected the issue to be parsed, got %+v / %v", got, err)
	}
}

func TestMode_ParseTextFallback(t *testing.T) {
	m := New("", nil)

	got, err := m.Parse(llm.Response{Text: " LGTM\n"}, collect.ReviewUnit{})
	if err != nil || len(got) != 0 {
		t.Errorf("expected no findings for LGTM, got %v / %v", got, err)
	}

	got, err = m.Parse(llm.Response{Text: "Review:\n{\"issues\":[{\"line\":3,\"description\":\"Bug\",\"suggestion\":\"\"}]}"}, collect.ReviewUnit{FilePath: "a.go"})
	if err != nil || len(got) != 1 || got[0].StartLine != 3 {
		t.Errorf("expected the issue to be salvaged from text, got %v / %v", got, err)
	}
}

func TestMode_BuildRequest(t *testing.T) {
	u := collect.ReviewUnit{Name: "f", FilePath: "a.py", Language: "python", StartLine: 1, EndLine: 2, Content: "def f(): pass", ChangeType: "added"}

	req := New("m", []string{"Rule one"}).BuildRequest(u, nil)

	if req.Model != "m" || len(req.System) != 2 || len(req.Messages) != 1 || req.Messages[0].Role != "user" {
		t.Fatalf("unexpected request shape: %+v", req)
	}
	if !req.System[0].Cache || req.System[1].Cache {
		t.Error("only the stable prefix must be cached")
	}
	if !strings.Contains(req.System[0].Text, "Rule one") || !strings.Contains(req.System[1].Text, "FOR PYTHON") {
		t.Error("the prefix should contain the rules and the second block the language checks")
	}
	if req.Schema == nil || req.Schema.Name != "code_review" || string(req.Schema.JSON) != string(reviewSchema) {
		t.Errorf("expected the review schema, got %+v", req.Schema)
	}
}
