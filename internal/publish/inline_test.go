package publish

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/findings"
	"github.com/iq2i/ainspector/internal/provider"
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

	err := NewInline(p, 5).Publish(context.Background(), Run{Units: testUnits, Published: []findings.Finding{
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

	if err := NewInline(p, 5).Publish(context.Background(), Run{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInline_FallsBackToGeneralComment(t *testing.T) {
	p := &fakeProvider{reviewFn: func(comments []provider.ReviewComment) ([]provider.CommentResult, error) {
		results, _ := allOK(comments)
		results[1].Err = errors.New("line not in diff")
		return results, nil
	}}

	err := NewInline(p, 5).Publish(context.Background(), Run{Units: testUnits, Published: []findings.Finding{
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

	err := NewInline(p, 5).Publish(context.Background(), Run{Units: testUnits, Published: []findings.Finding{
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

	err := NewInline(p, 5).Publish(context.Background(), Run{Units: testUnits, Published: []findings.Finding{finding("a.go", 3, "first", "aaaaaaaaaaaa", "")}})
	if err == nil || !strings.Contains(err.Error(), "1 of 1") {
		t.Errorf("expected a posting error, got %v", err)
	}
}

func TestInline_LineOutsideUnitIsNeverTriedInline(t *testing.T) {
	p := &fakeProvider{reviewFn: allOK}

	err := NewInline(p, 5).Publish(context.Background(), Run{Units: testUnits, Published: []findings.Finding{
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

	err := NewInline(p, 5).Publish(context.Background(), Run{Units: testUnits, Published: []findings.Finding{
		finding("a.go", 3, "x", "aaaaaaaaaaaa", "a\n```\n/close"),
	}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(p.notes[0], "````\na\n```\n/close\n````") {
		t.Errorf("suggestion must be in a longer fence, got %q", p.notes[0])
	}
}
