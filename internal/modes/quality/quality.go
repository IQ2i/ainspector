package quality

import (
	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/findings"
	"github.com/iq2i/ainspector/internal/llm"
)

// Mode reviews modified functions for bugs, readability and project rules
type Mode struct {
	model string
	rules []string
}

// New creates the quality mode. An empty model leaves the choice to the client.
func New(model string, rules []string) *Mode {
	return &Mode{model: model, rules: rules}
}

func (m *Mode) Name() string { return "quality" }

func (m *Mode) Scope() collect.Scope { return collect.ScopeFunction }

func (m *Mode) Model() string { return m.model }

func (m *Mode) Version() string { return "quality/1" }

func (m *Mode) BuildRequest(u collect.ReviewUnit, pc *collect.ProjectContext) llm.Request {
	return llm.Request{
		Model:    m.model,
		System:   []llm.Block{{Text: buildSystemPrompt(u.Language, pc, m.rules)}},
		Messages: []llm.Message{{Role: "user", Content: buildUserPrompt(&u)}},
	}
}

func (m *Mode) Parse(resp llm.Response, u collect.ReviewUnit) ([]findings.Finding, error) {
	var result []findings.Finding
	for _, s := range parseReviewResponse(resp.Text) {
		result = append(result, findings.Finding{
			Mode:        m.Name(),
			File:        u.FilePath,
			StartLine:   s.Line,
			EndLine:     s.Line,
			Explanation: s.Description,
			Suggestion:  s.Code,
		})
	}
	return result, nil
}

func (m *Mode) FilterPolicy() findings.Policy { return findings.Policy{} }
