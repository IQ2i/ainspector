package publish

import (
	"context"

	"github.com/iq2i/ainspector/internal/cache"
	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/findings"
)

// Policy tells which outputs a mode produces
type Policy struct {
	Inline bool
}

// Run is what a mode produced during one execution
type Run struct {
	Mode      string
	Units     []collect.ReviewUnit
	Published []findings.Finding
}

type Publisher interface {
	Publish(ctx context.Context, run Run) error
}

// BodyFor renders a finding as a comment body, with the marker that prevents reviewing its unit again
func BodyFor(f findings.Finding) string {
	return sanitizeText(f.Explanation) + "\n\n" + cache.FormatHashMarker(f.UnitHash)
}
