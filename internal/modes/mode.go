package modes

import (
	"fmt"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/config"
	"github.com/iq2i/ainspector/internal/findings"
	"github.com/iq2i/ainspector/internal/llm"
	"github.com/iq2i/ainspector/internal/modes/quality"
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
		default:
			return nil, fmt.Errorf("unknown mode %q", name)
		}
	}
	return result, nil
}
