package publish

import (
	"context"
	"fmt"
	"strings"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/findings"
	"github.com/iq2i/ainspector/internal/provider"
	"github.com/iq2i/ainspector/internal/state"
	"github.com/iq2i/ainspector/internal/text"
)

// SummaryMarker identifies the summary comment of a mode
func SummaryMarker(mode string) string {
	return state.SummaryMarker(mode)
}

// Summary maintains one comment per mode: created by the first run, updated by the next
// ones. It also stores the state block that lets later runs skip the units already reviewed,
// which is why only a comment written by self is trusted.
func Summary(ctx context.Context, p provider.Provider, number int, self string, run Run) error {
	return upsertNote(ctx, p, number, self, SummaryMarker(run.Mode), "summary", func(existing provider.ExistingComment) string {
		var previous *state.StateBlock
		if b, ok := state.ParseStateBlock(run.Mode, existing.Body); ok {
			previous = &b
		}
		return SummaryBody(run, previous)
	})
}

// SummaryBody renders the summary comment of a run. previous is the state block of the comment
// being replaced: its hashes are kept unless the mode version or scope changed since, as they
// could not match a unit hash anymore.
func SummaryBody(run Run, previous *state.StateBlock) string {
	var sb strings.Builder

	sb.WriteString(SummaryMarker(run.Mode) + "\n")
	fmt.Fprintf(&sb, "### ainspector: %s review\n\n", run.Mode)

	// A run that reports nothing new must not read as a clean change while earlier findings hold
	var stillOpen []findings.Reported
	for _, f := range run.Repeated {
		stillOpen = append(stillOpen, f.Open(0, ""))
	}
	stillOpen = append(stillOpen, run.Carried...)

	issues := "no issue found"
	if n := len(run.Published); n > 0 {
		issues = fmt.Sprintf("%d %s reported", n, text.Plural(n, "issue"))
	} else if len(stillOpen) > 0 {
		issues = "no new issue found"
	}
	unit := "function"
	for _, u := range run.Units {
		if u.Kind != collect.KindFunction {
			unit = "unit"
			break
		}
	}
	unchanged := ""
	if n := run.AlreadyReviewed; n > 0 {
		unchanged = fmt.Sprintf(" (%d unchanged since an earlier review)", n)
	}
	fmt.Fprintf(&sb, "Reviewed %d %s%s, %s.\n", len(run.Reviewed), text.Plural(len(run.Reviewed), unit), unchanged, issues)

	if run.Model != "" {
		fmt.Fprintf(&sb, "\nModel: `%s`\n", run.Model)
	}
	if run.Discarded > 0 {
		fmt.Fprintf(&sb, "\n%d %s discarded as false %s", run.Discarded, text.Plural(run.Discarded, "finding"), text.Plural(run.Discarded, "positive"))
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
		overflow = append(overflow, fmt.Sprintf("%s:%d [%s] %s", f.File, f.StartLine, f.Severity, text.OneLine(f.Title)))
	}
	writeList(fmt.Sprintf("Not commented, only the %d most severe findings are published:", len(run.Published)), overflow)
	var open []string
	for _, f := range stillOpen {
		item := fmt.Sprintf("%s:%d", f.File, f.Line)
		if f.Severity != "" {
			item += fmt.Sprintf(" [%s]", f.Severity)
		}
		open = append(open, item+" "+text.CutAtRune(text.OneLine(f.Title), maxOpenItem))
	}
	writeList("Still open from earlier runs:", open)
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

// maxOpenItem bounds the title of a finding still open, as the quality ones are whole explanations
const maxOpenItem = 120

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
