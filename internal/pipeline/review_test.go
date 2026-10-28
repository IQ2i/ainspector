package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/findings"
	"github.com/iq2i/ainspector/internal/llm"
	"github.com/iq2i/ainspector/internal/modes/quality"
	"github.com/iq2i/ainspector/internal/state"
)

type fakeClient struct {
	mu       sync.Mutex
	requests []llm.Request
	reply    func(call int, req llm.Request) (string, error)
	usage    llm.Usage
}

func (f *fakeClient) Backend() string { return "fake" }

func (f *fakeClient) Complete(ctx context.Context, req llm.Request) (llm.Response, error) {
	f.mu.Lock()
	f.requests = append(f.requests, req)
	call := len(f.requests)
	f.mu.Unlock()

	text, err := f.reply(call, req)
	resp := llm.Response{Text: text, Usage: f.usage, Model: "fake-model"}
	if req.Schema != nil && json.Valid([]byte(text)) {
		resp.JSON = json.RawMessage(text)
	}
	return resp, err
}

func lgtm(int, llm.Request) (string, error) { return `{"issues":[]}`, nil }

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
	want := state.UnitHash(quality.New("", nil), unit, state.RulesDigest(nil))
	if f.UnitHash != want {
		t.Errorf("expected unit hash %s, got %s", want, f.UnitHash)
	}
}

func TestReviewUnits_ContinuesAfterFailure(t *testing.T) {
	client := &fakeClient{reply: func(call int, req llm.Request) (string, error) {
		if strings.Contains(req.Messages[0].Content, "Function: b ") {
			return "", errors.New("server error")
		}
		return `{"issues":[]}`, nil
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
	pc := &collect.ProjectContext{Description: "A web app"}
	mode := quality.New("some-model", []string{"No globals"})

	ReviewUnits(context.Background(), client, mode, []collect.ReviewUnit{goUnit("hello")}, pc)

	req := client.requests[0]
	if req.Model != "some-model" {
		t.Errorf("expected mode model, got %q", req.Model)
	}
	var texts []string
	for _, b := range req.System {
		texts = append(texts, b.Text)
	}
	system := strings.Join(texts, "\n")
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

func manyUnits(n int) []collect.ReviewUnit {
	units := make([]collect.ReviewUnit, n)
	for i := range units {
		units[i] = goUnit(fmt.Sprintf("fn%02d", i))
	}
	return units
}

func TestReviewUnits_ParallelKeepsOrderAndBoundsConcurrency(t *testing.T) {
	var running, peak atomic.Int32
	client := &fakeClient{reply: func(call int, req llm.Request) (string, error) {
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		// Later units finish first, to prove the order does not come from completion order
		time.Sleep(time.Duration(20-call) * time.Millisecond)
		running.Add(-1)
		return `{"issues":[{"line":2,"description":"issue","suggestion":""}]}`, nil
	}}
	units := manyUnits(12)

	results := ReviewUnits(context.Background(), client, quality.New("", nil), units, nil)

	for i, r := range results {
		if r.Unit.Name != units[i].Name || len(r.Findings) != 1 || r.Err != nil {
			t.Fatalf("result %d does not match unit %s: %+v", i, units[i].Name, r)
		}
	}
	if p := peak.Load(); p > maxWorkers || p < 2 {
		t.Errorf("expected between 2 and %d concurrent calls, got %d", maxWorkers, p)
	}
}

func TestReviewUnits_FirstUnitRunsAlone(t *testing.T) {
	var firstDone, violated atomic.Bool
	client := &fakeClient{reply: func(call int, req llm.Request) (string, error) {
		if call == 1 {
			time.Sleep(30 * time.Millisecond)
			firstDone.Store(true)
		} else if !firstDone.Load() {
			violated.Store(true)
		}
		return `{"issues":[]}`, nil
	}}

	ReviewUnits(context.Background(), client, quality.New("", nil), manyUnits(6), nil)

	if violated.Load() {
		t.Error("the first call must complete before the others start, so that they can read its cache")
	}
}

func TestReviewUnits_NoUnits(t *testing.T) {
	client := &fakeClient{reply: lgtm}
	if results := ReviewUnits(context.Background(), client, quality.New("", nil), nil, nil); len(results) != 0 || len(client.requests) != 0 {
		t.Errorf("nothing to review, got %d results and %d requests", len(results), len(client.requests))
	}
}

func TestReviewUnits_BudgetSkipsTheRemainingUnits(t *testing.T) {
	inner := &fakeClient{reply: lgtm, usage: llm.Usage{Input: 100, Output: 10}}
	client := llm.WithBudget(inner, 50, nil, nil)

	results := ReviewUnits(context.Background(), client, quality.New("", nil), manyUnits(5), nil)

	if results[0].Err != nil || results[0].Skipped {
		t.Errorf("the first unit fits the budget, got %+v", results[0])
	}
	for _, r := range results[1:] {
		if !r.Skipped || !errors.Is(r.Err, llm.ErrBudgetExceeded) {
			t.Errorf("unit %s should be skipped for the budget, got %+v", r.Unit.Name, r)
		}
	}
	if len(inner.requests) != 1 {
		t.Errorf("a skipped unit must not reach the backend, got %d requests", len(inner.requests))
	}
}

func TestReviewUnits_ResultsCarryHashAndModel(t *testing.T) {
	client := &fakeClient{reply: lgtm}
	mode := quality.New("", []string{"rule"})
	unit := goUnit("hello")

	results := ReviewUnits(context.Background(), client, mode, []collect.ReviewUnit{unit}, nil)

	if want := state.UnitHash(mode, unit, state.RulesDigest([]string{"rule"})); results[0].Hash != want || results[0].Model != "fake-model" {
		t.Errorf("unexpected hash or model: %+v", results[0])
	}
}
