package publish

import (
	"context"
	"fmt"
	"strings"

	"github.com/iq2i/ainspector/internal/provider"
	"github.com/iq2i/ainspector/internal/text"
)

// NoticeMarker identifies the comment that tells a review stopped before reviewing anything
const NoticeMarker = "<!-- ainspector:notice -->"

// ownNote returns the general comment that self wrote and that carries marker. Only the
// comments of self are trusted: anyone could plant the marker in their own. The marker must open
// the comment, as LoadIndex requires for the state block.
func ownNote(existing []provider.ExistingComment, self, marker string) (provider.ExistingComment, bool) {
	for _, c := range existing {
		if !c.Positioned && strings.EqualFold(c.Author, self) && strings.HasPrefix(strings.TrimSpace(c.Body), marker) {
			return c, true
		}
	}
	return provider.ExistingComment{}, false
}

// PostNotice tells the participants that a review stopped before reviewing anything, when the
// change may be the cause: it modifies the configuration file (configFile, empty otherwise) or
// puts files out of review (notes). A change that blinds its own review would otherwise leave
// no trace on the PR/MR. The comment is created once, then updated.
func PostNotice(ctx context.Context, p provider.Provider, number int, self, configFile, reason string, notes []string) error {
	body := NoticeBody(configFile, reason, notes)
	return upsertNote(ctx, p, number, self, NoticeMarker, "notice", func(provider.ExistingComment) string { return body })
}

// upsertNote creates the general comment that opens with marker, or updates the one self already
// wrote. render gets the existing comment (zero when there is none) and returns the new body.
// Without an identity any participant could plant a comment carrying the marker and have the
// tool edit it or take its forged state for granted.
func upsertNote(ctx context.Context, p provider.Provider, number int, self, marker, what string, render func(existing provider.ExistingComment) string) error {
	if self == "" {
		return fmt.Errorf("cannot publish the %s: the token owner is unknown", what)
	}

	comments, err := p.ListComments(ctx, number)
	if err != nil {
		return fmt.Errorf("failed to list comments: %w", err)
	}

	existing, found := ownNote(comments, self, marker)
	body := render(existing)
	if !found {
		if _, err := p.PostComment(ctx, number, body); err != nil {
			return fmt.Errorf("failed to post the %s: %w", what, err)
		}
		return nil
	}
	if err := p.UpdateComment(ctx, number, existing.ID, body); err != nil {
		return fmt.Errorf("failed to update the %s: %w", what, err)
	}
	return nil
}

// NoticeBody renders the notice comment
func NoticeBody(configFile, reason string, notes []string) string {
	var sb strings.Builder
	sb.WriteString(NoticeMarker + "\n### ainspector\n\n")
	fmt.Fprintf(&sb, "The review stopped before reviewing anything: %s.\n", text.OneLine(sanitizeText(reason)))
	if configFile != "" {
		fmt.Fprintf(&sb, "\nThis change modifies `%s`, its settings may be the cause.\n", text.OneLine(sanitizeText(configFile)))
	}
	if len(notes) > 0 {
		sb.WriteString("\nNotes:\n")
		for _, n := range notes {
			fmt.Fprintf(&sb, "- %s\n", text.OneLine(sanitizeText(n)))
		}
	}
	return sb.String()
}
