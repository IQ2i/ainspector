package pipeline

import (
	"context"
	"fmt"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/findings"
	"github.com/iq2i/ainspector/internal/llm"
	"github.com/iq2i/ainspector/internal/modes"
	"github.com/iq2i/ainspector/internal/state"
)

// UnitResult is the outcome of reviewing one unit
type UnitResult struct {
	Unit     collect.ReviewUnit
	Findings []findings.Finding
	Err      error
}

// ReviewUnits reviews each unit sequentially. A failing unit does not stop the others.
func ReviewUnits(ctx context.Context, client llm.Client, m modes.Mode, units []collect.ReviewUnit, pc *collect.ProjectContext) []UnitResult {
	results := make([]UnitResult, 0, len(units))
	rulesDigest := state.RulesDigest(m.Rules())

	for _, u := range units {
		result := UnitResult{Unit: u}

		resp, err := client.Complete(ctx, m.BuildRequest(u, pc))
		if err == nil {
			result.Findings, err = m.Parse(resp, u)
		}
		if err != nil {
			fmt.Printf("Warning: review of %s in %s failed: %v\n", u.Name, u.FilePath, err)
			result.Err = err
			results = append(results, result)
			continue
		}

		hash := state.UnitHash(m, u, rulesDigest)
		for i := range result.Findings {
			result.Findings[i].UnitHash = hash
		}
		results = append(results, result)
	}

	return results
}
