package quality

import (
	"encoding/json"
	"fmt"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/findings"
	"github.com/iq2i/ainspector/internal/llm"
	"github.com/iq2i/ainspector/internal/publish"
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

func (m *Mode) Version() string { return "quality/3" }

func (m *Mode) Rules() []string { return m.rules }

// Accepts takes the functions that have added or modified lines: it never reviewed anything else
func (m *Mode) Accepts(u collect.ReviewUnit) bool {
	return u.Kind == collect.KindFunction && !u.DeletedOnly
}

func (m *Mode) Needs() collect.Request { return collect.Request{} }

func (m *Mode) BuildRequest(u collect.ReviewUnit, pc *collect.ProjectContext) llm.Request {
	return llm.Request{
		Model:    m.model,
		System:   buildSystemBlocks(u.Language, pc, m.rules),
		Messages: []llm.Message{{Role: "user", Content: buildUserPrompt(&u)}},
		Schema:   &llm.Schema{Name: "code_review", JSON: reviewSchema},
	}
}

func (m *Mode) Parse(resp llm.Response, u collect.ReviewUnit) ([]findings.Finding, error) {
	if resp.JSON == nil {
		return nil, fmt.Errorf("the reply holds no structured output")
	}
	var doc ReviewResponse
	if err := json.Unmarshal(resp.JSON, &doc); err != nil {
		return nil, fmt.Errorf("invalid review document: %w", err)
	}

	var result []findings.Finding
	for _, s := range doc.Issues {
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

func (m *Mode) PublishPolicy() publish.Policy {
	return publish.Policy{Inline: true, Summary: true, CodeQuality: true, Artifact: true}
}
