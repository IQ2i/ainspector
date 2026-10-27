package publish

import (
	"context"
	"fmt"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/findings"
	"github.com/iq2i/ainspector/internal/provider"
)

type inline struct {
	provider provider.Provider
	number   int
}

// NewInline posts findings as inline comments, falling back to a general comment
// for those the provider rejects (typically a line outside the diff).
func NewInline(p provider.Provider, number int) Publisher {
	return &inline{provider: p, number: number}
}

func (i *inline) Publish(ctx context.Context, run Run) error {
	if len(run.Published) == 0 {
		return nil
	}

	var inlineComments, general []provider.ReviewComment
	for _, f := range run.Published {
		c := provider.ReviewComment{
			Path:       f.File,
			Line:       f.StartLine,
			Body:       BodyFor(f),
			Suggestion: f.Suggestion,
		}
		if insideUnit(run.Units, f) {
			inlineComments = append(inlineComments, c)
		} else {
			fmt.Printf("Warning: %s:%d is outside the reviewed code, posting a general comment instead\n", c.Path, c.Line)
			general = append(general, c)
		}
	}

	if len(inlineComments) > 0 {
		results, err := i.provider.CreateReview(ctx, i.number, inlineComments)
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
		if _, err := i.provider.PostComment(ctx, i.number, fallbackBody(c)); err != nil {
			fmt.Printf("Warning: could not post general comment for %s:%d: %v\n", c.Path, c.Line, err)
			failed++
		}
	}

	if failed > 0 {
		return fmt.Errorf("failed to post %d of %d comments", failed, len(run.Published))
	}
	return nil
}

// insideUnit tells whether a finding points at lines of a reviewed unit: anything else
// (class header, related file, invented line) cannot be anchored to the diff.
func insideUnit(units []collect.ReviewUnit, f findings.Finding) bool {
	for _, u := range units {
		if u.FilePath == f.File && f.StartLine >= u.StartLine && f.StartLine <= u.EndLine {
			return true
		}
	}
	return false
}

func fallbackBody(c provider.ReviewComment) string {
	body := fmt.Sprintf("**%s:%d**\n\n%s", c.Path, c.Line, c.Body)
	if c.Suggestion != "" {
		body += "\n\n" + provider.Fence("", c.Suggestion)
	}
	return body
}
