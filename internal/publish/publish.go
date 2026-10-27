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

// Policy tells which outputs a mode produces
type Policy struct {
	Inline  bool
	Summary bool
}

// Run is what a mode produced during one execution
type Run struct {
	Mode    string
	Scope   string
	Version string
	// Model is the model that served the reviews, empty when unknown
	Model     string
	Units     []collect.ReviewUnit
	Published []findings.Finding
	// Overflow holds the findings left out because the mode caps what it publishes
	Overflow []findings.Finding
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
