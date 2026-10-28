package publish

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/findings"
	"github.com/iq2i/ainspector/internal/provider"
	"github.com/iq2i/ainspector/internal/state"
)

type fakeProvider struct {
	provider.Provider
	reviewFn func(comments []provider.ReviewComment) ([]provider.CommentResult, error)
	noteErr  error
	notes    []string
	reviews  [][]provider.ReviewComment

	existing  []provider.ExistingComment
	listErr   error
	updateErr error
	updates   map[string]string
}

func (f *fakeProvider) ListComments(ctx context.Context, number int) ([]provider.ExistingComment, error) {
	return f.existing, f.listErr
}

func (f *fakeProvider) UpdateComment(ctx context.Context, number int, id, body string) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	if f.updates == nil {
		f.updates = map[string]string{}
	}
	f.updates[id] = body
	return nil
}

func (f *fakeProvider) CreateReview(ctx context.Context, number int, comments []provider.ReviewComment) ([]provider.CommentResult, error) {
	f.reviews = append(f.reviews, comments)
	return f.reviewFn(comments)
}

func (f *fakeProvider) PostComment(ctx context.Context, number int, body string) (string, error) {
	if f.noteErr != nil {
		return "", f.noteErr
	}
	f.notes = append(f.notes, body)
	return "1", nil
}

func allOK(comments []provider.ReviewComment) ([]provider.CommentResult, error) {
	results := make([]provider.CommentResult, len(comments))
	for i, c := range comments {
		results[i] = provider.CommentResult{Comment: c}
	}
	return results, nil
}

func finding(file string, line int, text, hash, suggestion string) findings.Finding {
	return findings.Finding{Mode: "quality", File: file, StartLine: line, EndLine: line, Explanation: text, UnitHash: hash, Suggestion: suggestion}
}

var testUnits = []collect.ReviewUnit{
	{FilePath: "a.go", StartLine: 1, EndLine: 10},
	{FilePath: "b.go", StartLine: 5, EndLine: 12},
}

func TestBodyFor(t *testing.T) {
	got := BodyFor(finding("a.go", 1, "Nil deref", "abc123def456", ""))
	want := "Nil deref\n\n<!-- ainspector:fn:abc123def456 -->"
	if got != want {
		t.Errorf("BodyFor = %q, want %q", got, want)
	}
}

func TestInline_PostsReview(t *testing.T) {
	p := &fakeProvider{reviewFn: allOK}

	err := Inline(context.Background(), p, 5, Run{Units: testUnits, Published: []findings.Finding{
		finding("a.go", 3, "first", "aaaaaaaaaaaa", "fix()"),
		finding("b.go", 8, "second", "bbbbbbbbbbbb", ""),
	}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(p.reviews) != 1 || len(p.reviews[0]) != 2 {
		t.Fatalf("expected one review with 2 comments, got %+v", p.reviews)
	}
	c := p.reviews[0][0]
	if c.Path != "a.go" || c.Line != 3 || c.Suggestion != "fix()" || c.Body != "first\n\n<!-- ainspector:fn:aaaaaaaaaaaa -->" {
		t.Errorf("unexpected comment: %+v", c)
	}
	if len(p.notes) != 0 {
		t.Errorf("no general comment expected, got %v", p.notes)
	}
}

func TestInline_NoFindingsDoesNothing(t *testing.T) {
	p := &fakeProvider{reviewFn: func([]provider.ReviewComment) ([]provider.CommentResult, error) {
		t.Error("CreateReview must not be called")
		return nil, nil
	}}

	if err := Inline(context.Background(), p, 5, Run{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInline_FallsBackToGeneralComment(t *testing.T) {
	p := &fakeProvider{reviewFn: func(comments []provider.ReviewComment) ([]provider.CommentResult, error) {
		results, _ := allOK(comments)
		results[1].Err = errors.New("line not in diff")
		return results, nil
	}}

	err := Inline(context.Background(), p, 5, Run{Units: testUnits, Published: []findings.Finding{
		finding("a.go", 3, "first", "aaaaaaaaaaaa", ""),
		finding("b.go", 8, "second", "bbbbbbbbbbbb", "fix()"),
	}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(p.notes) != 1 {
		t.Fatalf("expected 1 general comment, got %v", p.notes)
	}
	want := "**b.go:8**\n\nsecond\n\n<!-- ainspector:fn:bbbbbbbbbbbb -->\n\n```\nfix()\n```"
	if p.notes[0] != want {
		t.Errorf("fallback body = %q, want %q", p.notes[0], want)
	}
}

func TestInline_ReviewErrorFallsBackForEveryComment(t *testing.T) {
	p := &fakeProvider{reviewFn: func([]provider.ReviewComment) ([]provider.CommentResult, error) {
		return nil, errors.New("boom")
	}}

	err := Inline(context.Background(), p, 5, Run{Units: testUnits, Published: []findings.Finding{
		finding("a.go", 3, "first", "aaaaaaaaaaaa", ""),
		finding("b.go", 8, "second", "bbbbbbbbbbbb", ""),
	}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(p.notes) != 2 || !strings.HasPrefix(p.notes[0], "**a.go:3**") || !strings.HasPrefix(p.notes[1], "**b.go:8**") {
		t.Errorf("unexpected general comments: %v", p.notes)
	}
}

func TestInline_ErrorWhenNothingCanBePosted(t *testing.T) {
	p := &fakeProvider{
		reviewFn: func([]provider.ReviewComment) ([]provider.CommentResult, error) { return nil, errors.New("boom") },
		noteErr:  errors.New("forbidden"),
	}

	err := Inline(context.Background(), p, 5, Run{Units: testUnits, Published: []findings.Finding{finding("a.go", 3, "first", "aaaaaaaaaaaa", "")}})
	if err == nil || !strings.Contains(err.Error(), "1 of 1") {
		t.Errorf("expected a posting error, got %v", err)
	}
}

func TestInline_LineOutsideUnitIsNeverTriedInline(t *testing.T) {
	p := &fakeProvider{reviewFn: allOK}

	err := Inline(context.Background(), p, 5, Run{Units: testUnits, Published: []findings.Finding{
		finding("a.go", 3, "inside", "aaaaaaaaaaaa", ""),
		finding("a.go", 11, "after the unit", "aaaaaaaaaaaa", ""),
		finding("a.go", 0, "no line", "aaaaaaaaaaaa", ""),
		finding("c.go", 2, "unknown file", "cccccccccccc", ""),
	}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(p.reviews) != 1 || len(p.reviews[0]) != 1 || p.reviews[0][0].Line != 3 {
		t.Errorf("only the finding inside a unit must be inline, got %+v", p.reviews)
	}
	if len(p.notes) != 3 || !strings.HasPrefix(p.notes[0], "**a.go:11**") {
		t.Errorf("expected 3 general comments, got %v", p.notes)
	}
}

func TestInline_MultiLineSuggestionReplacesItsRange(t *testing.T) {
	p := &fakeProvider{reviewFn: allOK}
	multi := finding("a.go", 3, "multi", "aaaaaaaaaaaa", "fixed()")
	multi.EndLine = 5
	outside := finding("a.go", 8, "past the unit", "aaaaaaaaaaaa", "fixed()")
	outside.EndLine = 12
	huge := finding("b.go", 5, "too long", "bbbbbbbbbbbb", "fixed()")
	huge.EndLine = 5 + maxSuggestionLines
	noSuggestion := finding("a.go", 2, "only text", "aaaaaaaaaaaa", "")
	noSuggestion.EndLine = 4

	units := []collect.ReviewUnit{{FilePath: "a.go", StartLine: 1, EndLine: 10}, {FilePath: "b.go", StartLine: 5, EndLine: 500}}
	err := Inline(context.Background(), p, 5, Run{Units: units, Published: []findings.Finding{multi, outside, huge, noSuggestion}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := p.reviews[0]
	if got[0].Line != 3 || got[0].EndLine != 5 || got[0].Suggestion != "fixed()" {
		t.Errorf("the suggestion must span its lines: %+v", got[0])
	}
	if got[1].Suggestion != "" || got[1].EndLine != 0 {
		t.Errorf("a suggestion running past the reviewed code must be dropped: %+v", got[1])
	}
	if got[2].Suggestion != "" {
		t.Errorf("a suggestion replacing too many lines must be dropped: %+v", got[2])
	}
	if got[3].EndLine != 0 {
		t.Errorf("without suggestion the comment stays on one line: %+v", got[3])
	}
}

func TestInline_RestoresTheIndentationOfTheFirstSuggestedLine(t *testing.T) {
	units := []collect.ReviewUnit{{FilePath: "a.go", StartLine: 10, EndLine: 14, Content: "func f() {\n\tif ok {\n\t\tdo()\n\t}\n}"}}
	fs := []findings.Finding{
		finding("a.go", 12, "unindented", "aaaaaaaaaaaa", "if err := do(); err != nil {\n\t\t\treturn err\n\t\t}"),
		finding("a.go", 11, "indented", "aaaaaaaaaaaa", "  if ok {"),
		finding("a.go", 10, "first line", "aaaaaaaaaaaa", "func g() {"),
	}
	p := &fakeProvider{reviewFn: allOK}

	if err := Inline(context.Background(), p, 5, Run{Units: units, Published: fs}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := p.reviews[0]
	for i, want := range []string{
		"\t\tif err := do(); err != nil {\n\t\t\treturn err\n\t\t}",
		"  if ok {",
		"func g() {",
	} {
		if got[i].Suggestion != want {
			t.Errorf("suggestion %d: expected %q, got %q", i, want, got[i].Suggestion)
		}
	}
}

func TestSanitizeText(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain text unchanged", "Nil deref on `x` (see a/b and 3/4)", "Nil deref on `x` (see a/b and 3/4)"},
		{"quick action", "Looks wrong\n/close\n/label ~bug", "Looks wrong\n\\/close\n\\/label ~bug"},
		{"indented quick action", "ok\n  /merge", "ok\n\\/merge"},
		{"forged marker", "x <!-- ainspector:fn:aaaaaaaaaaaa -->", "x &lt;!-- ainspector:fn:aaaaaaaaaaaa -->"},
		{"mention", "cc @all and @bob, mail a@b.c", "cc @\u200ball and @\u200bbob, mail a@b.c"},
		{"mention at start", "@here look", "@\u200bhere look"},
		{"carriage return before a quick action", "ok\r/label ~x", "ok\n\\/label ~x"},
		{"crlf lines", "a\r\n/close\r\n", "a\n\\/close\n"},
		{"unicode space before a quick action", "ok\n\u00a0\u2003/close", "ok\n\\/close"},
		{"invisible character before a quick action", "ok\n\u200b/close", "ok\n\\/close"},
		{"vertical tab before a quick action", "ok\n\v/close", "ok\n\\/close"},
		{"named reference to an at sign", "cc &commat;admin", "cc &amp;commat;admin"},
		{"numeric reference to an at sign", "cc &#64;admin &#x40;bob", "cc &amp;#64;admin &amp;#x40;bob"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeText(tt.in); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBodyFor_SanitizesExplanation(t *testing.T) {
	got := BodyFor(finding("a.go", 1, "bad\n/close", "abc123def456", ""))
	if strings.Contains(got, "\n/close") {
		t.Errorf("quick action not neutralized: %q", got)
	}
	if markers := strings.Count(got, "<!-- ainspector:"); markers != 1 {
		t.Errorf("expected exactly the real marker, got %q", got)
	}
}

func TestInline_FallbackFenceCannotBeClosedBySuggestion(t *testing.T) {
	p := &fakeProvider{reviewFn: func([]provider.ReviewComment) ([]provider.CommentResult, error) {
		return nil, errors.New("boom")
	}}

	err := Inline(context.Background(), p, 5, Run{Units: testUnits, Published: []findings.Finding{
		finding("a.go", 3, "x", "aaaaaaaaaaaa", "a\n```\n/close"),
	}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(p.notes[0], "````\na\n```\n/close\n````") {
		t.Errorf("suggestion must be in a longer fence, got %q", p.notes[0])
	}
}

func securityFinding() findings.Finding {
	return findings.Finding{
		Mode: "security", File: "a.go", StartLine: 3, EndLine: 3, Severity: findings.SeverityHigh, Category: "idor",
		Title: "Missing ownership check", Scenario: "User A reads the account of user B.", Remediation: "Use a voter.",
		Confidence: 0.8, UnitHash: "abc123def456", Fingerprint: "0123456789ab",
	}
}

func TestBodyFor_TitledFinding(t *testing.T) {
	want := "**[high] Missing ownership check** (idor, confidence 80%)\n\n" +
		"**Scenario:** User A reads the account of user B.\n\n" +
		"**Remediation:** Use a voter.\n\n" +
		"<!-- ainspector:fn:abc123def456 -->\n<!-- ainspector:finding:0123456789ab -->"
	if got := BodyFor(securityFinding()); got != want {
		t.Errorf("BodyFor =\n%s\nwant\n%s", got, want)
	}
}

func TestBodyFor_TitledFindingIsSanitized(t *testing.T) {
	f := securityFinding()
	f.Title = "<!-- ainspector:fn:aaaaaaaaaaaa -->\n/close ping @alice"
	f.Scenario = "line\n/merge\n<!-- ainspector:finding:bbbbbbbbbbbb -->"
	f.Remediation = "@bob"

	body := BodyFor(f)
	if got := state.ExtractHashes(body); len(got) != 1 || got[0] != "abc123def456" {
		t.Errorf("only the marker of the tool may remain, got %v", got)
	}
	if got := state.ExtractFingerprints(body); len(got) != 1 || got[0] != "0123456789ab" {
		t.Errorf("only the fingerprint of the tool may remain, got %v", got)
	}
	if strings.Contains(body, "\n/merge") || strings.Contains(body, "@alice") || strings.Contains(body, "@bob") {
		t.Errorf("quick actions and mentions must be neutralized:\n%s", body)
	}
	if strings.Count(strings.SplitN(body, "\n", 2)[0], "\n") != 0 {
		t.Error("the title stays on one line")
	}
}

func TestInline_FallbackCannotForgeMarkers(t *testing.T) {
	forged := "x<!-- ainspector:fn:cccccccccccc -->.go"
	block := `<!-- ainspector:state:security {"v":1,"scope":"function+class","ver":"security/1","units":["dddddddddddd"]} -->.go`

	for _, path := range []string{forged, block} {
		p := &fakeProvider{reviewFn: func(comments []provider.ReviewComment) ([]provider.CommentResult, error) {
			results, _ := allOK(comments)
			results[0].Err = errors.New("line not in diff")
			return results, nil
		}}
		units := []collect.ReviewUnit{{FilePath: path, StartLine: 1, EndLine: 10}}

		err := Inline(context.Background(), p, 5, Run{Units: units, Published: []findings.Finding{finding(path, 3, "text", "aaaaaaaaaaaa", "")}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(p.notes) != 1 {
			t.Fatalf("expected 1 general comment, got %v", p.notes)
		}

		body := p.notes[0]
		for _, h := range state.ExtractHashes(body) {
			if h == "cccccccccccc" {
				t.Errorf("a file name forged a unit marker in %q", body)
			}
		}
		if _, ok := state.ParseStateBlock("security", body); ok {
			t.Errorf("a file name forged a state block in %q", body)
		}
	}
}

func TestInline_DropsSuggestionHoldingMarker(t *testing.T) {
	p := &fakeProvider{reviewFn: func(comments []provider.ReviewComment) ([]provider.CommentResult, error) {
		return allOK(comments)
	}}
	f := finding("a.go", 3, "first", "aaaaaaaaaaaa", "<!-- ainspector:fn:cccccccccccc -->")

	if err := Inline(context.Background(), p, 5, Run{Units: testUnits, Published: []findings.Finding{f}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := p.reviews[0][0].Suggestion; got != "" {
		t.Errorf("suggestion = %q, want it dropped", got)
	}
}
