package pipeline

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/sync/errgroup"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/findings"
	"github.com/iq2i/ainspector/internal/llm"
	"github.com/iq2i/ainspector/internal/modes"
	"github.com/iq2i/ainspector/internal/state"
)

// maxWorkers bounds the concurrent LLM calls of a mode
const maxWorkers = 3

// UnitResult is the outcome of reviewing one unit
type UnitResult struct {
	Unit     collect.ReviewUnit
	Hash     string
	Model    string
	Findings []findings.Finding
	Err      error
	// Skipped is set when the unit was not sent because the token budget was spent
	Skipped bool
}

// ReviewUnits reviews the units with a few concurrent workers. A failing unit does not stop the
// others, and the results keep the order of the units whatever the order of completion.
func ReviewUnits(ctx context.Context, client llm.Client, m modes.Mode, units []collect.ReviewUnit, pc *collect.ProjectContext) []UnitResult {
	results := make([]UnitResult, len(units))
	if len(units) == 0 {
		return results
	}

	rulesDigest := state.RulesDigest(m.Rules())
	review := func(i int) {
		results[i] = reviewUnit(ctx, client, m, units[i], pc, rulesDigest)
	}

	// The first unit goes alone: it writes the prompt cache that the others then read,
	// instead of all of them paying for the same write at once
	review(0)

	var g errgroup.Group
	g.SetLimit(maxWorkers)
	for i := 1; i < len(units); i++ {
		g.Go(func() error {
			review(i)
			return nil
		})
	}
	_ = g.Wait()

	return results
}

func reviewUnit(ctx context.Context, client llm.Client, m modes.Mode, u collect.ReviewUnit, pc *collect.ProjectContext, rulesDigest string) UnitResult {
	result := UnitResult{Unit: u, Hash: state.UnitHash(m, u, rulesDigest)}

	resp, err := client.Complete(ctx, m.BuildRequest(u, pc))
	result.Model = resp.Model
	if err == nil {
		result.Findings, err = m.Parse(resp, u)
	}
	if err != nil {
		result.Err = err
		result.Skipped = errors.Is(err, llm.ErrBudgetExceeded)
		if !result.Skipped {
			fmt.Printf("Warning: review of %s in %s failed: %v\n", u.Name, u.FilePath, err)
		}
		return result
	}

	for i := range result.Findings {
		result.Findings[i].UnitHash = result.Hash
	}
	return result
}
