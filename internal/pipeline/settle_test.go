package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/findings"
	"github.com/iq2i/ainspector/internal/llm"
	"github.com/iq2i/ainspector/internal/modes"
	"github.com/iq2i/ainspector/internal/modes/quality"
	"github.com/iq2i/ainspector/internal/modes/security"
	"github.com/iq2i/ainspector/internal/provider"
	"github.com/iq2i/ainspector/internal/publish"
	"github.com/iq2i/ainspector/internal/state"
)

type scriptedClient struct {
	backend string
	reply   string
	err     error
	request *llm.Request
}

func (c *scriptedClient) Complete(ctx context.Context, req llm.Request) (llm.Response, error) {
	c.request = &req
	if c.err != nil {
		return llm.Response{}, c.err
	}
	return llm.Response{JSON: json.RawMessage(c.reply), Model: "served"}, nil
}

func finding(fp string, sev findings.Severity, conf float64) findings.Finding {
	return findings.Finding{Mode: "security", File: "a.php", StartLine: 3, EndLine: 3, Severity: sev, Confidence: conf, Title: "t " + fp, Category: "idor", Scenario: "s", UnitHash: "h1", Fingerprint: fp}
}

func settleResults(fs ...findings.Finding) []UnitResult {
	return []UnitResult{
		{Unit: collect.ReviewUnit{FilePath: "a.php", Name: "show"}, Hash: "h1", Findings: fs},
		{Unit: collect.ReviewUnit{FilePath: "b.php", Name: "failed"}, Hash: "h2", Err: errors.New("boom"), Findings: []findings.Finding{finding("zzz", findings.SeverityCritical, 1)}},
	}
}

func securityMode(max int, fpModel string) *security.Mode {
	return security.New(security.Config{Scope: collect.ScopeFunctionClass, MaxFindings: max, FalsePositive: findings.FPConfig{Model: fpModel}})
}

func TestSettleFindings_ThresholdDedupFilterAndCap(t *testing.T) {
	results := settleResults(
		finding("aaaaaaaaaaaa", findings.SeverityCritical, 0.9),
		finding("bbbbbbbbbbbb", findings.SeverityInfo, 0.9),
		finding("cccccccccccc", findings.SeverityHigh, 0.2),
		finding("dddddddddddd", findings.SeverityHigh, 0.9),
		finding("eeeeeeeeeeee", findings.SeverityMedium, 0.9),
		finding("ffffffffffff", findings.SeverityLow, 0.9),
	)
	// dddd was reported by a previous run, eeee is judged a false positive
	block := state.FormatFindingMarker("dddddddddddd")
	index := state.LoadIndex([]provider.ExistingComment{{Author: "bot", Body: block}}, "bot")
	client := &scriptedClient{backend: "anthropic", reply: `{"verdicts":[{"id":0,"keep":true,"reason":"r"},{"id":1,"keep":false,"reason":"generic"},{"id":2,"keep":true,"reason":"r"}]}`}

	got := settle(context.Background(), client, securityMode(1, ""), results, index)

	if len(got.Published) != 1 || got.Published[0].Fingerprint != "aaaaaaaaaaaa" {
		t.Errorf("published %+v", got.Published)
	}
	if len(got.Overflow) != 1 || got.Overflow[0].Fingerprint != "ffffffffffff" {
		t.Errorf("overflow %+v", got.Overflow)
	}
	if got.Discarded != 1 || got.FPModel != "claude-opus-5-5" {
		t.Errorf("discarded %d by %q", got.Discarded, got.FPModel)
	}
	if client.request.Model != "claude-opus-5-5" {
		t.Errorf("the filter must use the stronger model on the anthropic backend, got %q", client.request.Model)
	}
	if prompt := client.request.Messages[0].Content; strings.Contains(prompt, "id 3") || strings.Contains(prompt, "t zzz") {
		t.Errorf("thresholded, duplicate and failed findings must not reach the filter:\n%s", prompt)
	}
}

func TestSettleFindings_FilterOnlyJudgesTheMostSevere(t *testing.T) {
	var fs []findings.Finding
	for i := 0; i < 8; i++ {
		sev := findings.SeverityLow
		if i < 3 {
			sev = findings.SeverityHigh
		}
		fs = append(fs, finding(strings.Repeat(string(rune('a'+i)), 12), sev, 0.9))
	}
	client := &scriptedClient{backend: "anthropic", reply: `{"verdicts":[{"id":0,"keep":false,"reason":"r"}]}`}

	got := settle(context.Background(), client, securityMode(2, ""), settleResults(fs...), nil)

	// 2 published x 3 = 6 judged, the 2 least severe go straight to the overflow
	prompt := client.request.Messages[0].Content
	if !strings.Contains(prompt, "id 5:") || strings.Contains(prompt, "id 6:") {
		t.Errorf("the filter must receive 6 findings:\n%s", prompt)
	}
	if len(got.Published) != 2 || got.Discarded != 1 {
		t.Errorf("published %d, discarded %d", len(got.Published), got.Discarded)
	}
	if len(got.Overflow) != 3 || len(got.Unjudged) != 2 {
		t.Errorf("3 judged findings left over and 2 never judged, got %d in overflow and %d unjudged", len(got.Overflow), len(got.Unjudged))
	}
}

func TestSettleFindings_FilterModelOnOtherBackends(t *testing.T) {
	client := &scriptedClient{backend: "openai", reply: `{"verdicts":[]}`}
	got := settle(context.Background(), client, securityMode(5, ""), settleResults(finding("aaaaaaaaaaaa", findings.SeverityHigh, 0.9)), nil)
	if client.request.Model != "" || got.FPModel != "" || len(got.Published) != 1 {
		t.Errorf("an openai-compatible backend keeps its default model, got %q / %+v", client.request.Model, got)
	}

	client = &scriptedClient{backend: "anthropic", reply: `{"verdicts":[]}`}
	settle(context.Background(), client, securityMode(5, "my-model"), settleResults(finding("aaaaaaaaaaaa", findings.SeverityHigh, 0.9)), nil)
	if client.request.Model != "my-model" {
		t.Errorf("the configured model wins, got %q", client.request.Model)
	}
}

func TestSettleFindings_FilterFailureKeepsEverything(t *testing.T) {
	client := &scriptedClient{backend: "anthropic", err: errors.New("budget")}
	results := settleResults(finding("aaaaaaaaaaaa", findings.SeverityHigh, 0.9), finding("bbbbbbbbbbbb", findings.SeverityMedium, 0.9), finding("cccccccccccc", findings.SeverityLow, 0.9))

	got := settle(context.Background(), client, securityMode(2, ""), results, nil)

	if len(got.Published) != 2 || len(got.Overflow) != 1 || got.Discarded != 0 {
		t.Errorf("findings must be kept and still capped: %+v", got)
	}
	if len(got.Notes) != 1 || !strings.Contains(got.Notes[0], "false positive filter failed") {
		t.Errorf("the summary must say the filter did not run: %v", got.Notes)
	}
}

func TestSettleFindings_NoFilterCallWithoutFindings(t *testing.T) {
	client := &scriptedClient{backend: "anthropic"}
	got := settle(context.Background(), client, securityMode(5, ""), settleResults(), nil)
	if client.request != nil || len(got.Published) != 0 {
		t.Errorf("no call expected, got %+v", got)
	}
}

func TestSettleFindings_QualityIsLeftUntouched(t *testing.T) {
	// no fingerprint, no severity: neither dedup, threshold, cap nor reordering may apply
	var fs []findings.Finding
	for _, f := range []string{"z.go", "a.go", "m.go"} {
		fs = append(fs, findings.Finding{Mode: "quality", File: f, StartLine: 1, Explanation: f})
	}
	client := &scriptedClient{backend: "anthropic"}

	got := settle(context.Background(), client, quality.New("", nil, false), settleResults(fs...), nil)
	if len(got.Published) != 3 || got.Published[0].File != "z.go" || got.Published[2].File != "m.go" || got.Overflow != nil || client.request != nil {
		t.Errorf("unexpected outcome %+v", got)
	}
}

func TestCollectRequest_MergesTheNeedsOfTheModes(t *testing.T) {
	sec := security.New(security.Config{Scope: collect.ScopeFunctionClass, Hunks: collect.HunkPolicy{Extensions: []string{"twig", "yaml"}, Before: 5, After: 1}})
	q := quality.New("", nil, false)

	req := collectRequest(nil, []modes.Mode{q, sec})
	if req.Scope != collect.ScopeFunctionClass || !req.MatchDeleted || !req.Hunks.Orphans || req.Hunks.Before != 5 || req.Hunks.After != 1 || len(req.Hunks.Extensions) != 2 {
		t.Errorf("unexpected request %+v", req)
	}

	req = collectRequest(nil, []modes.Mode{q})
	if req.Scope != collect.ScopeFunction || req.MatchDeleted || req.Hunks.Orphans || len(req.Hunks.Extensions) != 0 {
		t.Errorf("quality alone asks for nothing more than before: %+v", req)
	}
}

func TestUnitsFor(t *testing.T) {
	enclosing := &collect.ClassHeader{Name: "A", Rendered: "class A {}"}
	units := []collect.ReviewUnit{
		{Kind: collect.KindFunction, Name: "show", Enclosing: enclosing},
		{Kind: collect.KindFunction, Name: "removed", Enclosing: enclosing, DeletedOnly: true},
		{Kind: collect.KindHunk, Name: "lines 1-3"},
	}

	got := unitsFor(quality.New("", nil, false), units)
	if len(got) != 1 || got[0].Name != "show" || got[0].Enclosing != nil {
		t.Errorf("quality only reviews functions with added lines, without class header: %+v", got)
	}
	if units[0].Enclosing == nil {
		t.Error("the collected units must not be modified")
	}

	got = unitsFor(securityMode(5, ""), units)
	if len(got) != 3 || got[0].Enclosing == nil {
		t.Errorf("security reviews every unit with its class header: %+v", got)
	}

	flat := security.New(security.Config{Scope: collect.ScopeFunction})
	if got := unitsFor(flat, units); got[0].Enclosing != nil {
		t.Error("a function scope security mode gets no class header")
	}
}

func TestChangedPaths(t *testing.T) {
	got := changedPaths([]provider.ModifiedFile{{Path: "a"}, {Path: "b"}})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("got %v", got)
	}
}

func securityHarness(t *testing.T, name string) *harness {
	t.Helper()
	return newHarness(t, filepath.Join("..", "..", "testdata", "mrs", name))
}

func TestRun_SecurityOverflowIsListedInTheSummary(t *testing.T) {
	h := securityHarness(t, "security-removed-isgranted")
	h.deps.Config.Modes.Security.MaxFindings = 1

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}

	if len(h.fake.posted) != 1 || !strings.Contains(h.fake.posted[0].Body, "edit() lost its #[IsGranted] check") {
		t.Fatalf("only the most severe finding is commented, got %+v", h.fake.posted)
	}
	notes := h.fake.noteBodies()
	if len(notes) != 1 || !strings.Contains(notes[0], "Not commented, only the 1 most severe findings are published:\n- src/Controller/AccountController.php:11 [high] Class-level") {
		t.Errorf("the summary must list the finding left out:\n%v", notes)
	}
}

func TestRun_SecurityFilterFailureIsReportedAndKeepsFindings(t *testing.T) {
	h := securityHarness(t, "security-idor")
	h.responses[falsePositiveKey] = "this is not a verdict"

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatalf("a failing filter must not fail the run: %v", err)
	}

	if len(h.fake.posted) != 1 {
		t.Errorf("the finding must be kept, got %d comments", len(h.fake.posted))
	}
	if notes := h.fake.noteBodies(); len(notes) != 1 || !strings.Contains(notes[0], "The false positive filter failed") {
		t.Errorf("the summary must say the filter did not run:\n%v", notes)
	}
}

func TestRun_SecurityDiscardsWhatTheFilterRejects(t *testing.T) {
	h := securityHarness(t, "security-idor")
	h.responses[falsePositiveKey] = `{"verdicts":[{"id":0,"keep":false,"reason":"the route is protected by a firewall"}]}`

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}

	if len(h.fake.posted) != 0 {
		t.Errorf("a discarded finding must not be commented, got %+v", h.fake.posted)
	}
	if notes := h.fake.noteBodies(); len(notes) != 1 || !strings.Contains(notes[0], "1 finding discarded as false positive") {
		t.Errorf("the summary must count what was discarded:\n%v", notes)
	}
}

func TestRun_SecuritySecondRunReviewsNothing(t *testing.T) {
	h := securityHarness(t, "security-removed-isgranted")

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}
	if len(h.fake.posted) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(h.fake.posted))
	}

	h.keepPostedComments()
	h.fake.posted = nil
	h.requests = nil

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}
	if len(h.requests) != 0 || len(h.fake.posted) != 0 {
		t.Errorf("second run should do nothing, got %d requests and %d comments", len(h.requests), len(h.fake.posted))
	}
}

func TestRun_QualityAndSecurityShareTheUnits(t *testing.T) {
	h := securityHarness(t, "security-dql")
	h.deps.Config.Modes.Quality.Enabled = nil
	// one reply that both modes can read: each ignores the field of the other
	h.responses["findByName"] = `{"issues": [], "findings": []}`

	if err := Run(context.Background(), h.deps, Options{Number: 1, Modes: []string{"quality", "security"}}); err != nil {
		t.Fatal(err)
	}

	var quality, security int
	for _, r := range h.requests {
		switch {
		case strings.Contains(string(r), "application security engineer"):
			security++
		case strings.Contains(string(r), "expert code reviewer"):
			quality++
		}
	}
	if len(h.requests) != 2 || quality != 1 || security != 1 {
		t.Errorf("each mode reviews the unit once with its own prompt, got %d requests (%d quality, %d security)", len(h.requests), quality, security)
	}
	if notes := h.fake.noteBodies(); len(notes) != 2 || !strings.Contains(notes[0], "ainspector:summary:quality") || !strings.Contains(notes[1], "ainspector:summary:security") {
		t.Errorf("each mode keeps its own summary:\n%v", notes)
	}
}

// settle runs settleFindings on a fresh run and returns it
func settle(ctx context.Context, client llm.Client, mode modes.Mode, results []UnitResult, index *state.Index) publish.Run {
	// The anthropic backend is the one that is given a default model for the filter
	defaultFP := ""
	if sc, ok := client.(*scriptedClient); ok && sc.backend == "anthropic" {
		defaultFP = "claude-opus-5-5"
	}
	var run publish.Run
	settleFindings(ctx, client, defaultFP, mode, results, index, &run)
	return run
}

func TestRun_UsageOfAModeIncludesTheFalsePositiveFilter(t *testing.T) {
	h := securityHarness(t, "security-idor")
	h.responses[falsePositiveKey] = `{"verdicts":[{"id":0,"keep":true,"reason":"real"}]}`

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}

	var doc struct {
		Modes []struct {
			Usage struct {
				Input int `json:"input"`
			} `json:"usage"`
		} `json:"modes"`
	}
	if err := json.Unmarshal(h.report("ainspector-report.json"), &doc); err != nil || len(doc.Modes) != 1 {
		t.Fatalf("unreadable artifact: %v", err)
	}
	// The scripted backend bills 100 input tokens per call, the filter's call included
	if want := 100 * len(h.requests); doc.Modes[0].Usage.Input != want {
		t.Errorf("%d requests were made, the usage of the mode holds %d input tokens, want %d", len(h.requests), doc.Modes[0].Usage.Input, want)
	}
}
