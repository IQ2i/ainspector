package quality

import (
	"strings"
	"testing"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/llm"
)

func TestMode_Identity(t *testing.T) {
	m := New("some-model", nil)

	if m.Name() != "quality" || m.Model() != "some-model" || m.Version() != "quality/1" {
		t.Errorf("unexpected identity: %s %s %s", m.Name(), m.Model(), m.Version())
	}
	if m.Scope() != collect.ScopeFunction {
		t.Errorf("expected function scope, got %s", m.Scope())
	}
	if p := m.FilterPolicy(); p.MaxPublished != 0 || p.MinConfidence != 0 || p.MinSeverity != "" {
		t.Errorf("quality must not filter findings, got %+v", p)
	}
}

func TestMode_Parse(t *testing.T) {
	m := New("", nil)
	u := collect.ReviewUnit{FilePath: "pkg/a.go"}

	got, err := m.Parse(llm.Response{Text: `{"issues":[{"line":7,"description":"Nil deref","suggestion":"check it"},{"line":9,"description":"Leak","suggestion":""}]}`}, u)
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

func TestMode_ParseLGTM(t *testing.T) {
	got, err := New("", nil).Parse(llm.Response{Text: " LGTM\n"}, collect.ReviewUnit{})
	if err != nil || len(got) != 0 {
		t.Errorf("expected no findings, got %v / %v", got, err)
	}
}

func TestMode_BuildRequest(t *testing.T) {
	u := collect.ReviewUnit{Name: "f", FilePath: "a.py", Language: "python", StartLine: 1, EndLine: 2, Content: "def f(): pass", ChangeType: "added"}

	req := New("m", []string{"Rule one"}).BuildRequest(u, nil)

	if req.Model != "m" || len(req.System) != 1 || len(req.Messages) != 1 || req.Messages[0].Role != "user" {
		t.Fatalf("unexpected request shape: %+v", req)
	}
	if !strings.Contains(req.System[0].Text, "Rule one") || !strings.Contains(req.System[0].Text, "FOR PYTHON") {
		t.Error("system prompt should contain rules and language checks")
	}
	if req.System[0].Text != buildSystemPrompt("python", nil, []string{"Rule one"}) {
		t.Error("system prompt should be the legacy one, byte for byte")
	}
}
