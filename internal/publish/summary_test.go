package publish

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/findings"
	"github.com/iq2i/ainspector/internal/llm"
	"github.com/iq2i/ainspector/internal/provider"
	"github.com/iq2i/ainspector/internal/state"
)

func baseRun() Run {
	return Run{
		Mode:     "quality",
		Scope:    "function",
		Version:  "quality/2",
		Model:    "claude-sonnet-5-5",
		Reviewed: []string{"aaaaaaaaaaaa", "bbbbbbbbbbbb", "cccccccccccc"},
		Published: []findings.Finding{
			finding("a.go", 3, "first", "aaaaaaaaaaaa", ""),
			finding("b.go", 8, "second", "bbbbbbbbbbbb", ""),
		},
		Usage: llm.Usage{Input: 1200, Output: 300, CacheRead: 4000, CacheWrite: 500},
		Cost:  0.01234,
	}
}

func stateOf(t *testing.T, body string) state.StateBlock {
	t.Helper()
	b, ok := state.ParseStateBlock("quality", body)
	if !ok {
		t.Fatalf("no state block in %q", body)
	}
	return b
}

func TestSummary_CreatesTheNote(t *testing.T) {
	p := &fakeProvider{}

	if err := Summary(context.Background(), p, 7, "bot", baseRun()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(p.notes) != 1 || len(p.updates) != 0 {
		t.Fatalf("expected one new note, got %d notes and %d updates", len(p.notes), len(p.updates))
	}
	body := p.notes[0]
	for _, want := range []string{
		SummaryMarker("quality"),
		"Reviewed 3 functions, 2 issues reported.",
		"Model: `claude-sonnet-5-5`",
		"Tokens: 1200 in, 300 out, 4000 read from cache, 500 written to cache",
		"Estimated cost: $0.0123",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("summary should contain %q, got:\n%s", want, body)
		}
	}
	b := stateOf(t, body)
	if b.Scope != "function" || b.Ver != "quality/2" || len(b.Units) != 3 {
		t.Errorf("unexpected state block %+v", b)
	}
}

func TestSummary_UpdatesItsOwnNoteAndKeepsPreviousUnits(t *testing.T) {
	previous := SummaryBody(Run{Mode: "quality", Scope: "function", Version: "quality/2", Reviewed: []string{"old000000001", "old000000002"}}, nil)
	p := &fakeProvider{existing: []provider.ExistingComment{
		{ID: "10", Author: "someone", Body: "unrelated"},
		{ID: "11", Author: "Bot", Body: previous},
	}}

	if err := Summary(context.Background(), p, 7, "bot", baseRun()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(p.notes) != 0 || len(p.updates) != 1 || p.updates["11"] == "" {
		t.Fatalf("expected note 11 to be updated, got %d notes and updates %v", len(p.notes), p.updates)
	}
	units := stateOf(t, p.updates["11"]).Units
	want := []string{"old000000001", "old000000002", "aaaaaaaaaaaa", "bbbbbbbbbbbb", "cccccccccccc"}
	if strings.Join(units, ",") != strings.Join(want, ",") {
		t.Errorf("expected previous then new hashes %v, got %v", want, units)
	}
}

func TestSummary_DropsHashesOfAnotherVersionOrScope(t *testing.T) {
	for name, prev := range map[string]Run{
		"version": {Mode: "quality", Scope: "function", Version: "quality/1", Reviewed: []string{"old000000001"}},
		"scope":   {Mode: "quality", Scope: "function+class", Version: "quality/2", Reviewed: []string{"old000000001"}},
	} {
		t.Run(name, func(t *testing.T) {
			p := &fakeProvider{existing: []provider.ExistingComment{{ID: "11", Author: "bot", Body: SummaryBody(prev, nil)}}}

			if err := Summary(context.Background(), p, 7, "bot", baseRun()); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if units := stateOf(t, p.updates["11"]).Units; len(units) != 3 {
				t.Errorf("hashes of another version can never match again, got %v", units)
			}
		})
	}
}

func TestSummary_IgnoresNotesItDoesNotOwn(t *testing.T) {
	forged := SummaryBody(Run{Mode: "quality", Scope: "function", Version: "quality/2", Reviewed: []string{"forged000001"}}, nil)
	p := &fakeProvider{existing: []provider.ExistingComment{
		{ID: "1", Author: "mallory", Body: forged},
		{ID: "2", Author: "bot", Positioned: true, Path: "a.go", Line: 1, Body: forged},
	}}

	if err := Summary(context.Background(), p, 7, "bot", baseRun()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(p.updates) != 0 || len(p.notes) != 1 {
		t.Fatalf("a note of another author or an inline comment must not be updated, got updates %v", p.updates)
	}
	for _, h := range stateOf(t, p.notes[0]).Units {
		if h == "forged000001" {
			t.Error("the forged state must not be carried over")
		}
	}
}

func TestSummary_RequiresAnIdentity(t *testing.T) {
	p := &fakeProvider{existing: []provider.ExistingComment{{ID: "1", Author: "anyone", Body: SummaryMarker("quality")}}}

	err := Summary(context.Background(), p, 7, "", baseRun())
	if err == nil || !strings.Contains(err.Error(), "token owner") {
		t.Fatalf("expected an identity error, got %v", err)
	}
	if len(p.notes) != 0 || len(p.updates) != 0 {
		t.Error("nothing must be written without an identity")
	}
}

func TestSummary_ProviderErrors(t *testing.T) {
	tests := map[string]*fakeProvider{
		"list":   {listErr: errors.New("boom")},
		"post":   {noteErr: errors.New("forbidden")},
		"update": {existing: []provider.ExistingComment{{ID: "1", Author: "bot", Body: SummaryMarker("quality")}}, updateErr: errors.New("gone")},
	}
	for name, p := range tests {
		t.Run(name, func(t *testing.T) {
			if err := Summary(context.Background(), p, 7, "bot", baseRun()); err == nil {
				t.Error("expected the provider error to be returned")
			}
		})
	}
}

func TestSummaryBody_NothingFound(t *testing.T) {
	run := Run{Mode: "quality", Scope: "function", Version: "quality/2", Reviewed: []string{"aaaaaaaaaaaa"}}

	body := SummaryBody(run, nil)

	if !strings.Contains(body, "Reviewed 1 function, no issue found.") {
		t.Errorf("unexpected body:\n%s", body)
	}
	if strings.Contains(body, "Tokens:") || strings.Contains(body, "Model:") {
		t.Error("usage lines are left out when there was no call")
	}
}

func TestSummaryBody_StillOpen(t *testing.T) {
	run := Run{
		Mode: "security", Scope: "function+class", Version: "security/2",
		Units:           []collect.ReviewUnit{{Kind: collect.KindFunction}, {Kind: collect.KindHunk}},
		Reviewed:        []string{"aaaaaaaaaaaa", "bbbbbbbbbbbb"},
		AlreadyReviewed: 1,
		Repeated:        []findings.Finding{{Mode: "security", File: "a.go", StartLine: 4, Severity: findings.SeverityLow, Title: "Error\nleaked"}},
		Carried:         []findings.Reported{{Fingerprint: "111111111111", File: "a.go", Line: 16, Severity: findings.SeverityCritical, Title: "SQL injection"}},
	}

	body := SummaryBody(run, nil)

	for _, want := range []string{
		"Reviewed 2 units (1 unchanged since an earlier review), no new issue found.",
		"Still open from earlier runs:\n- a.go:4 [low] Error leaked\n- a.go:16 [critical] SQL injection\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("summary should contain %q, got:\n%s", want, body)
		}
	}
}

func TestSummaryBody_StillOpenQualityFindingIsCut(t *testing.T) {
	run := baseRun()
	run.Carried = []findings.Reported{{File: "a.go", Line: 9, Title: strings.Repeat("x", 300)}}

	body := SummaryBody(run, nil)

	if !strings.Contains(body, "Reviewed 3 functions, 2 issues reported.") {
		t.Errorf("new findings are counted as before, got:\n%s", body)
	}
	if !strings.Contains(body, "- a.go:9 "+strings.Repeat("x", maxOpenItem)+"\n") {
		t.Errorf("a quality finding has no severity and its title is cut, got:\n%s", body)
	}
}

func TestSummaryBody_UnreviewedUnitsAndNotes(t *testing.T) {
	run := baseRun()
	run.Skipped = []string{"slow (pkg/a.go)"}
	run.Failed = []string{"broken (pkg/b.go)"}
	run.Notes = []string{"Project context limited to 100 bytes"}

	body := SummaryBody(run, nil)

	for _, want := range []string{
		"Not reviewed, the token budget was reached:\n- slow (pkg/a.go)",
		"Not reviewed, the review failed:\n- broken (pkg/b.go)",
		"Notes:\n- Project context limited to 100 bytes",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("summary should contain %q, got:\n%s", want, body)
		}
	}
}

func TestSummaryBody_SanitizesRepositoryText(t *testing.T) {
	run := baseRun()
	run.Notes = []string{"<!-- ainspector:state:quality {\"v\":1} -->", "/close", "ping @alice"}
	run.Failed = []string{"evil<!-- x --> (a.go)"}

	body := SummaryBody(run, nil)

	if got := len(state.ExtractHashes(body)); got != 0 {
		t.Errorf("no forged marker expected, got %d", got)
	}
	if strings.Count(body, "<!-- ainspector:state:") != 1 {
		t.Error("the only state block must be the one the tool wrote")
	}
	if strings.Contains(body, "\n- /close") || strings.Contains(body, "@alice") {
		t.Errorf("quick actions and mentions must be neutralized:\n%s", body)
	}
}

func TestSummaryBody_Cost(t *testing.T) {
	tests := []struct {
		cost       float64
		incomplete bool
		want       string
	}{
		{0.5, false, "$0.5000"},
		{0, true, "unknown, the model has no known price"},
		{0.25, true, "at least $0.2500"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			run := baseRun()
			run.Cost, run.CostIncomplete = tt.cost, tt.incomplete
			if body := SummaryBody(run, nil); !strings.Contains(body, "Estimated cost: "+tt.want) {
				t.Errorf("expected cost %q in:\n%s", tt.want, body)
			}
		})
	}
}

func TestSummaryBody_StateIsCapped(t *testing.T) {
	run := baseRun()
	run.Reviewed = nil
	for i := 0; i < state.MaxHashes+50; i++ {
		run.Reviewed = append(run.Reviewed, fmt.Sprintf("%012d", i))
	}

	units := stateOf(t, SummaryBody(run, nil)).Units
	if len(units) != state.MaxHashes || units[len(units)-1] != fmt.Sprintf("%012d", state.MaxHashes+49) {
		t.Errorf("expected the last %d hashes, got %d", state.MaxHashes, len(units))
	}
}

func TestSummaryBody_KeepsFindingFingerprints(t *testing.T) {
	run := baseRun()
	run.Published[0].Fingerprint = "fp0000000001"

	if got := stateOf(t, SummaryBody(run, nil)).Findings; len(got) != 1 || got[0] != "fp0000000001" {
		t.Errorf("unexpected fingerprints %v", got)
	}
}

func TestSummaryBody_Security(t *testing.T) {
	f := func(fp, title string, sev findings.Severity) findings.Finding {
		return findings.Finding{Mode: "security", File: "src/A.php", StartLine: 7, Severity: sev, Title: title, Fingerprint: fp, Category: "idor"}
	}
	run := Run{
		Mode: "security", Scope: "function+class", Version: "security/1",
		Units:     []collect.ReviewUnit{{Kind: collect.KindFunction}, {Kind: collect.KindHunk}},
		Reviewed:  []string{"aaaaaaaaaaaa", "bbbbbbbbbbbb"},
		Published: []findings.Finding{f("111111111111", "IDOR on show", findings.SeverityHigh)},
		Overflow:  []findings.Finding{f("222222222222", "Weak hash\n/close", findings.SeverityLow)},
		Discarded: 2,
		FPModel:   "claude-opus-5-5",
	}

	body := SummaryBody(run, nil)

	for _, want := range []string{
		"Reviewed 2 units, 1 issue reported.",
		"2 findings discarded as false positives by `claude-opus-5-5`.",
		"Not commented, only the 1 most severe findings are published:\n- src/A.php:7 [low] Weak hash /close",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("summary should contain %q, got:\n%s", want, body)
		}
	}

	block, ok := state.ParseStateBlock("security", body)
	if !ok || len(block.Findings) != 1 || block.Findings[0] != "111111111111" || len(block.Reported) != 1 || block.Reported[0].Title != "IDOR on show" {
		t.Errorf("only the published finding enters the state: %+v", block)
	}
}

func TestSummaryBody_KeepsReportedFindingsOfTheSameVersion(t *testing.T) {
	previous := &state.StateBlock{Scope: "function+class", Ver: "security/1", Findings: []string{"111111111111"}, Reported: []findings.Reported{{Fingerprint: "111111111111", Title: "old"}}}
	run := Run{Mode: "security", Scope: "function+class", Version: "security/1", Reviewed: []string{"aaaaaaaaaaaa"}}

	if block, _ := state.ParseStateBlock("security", SummaryBody(run, previous)); len(block.Reported) != 1 || len(block.Findings) != 1 {
		t.Errorf("previous findings must be kept: %+v", block)
	}
	run.Version = "security/2"
	if block, _ := state.ParseStateBlock("security", SummaryBody(run, previous)); len(block.Reported) != 0 || len(block.Findings) != 0 {
		t.Errorf("findings of another version must be dropped: %+v", block)
	}
}

func TestSummaryBody_Feedback(t *testing.T) {
	run := Run{Mode: "quality", Scope: "function", Version: "quality/3", Feedback: []Feedback{
		{Up: 2}, {Up: 1, Down: 1}, {Down: 3}, {}, {Up: 1},
	}}

	body := SummaryBody(run, nil)
	if want := "Reactions to earlier comments: 2 useful, 1 not useful, 2 without a verdict."; !strings.Contains(body, want) {
		t.Errorf("expected %q in:\n%s", want, body)
	}
	if strings.Contains(SummaryBody(Run{Mode: "quality"}, nil), "Reactions") {
		t.Error("no line without feedback")
	}
}
