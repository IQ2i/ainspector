package publish

import (
	"context"
	"fmt"
	"strings"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/findings"
	"github.com/iq2i/ainspector/internal/llm"
	"github.com/iq2i/ainspector/internal/state"
)

// Review status of a unit
const (
	StatusReviewed = "reviewed"
	// StatusSkipped is a unit left unreviewed because the token budget ran out
	StatusSkipped = "skipped"
	StatusFailed  = "failed"
)

// UnitStatus tells what became of a unit of Run.Units, at the same index
type UnitStatus struct {
	Hash   string
	Status string
}

// Feedback is what reviewers thought of a comment of an earlier run, from its thumbs
type Feedback struct {
	File        string `json:"file"`
	Line        int    `json:"line"`
	Unit        string `json:"unit,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Up          int    `json:"up"`
	Down        int    `json:"down"`
}

// Run is what a mode produced during one execution
type Run struct {
	Mode    string
	Scope   string
	Version string
	// Model is the model that served the reviews, empty when unknown
	Model    string
	Units    []collect.ReviewUnit
	Statuses []UnitStatus
	// AlreadyReviewed counts the units that were not reviewed again, a previous run did
	AlreadyReviewed int
	Published       []findings.Finding
	// Overflow holds the findings left out because the mode caps what it publishes
	Overflow []findings.Finding
	// Unjudged holds the findings cut before the false positive filter, which never saw them:
	// they stay out of what is kept open
	Unjudged []findings.Finding
	// Repeated holds the findings of an earlier run that this one found again: they are not
	// published twice but remain open
	Repeated []findings.Finding
	// Carried holds the findings of an earlier run whose units were not reviewed again, which
	// still hold
	Carried []findings.Reported
	// Verdicts are the decisions of the false positive filter
	Verdicts []findings.Verdict
	// Discarded counts the findings the false positive filter removed, by FPModel
	Discarded int
	FPModel   string
	// Reviewed holds the hashes of the units reviewed without error
	Reviewed []string
	// Skipped names the units left unreviewed because the token budget ran out, Failed those
	// whose review failed. Both are reviewed again by the next run.
	Skipped, Failed []string
	Usage           llm.Usage
	Cost            float64
	// CostIncomplete is set when some calls used a model without a known price
	CostIncomplete bool
	Notes          []string
	// Feedback holds the reactions to the comments of earlier runs
	Feedback []Feedback
}

type Publisher interface {
	Publish(ctx context.Context, run Run) error
}

// BodyFor renders a finding as a comment body, with the marker that prevents reviewing its unit
// again. A finding with a title carries severity, scenario and remediation, and a second marker
// that identifies it whatever line it moves to.
func BodyFor(f findings.Finding) string {
	if f.Title == "" {
		return sanitizeText(f.Explanation) + "\n\n" + state.FormatMarker(f.UnitHash)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "**[%s] %s**", f.Severity, oneLine(sanitizeText(f.Title)))
	if f.Category != "" {
		fmt.Fprintf(&sb, " (%s, confidence %.0f%%)", f.Category, f.Confidence*100)
	}
	sb.WriteString("\n")
	if f.Explanation != "" {
		sb.WriteString("\n" + sanitizeText(f.Explanation) + "\n")
	}
	if f.Scenario != "" {
		sb.WriteString("\n**Scenario:** " + sanitizeText(f.Scenario) + "\n")
	}
	if f.Remediation != "" {
		sb.WriteString("\n**Remediation:** " + sanitizeText(f.Remediation) + "\n")
	}
	sb.WriteString("\n" + state.FormatMarker(f.UnitHash))
	if f.Fingerprint != "" {
		sb.WriteString("\n" + state.FormatFindingMarker(f.Fingerprint))
	}
	return sb.String()
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// openFindings summarizes the findings of the run that are still true, each with the unit that
// produced it: published, left out by the cap, and repeated. The ones the false positive filter
// never judged are not among them.
func (r Run) openFindings() []findings.Reported {
	starts := make(map[string]collect.ReviewUnit, len(r.Statuses))
	for i, s := range r.Statuses {
		if i < len(r.Units) {
			starts[s.Hash] = r.Units[i]
		}
	}

	var open []findings.Reported
	for _, group := range [][]findings.Finding{r.Published, r.Overflow, r.Repeated} {
		for _, f := range group {
			u := starts[f.UnitHash]
			open = append(open, f.Open(u.StartLine, u.Name))
		}
	}
	return open
}
