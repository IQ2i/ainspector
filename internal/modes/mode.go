package modes

import (
	"fmt"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/config"
	"github.com/iq2i/ainspector/internal/findings"
	"github.com/iq2i/ainspector/internal/llm"
	"github.com/iq2i/ainspector/internal/modes/quality"
	"github.com/iq2i/ainspector/internal/modes/security"
	"github.com/iq2i/ainspector/internal/publish"
)

// Mode is a kind of review with its own prompt, output parsing and noise policy
type Mode interface {
	Name() string
	Scope() collect.Scope
	Model() string
	// Version enters the unit hash: any prompt change must bump it
	Version() string
	// Rules are the project rules given to the mode, they enter the unit hash
	Rules() []string
	// Accepts tells whether a collected unit is reviewed by this mode
	Accepts(u collect.ReviewUnit) bool
	// Needs holds what the mode asks of the collection beyond its scope (MatchDeleted, Hunks)
	Needs() collect.Request
	BuildRequest(u collect.ReviewUnit, pc *collect.ProjectContext) llm.Request
	Parse(resp llm.Response, u collect.ReviewUnit) ([]findings.Finding, error)
	FilterPolicy() findings.Policy
	PublishPolicy() publish.Policy
}

// Enabled builds the modes named in names, in order
func Enabled(names []string, cfg *config.Config) ([]Mode, error) {
	var result []Mode
	seen := make(map[string]bool)
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true

		switch name {
		case "quality":
			result = append(result, quality.New(cfg.Modes.Quality.Model, cfg.QualityRules()))
		case "security":
			mode, err := newSecurity(cfg)
			if err != nil {
				return nil, err
			}
			result = append(result, mode)
		default:
			return nil, fmt.Errorf("unknown mode %q", name)
		}
	}
	return result, nil
}

func newSecurity(cfg *config.Config) (*security.Mode, error) {
	sec := cfg.Modes.Security

	scope, err := collect.ParseScope(sec.ScopeName())
	if err != nil {
		return nil, err
	}

	hunks := sec.Hunks.Resolved()
	related := sec.Related.Resolved()
	return security.New(security.Config{
		Model:       sec.Model,
		Scope:       scope,
		MaxFindings: sec.Findings(),
		Hunks:       collect.HunkPolicy{Extensions: hunks.Extensions, Before: hunks.Before, After: hunks.After},
		Related:     collect.RelatedPolicy{Presets: related.Presets, MaxFiles: related.MaxFiles, MaxBytes: related.MaxBytes},
		FalsePositive: findings.FPConfig{
			Model:        sec.FalsePositive.Model,
			Instructions: sec.FalsePositive.Instructions,
		},
	}), nil
}
