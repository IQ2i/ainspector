package publish

import (
	"context"

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

// BodyFor renders a finding as a comment body, with the marker that prevents reviewing its unit again
func BodyFor(f findings.Finding) string {
	return sanitizeText(f.Explanation) + "\n\n" + state.FormatMarker(f.UnitHash)
}
