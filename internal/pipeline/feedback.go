package pipeline

import (
	"context"
	"fmt"

	"github.com/iq2i/ainspector/internal/provider"
	"github.com/iq2i/ainspector/internal/publish"
	"github.com/iq2i/ainspector/internal/state"
)

// collectFeedback reads the thumbs given to the inline comments that ainspector posted in earlier
// runs, by mode. They measure how useful the findings are: a failure to read them is only logged.
func collectFeedback(ctx context.Context, p provider.Provider, number int, existing []provider.ExistingComment, self string) map[string][]publish.Feedback {
	comments := state.OwnInlineComments(existing, self)
	if len(comments) == 0 {
		return nil
	}

	ids := make([]string, len(comments))
	for i, c := range comments {
		ids[i] = c.ID
	}
	counts, err := p.ListReactions(ctx, number, ids)
	if err != nil {
		fmt.Printf("Warning: could not read all the reactions to earlier comments: %v\n", err)
	}

	feedback := make(map[string][]publish.Feedback)
	for _, c := range comments {
		count, ok := counts[c.ID]
		if !ok {
			continue
		}
		feedback[c.Mode] = append(feedback[c.Mode], publish.Feedback{
			File: c.Path, Line: c.Line, Unit: c.Unit, Fingerprint: c.Fingerprint, Up: count.Up, Down: count.Down,
		})
	}
	return feedback
}
