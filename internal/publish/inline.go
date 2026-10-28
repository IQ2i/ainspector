package publish

import (
	"context"
	"fmt"
	"strings"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/findings"
	"github.com/iq2i/ainspector/internal/provider"
)

// Inline posts findings as inline comments, falling back to a general comment
// for those the provider rejects (typically a line outside the diff).
func Inline(ctx context.Context, p provider.Provider, number int, run Run) error {
	if len(run.Published) == 0 {
		return nil
	}

	var inlineComments, general []provider.ReviewComment
	for _, f := range run.Published {
		c := provider.ReviewComment{
			Path:       f.File,
			Line:       f.StartLine,
			Body:       BodyFor(f),
			Suggestion: safeSuggestion(f),
		}
		if unit := unitOf(run.Units, f); unit != nil {
			if c.Suggestion != "" && f.EndLine > f.StartLine {
				if f.EndLine <= unit.EndLine && f.EndLine-f.StartLine < maxSuggestionLines {
					c.EndLine = f.EndLine
				} else {
					// Applied to the wrong lines, a suggestion would damage the code
					fmt.Printf("Warning: dropping the suggestion for %s:%d-%d, its lines are not all inside the reviewed code\n", f.File, f.StartLine, f.EndLine)
					c.Suggestion = ""
				}
			}
			inlineComments = append(inlineComments, c)
		} else {
			fmt.Printf("Warning: %s:%d is outside the reviewed code, posting a general comment instead\n", c.Path, c.Line)
			general = append(general, c)
		}
	}

	if len(inlineComments) > 0 {
		results, err := p.CreateReview(ctx, number, inlineComments)
		if err != nil {
			results = make([]provider.CommentResult, len(inlineComments))
			for n, c := range inlineComments {
				results[n] = provider.CommentResult{Comment: c, Err: err}
			}
		}
		for _, r := range results {
			if r.Err != nil {
				fmt.Printf("Warning: could not post inline comment on %s:%d (%v), posting a general comment instead\n", r.Comment.Path, r.Comment.Line, r.Err)
				general = append(general, r.Comment)
			}
		}
	}

	failed := 0
	for _, c := range general {
		if _, err := p.PostComment(ctx, number, fallbackBody(c)); err != nil {
			fmt.Printf("Warning: could not post general comment for %s:%d: %v\n", c.Path, c.Line, err)
			failed++
		}
	}

	if failed > 0 {
		return fmt.Errorf("failed to post %d of %d comments", failed, len(run.Published))
	}
	return nil
}

// maxSuggestionLines caps the lines a single suggestion may replace
const maxSuggestionLines = 30

// unitOf returns the reviewed unit a finding points at, nil for anything that cannot be anchored
// to the diff: class header, related file, invented line.
func unitOf(units []collect.ReviewUnit, f findings.Finding) *collect.ReviewUnit {
	for i, u := range units {
		if u.FilePath == f.File && f.StartLine >= u.StartLine && f.StartLine <= u.EndLine {
			return &units[i]
		}
	}
	return nil
}

// safeSuggestion drops a suggestion that holds an ainspector marker: it is code written by the
// model, posted as is, and a marker in the comment of the token owner would be taken for its own
func safeSuggestion(f findings.Finding) string {
	if strings.Contains(f.Suggestion, "ainspector:") {
		fmt.Printf("Warning: dropping the suggestion for %s:%d, it holds an ainspector marker\n", f.File, f.StartLine)
		return ""
	}
	return f.Suggestion
}

func fallbackBody(c provider.ReviewComment) string {
	// The path is the name of a file of the change: it must not forge a marker either
	body := fmt.Sprintf("**%s:%d**\n\n%s", sanitizeText(c.Path), c.Line, c.Body)
	if c.Suggestion != "" {
		body += "\n\n" + provider.Fence("", c.Suggestion)
	}
	return body
}
