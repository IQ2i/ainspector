package publish

import (
	"context"
	"fmt"
	"strings"

	"github.com/iq2i/ainspector/internal/provider"
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
	if self == "" {
		return fmt.Errorf("cannot post the notice: the token owner is unknown")
	}

	existing, err := p.ListComments(ctx, number)
	if err != nil {
		return fmt.Errorf("failed to list comments: %w", err)
	}

	body := NoticeBody(configFile, reason, notes)
	if c, ok := ownNote(existing, self, NoticeMarker); ok {
		if err := p.UpdateComment(ctx, number, c.ID, body); err != nil {
			return fmt.Errorf("failed to update the notice: %w", err)
		}
		return nil
	}
	if _, err := p.PostComment(ctx, number, body); err != nil {
		return fmt.Errorf("failed to post the notice: %w", err)
	}
	return nil
}

// NoticeBody renders the notice comment
func NoticeBody(configFile, reason string, notes []string) string {
	var sb strings.Builder
	sb.WriteString(NoticeMarker + "\n### ainspector\n\n")
	fmt.Fprintf(&sb, "The review stopped before reviewing anything: %s.\n", oneLine(sanitizeText(reason)))
	if configFile != "" {
		fmt.Fprintf(&sb, "\nThis change modifies `%s`, its settings may be the cause.\n", oneLine(sanitizeText(configFile)))
	}
	if len(notes) > 0 {
		sb.WriteString("\nNotes:\n")
		for _, n := range notes {
			fmt.Fprintf(&sb, "- %s\n", oneLine(sanitizeText(n)))
		}
	}
	return sb.String()
}
