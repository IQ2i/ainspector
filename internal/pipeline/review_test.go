package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/iq2i/ainspector/internal/cache"
	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/findings"
	"github.com/iq2i/ainspector/internal/llm"
	"github.com/iq2i/ainspector/internal/modes/quality"
)

type fakeClient struct {
	requests []llm.Request
	reply    func(call int, req llm.Request) (string, error)
}

func (f *fakeClient) Backend() string { return "fake" }

func (f *fakeClient) Complete(ctx context.Context, req llm.Request) (llm.Response, error) {
	f.requests = append(f.requests, req)
	text, err := f.reply(len(f.requests), req)
	return llm.Response{Text: text}, err
}

func lgtm(int, llm.Request) (string, error) { return "LGTM", nil }

func goUnit(name string) collect.ReviewUnit {
	return collect.ReviewUnit{
		Kind:       collect.KindFunction,
		Name:       name,
		StartLine:  1,
		EndLine:    5,
		Content:    "func " + name + "() {}",
		Diff:       "+func " + name + "() {}",
		FilePath:   "main.go",
		Language:   "go",
		ChangeType: "modified",
	}
}

func TestReviewUnits_LGTM(t *testing.T) {
	client := &fakeClient{reply: lgtm}

	results := ReviewUnits(context.Background(), client, quality.New("", nil), []collect.ReviewUnit{goUnit("hello")}, nil)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err != nil || len(results[0].Findings) != 0 {
		t.Errorf("expected no error and no findings, got %+v", results[0])
	}
	if results[0].Unit.Name != "hello" {
		t.Errorf("expected unit to be preserved, got %s", results[0].Unit.Name)
	}
}

func TestReviewUnits_FindingsCarryUnitHash(t *testing.T) {
	client := &fakeClient{reply: func(int, llm.Request) (string, error) {
		return `{"issues":[{"line":3,"description":"Bug here","suggestion":"fix()"}]}`, nil
	}}
	unit := goUnit("hello")

	results := ReviewUnits(context.Background(), client, quality.New("", nil), []collect.ReviewUnit{unit}, nil)

	if len(results[0].Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(results[0].Findings))
	}
	f := results[0].Findings[0]
	if f.Mode != "quality" || f.File != "main.go" || f.StartLine != 3 || f.EndLine != 3 {
		t.Errorf("unexpected finding location: %+v", f)
	}
	if f.Explanation != "Bug here" || f.Suggestion != "fix()" {
		t.Errorf("unexpected finding content: %+v", f)
	}
	if f.UnitHash != cache.FunctionHash(&unit) {
		t.Errorf("expected unit hash %s, got %s", cache.FunctionHash(&unit), f.UnitHash)
	}
}

func TestReviewUnits_ContinuesAfterFailure(t *testing.T) {
	client := &fakeClient{reply: func(call int, req llm.Request) (string, error) {
		if call == 2 {
			return "", errors.New("server error")
		}
		return "LGTM", nil
	}}
	units := []collect.ReviewUnit{goUnit("a"), goUnit("b"), goUnit("c")}

	results := ReviewUnits(context.Background(), client, quality.New("", nil), units, nil)

	if len(results) != 3 || len(client.requests) != 3 {
		t.Fatalf("expected 3 results and 3 calls, got %d and %d", len(results), len(client.requests))
	}
	if results[0].Err != nil || results[2].Err != nil {
		t.Errorf("first and third units should succeed: %v / %v", results[0].Err, results[2].Err)
	}
	if results[1].Err == nil {
		t.Error("second unit should carry the error")
	}
}

func TestReviewUnits_EmptyList(t *testing.T) {
	client := &fakeClient{reply: func(int, llm.Request) (string, error) {
		t.Error("client must not be called for an empty list")
		return "", nil
	}}

	if results := ReviewUnits(context.Background(), client, quality.New("", nil), nil, nil); len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestReviewUnits_RequestFromMode(t *testing.T) {
	client := &fakeClient{reply: lgtm}
	pc := &collect.ProjectContext{Description: "A web app", IsRaw: true}
	mode := quality.New("some-model", []string{"No globals"})

	ReviewUnits(context.Background(), client, mode, []collect.ReviewUnit{goUnit("hello")}, pc)

	req := client.requests[0]
	if req.Model != "some-model" {
		t.Errorf("expected mode model, got %q", req.Model)
	}
	system := req.System[0].Text
	for _, want := range []string{"A web app", "No globals", "LANGUAGE-SPECIFIC CHECKS FOR GO"} {
		if !strings.Contains(system, want) {
			t.Errorf("system prompt should contain %q", want)
		}
	}
	if len(req.Messages) != 1 || !strings.Contains(req.Messages[0].Content, "Function: hello (lines 1-5)") {
		t.Errorf("unexpected user message: %+v", req.Messages)
	}
}

type failingParseMode struct{ *quality.Mode }

func (failingParseMode) Parse(llm.Response, collect.ReviewUnit) ([]findings.Finding, error) {
	return nil, errors.New("bad output")
}

func TestReviewUnits_ParseError(t *testing.T) {
	client := &fakeClient{reply: lgtm}
	mode := failingParseMode{quality.New("", nil)}

	results := ReviewUnits(context.Background(), client, mode, []collect.ReviewUnit{goUnit("hello")}, nil)

	if results[0].Err == nil || len(results[0].Findings) != 0 {
		t.Errorf("expected parse error to be recorded, got %+v", results[0])
	}
}
