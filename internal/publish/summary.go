package publish

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/findings"
	"github.com/iq2i/ainspector/internal/provider"
	"github.com/iq2i/ainspector/internal/state"
)

type summary struct {
	provider provider.Provider
	number   int
	self     string
}

// SummaryMarker identifies the summary comment of a mode
func SummaryMarker(mode string) string {
	return state.SummaryMarker(mode)
}

// NewSummary maintains one comment per mode: created by the first run, updated by the next
// ones. It also stores the state block that lets later runs skip the units already reviewed,
// which is why only a comment written by self is trusted.
func NewSummary(p provider.Provider, number int, self string) Publisher {
	return &summary{provider: p, number: number, self: self}
}

func (s *summary) Publish(ctx context.Context, run Run) error {
	// Without an identity any participant could plant a comment carrying the marker and have
	// the tool edit it or take its forged state for granted
	if s.self == "" {
		return errors.New("cannot publish the summary: the token owner is unknown")
	}

	existing, err := s.provider.ListComments(ctx, s.number)
	if err != nil {
		return fmt.Errorf("failed to list comments: %w", err)
	}

	var id string
	var previous *state.StateBlock
	if c, ok := ownNote(existing, s.self, SummaryMarker(run.Mode)); ok {
		id = c.ID
		if b, ok := state.ParseStateBlock(run.Mode, c.Body); ok {
			previous = &b
		}
	}

	body := SummaryBody(run, previous)
	if id == "" {
		if _, err := s.provider.PostComment(ctx, s.number, body); err != nil {
			return fmt.Errorf("failed to post the summary: %w", err)
		}
		return nil
	}
	if err := s.provider.UpdateComment(ctx, s.number, id, body); err != nil {
		return fmt.Errorf("failed to update the summary: %w", err)
	}
	return nil
}

// SummaryBody renders the summary comment of a run. previous is the state block of the comment
// being replaced: its hashes are kept unless the mode version or scope changed since, as they
// could not match a unit hash anymore.
func SummaryBody(run Run, previous *state.StateBlock) string {
	var sb strings.Builder

	sb.WriteString(SummaryMarker(run.Mode) + "\n")
	fmt.Fprintf(&sb, "### ainspector: %s review\n\n", run.Mode)

	issues := "no issue found"
	if n := len(run.Published); n > 0 {
		issues = fmt.Sprintf("%d %s reported", n, plural(n, "issue"))
	}
	unit := "function"
	for _, u := range run.Units {
		if u.Kind != collect.KindFunction {
			unit = "unit"
			break
		}
	}
	fmt.Fprintf(&sb, "Reviewed %d %s, %s.\n", len(run.Reviewed), plural(len(run.Reviewed), unit), issues)

	if run.Model != "" {
		fmt.Fprintf(&sb, "\nModel: `%s`\n", run.Model)
	}
	if run.Discarded > 0 {
		fmt.Fprintf(&sb, "\n%d %s discarded as false %s", run.Discarded, plural(run.Discarded, "finding"), plural(run.Discarded, "positive"))
		if run.FPModel != "" {
			fmt.Fprintf(&sb, " by `%s`", run.FPModel)
		}
		sb.WriteString(".\n")
	}
	if line := feedbackLine(run.Feedback); line != "" {
		fmt.Fprintf(&sb, "\n%s\n", line)
	}
	u := run.Usage
	if u.Input+u.Output+u.CacheRead+u.CacheWrite > 0 {
		fmt.Fprintf(&sb, "\nTokens: %d in, %d out, %d read from cache, %d written to cache\n", u.Input, u.Output, u.CacheRead, u.CacheWrite)
		fmt.Fprintf(&sb, "Estimated cost: %s\n", FormatCost(run.Cost, run.CostIncomplete))
	}

	writeList := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&sb, "\n%s\n", title)
		for _, item := range items {
			fmt.Fprintf(&sb, "- %s\n", sanitizeText(item))
		}
	}
	var overflow []string
	for _, f := range append(append([]findings.Finding{}, run.Overflow...), run.Unjudged...) {
		overflow = append(overflow, fmt.Sprintf("%s:%d [%s] %s", f.File, f.StartLine, f.Severity, oneLine(f.Title)))
	}
	writeList(fmt.Sprintf("Not commented, only the %d most severe findings are published:", len(run.Published)), overflow)
	writeList("Not reviewed, the token budget was reached:", run.Skipped)
	writeList("Not reviewed, the review failed:", run.Failed)
	writeList("Notes:", run.Notes)

	var units, fingerprints []string
	var reported []findings.Reported
	if previous != nil && previous.Ver == run.Version && previous.Scope == run.Scope {
		units, fingerprints, reported = previous.Units, previous.Findings, previous.Reported
	}
	var found []string
	var foundReports []findings.Reported
	for _, f := range run.Published {
		if f.Fingerprint != "" {
			found = append(found, f.Fingerprint)
			foundReports = append(foundReports, f.Report())
		}
	}
	block := state.StateBlock{
		Scope:    run.Scope,
		Ver:      run.Version,
		Units:    state.MergeHashes(units, run.Reviewed),
		Findings: state.MergeHashes(fingerprints, found),
		Reported: state.MergeReported(reported, foundReports),
		Open:     state.MergeOpen(run.Carried, run.openFindings()),
	}
	sb.WriteString("\n" + state.FormatStateBlock(run.Mode, block) + "\n")

	return sb.String()
}

// feedbackLine counts the earlier comments that reviewers found useful (more thumbs up than
// down), not useful (more down than up), or left without a verdict
func feedbackLine(feedback []Feedback) string {
	if len(feedback) == 0 {
		return ""
	}
	var useful, useless int
	for _, f := range feedback {
		switch {
		case f.Up > f.Down:
			useful++
		case f.Down > f.Up:
			useless++
		}
	}
	return fmt.Sprintf("Reactions to earlier comments: %d useful, %d not useful, %d without a verdict.", useful, useless, len(feedback)-useful-useless)
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// FormatCost renders an estimated cost, saying when it is incomplete
func FormatCost(cost float64, incomplete bool) string {
	switch {
	case incomplete && cost == 0:
		return "unknown, the model has no known price"
	case incomplete:
		return fmt.Sprintf("at least $%.4f, some calls used a model with no known price", cost)
	}
	return fmt.Sprintf("$%.4f", cost)
}
