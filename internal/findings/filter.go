package findings

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/llm"
	"github.com/iq2i/ainspector/internal/provider"
)

//go:embed false_positive.md
var defaultFPInstructions string

// FPConfig configures the false positive filter
type FPConfig struct {
	// Model of the filter, empty for the default of the client
	Model string
	// Instructions are appended to the built-in ones
	Instructions string
}

// Verdict is the decision of the filter on one finding
type Verdict struct {
	ID          int    `json:"id"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Keep        bool   `json:"keep"`
	Reason      string `json:"reason"`
}

var verdictSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "verdicts": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "id": {"type": "integer"},
          "keep": {"type": "boolean"},
          "reason": {"type": "string"}
        },
        "required": ["id", "keep", "reason"],
        "additionalProperties": false
      }
    }
  },
  "required": ["verdicts"],
  "additionalProperties": false
}`)

// FilterFalsePositives asks the model, in one call, which findings are false positives. units
// holds the reviewed code by unit hash, previous the findings reported by earlier runs.
// A finding without verdict is kept. On error nothing is filtered: the caller decides.
func FilterFalsePositives(ctx context.Context, c llm.Client, cfg FPConfig, units map[string]collect.ReviewUnit, previous []Reported, fs []Finding) ([]Finding, []Verdict, error) {
	if len(fs) == 0 {
		return nil, nil, nil
	}

	instructions := strings.TrimSpace(defaultFPInstructions)
	if extra := strings.TrimSpace(cfg.Instructions); extra != "" {
		instructions += "\n\nProject specific instructions:\n" + extra
	}

	resp, err := c.Complete(ctx, llm.Request{
		Model:    cfg.Model,
		System:   []llm.Block{{Text: instructions, Cache: true}},
		Messages: []llm.Message{{Role: "user", Content: buildFPPrompt(units, previous, fs)}},
		Schema:   &llm.Schema{Name: "false_positive_verdicts", JSON: verdictSchema},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("false positive filter: %w", err)
	}
	if resp.JSON == nil {
		return nil, nil, fmt.Errorf("false positive filter: the reply holds no structured output")
	}
	var doc struct {
		Verdicts []Verdict `json:"verdicts"`
	}
	if err := json.Unmarshal(resp.JSON, &doc); err != nil {
		return nil, nil, fmt.Errorf("false positive filter: invalid verdicts: %w", err)
	}

	byID := make(map[int]Verdict, len(doc.Verdicts))
	for _, v := range doc.Verdicts {
		if _, ok := byID[v.ID]; ok || v.ID < 0 || v.ID >= len(fs) {
			continue
		}
		v.Fingerprint = fs[v.ID].Fingerprint
		byID[v.ID] = v
	}

	var kept []Finding
	var verdicts []Verdict
	for i, f := range fs {
		v, ok := byID[i]
		if !ok {
			v = Verdict{ID: i, Fingerprint: f.Fingerprint, Keep: true, Reason: "no verdict returned"}
		}
		verdicts = append(verdicts, v)
		if v.Keep {
			kept = append(kept, f)
		}
	}
	return kept, verdicts, nil
}

func buildFPPrompt(units map[string]collect.ReviewUnit, previous []Reported, fs []Finding) string {
	var sb strings.Builder

	sb.WriteString("=== PREVIOUSLY REPORTED FINDINGS (data) ===\n")
	if len(previous) == 0 {
		sb.WriteString("none\n")
	}
	for _, r := range previous {
		fmt.Fprintf(&sb, "- %s:%d [%s] %s\n", oneLine(r.File), r.Line, oneLine(r.Category), oneLine(r.Title))
	}

	sb.WriteString("\n=== CODE UNDER REVIEW (data) ===\n")
	seen := make(map[string]bool)
	for _, f := range fs {
		u, ok := units[f.UnitHash]
		if !ok || seen[f.UnitHash] {
			continue
		}
		seen[f.UnitHash] = true
		fmt.Fprintf(&sb, "\n%s, %s (lines %d-%d)\n", oneLine(u.FilePath), oneLine(u.Name), u.StartLine, u.EndLine)
		if u.Enclosing != nil {
			sb.WriteString("Class header:\n" + provider.Fence(u.Language, u.Enclosing.Rendered) + "\n")
		}
		if u.Diff != "" {
			sb.WriteString("Changes:\n" + provider.Fence("diff", u.Diff) + "\n")
		}
		sb.WriteString("Code:\n" + provider.Fence(u.Language, u.Content) + "\n")
	}

	sb.WriteString("\n=== FINDINGS TO ASSESS (data) ===\n")
	for i, f := range fs {
		// The text of a finding derives from the code under review: it is fenced like the code
		fmt.Fprintf(&sb, "\nid %d: %s:%d, severity %s, category %s, confidence %.2f\nTitle: %s\nScenario:\n%s\nRemediation:\n%s\n",
			i, oneLine(f.File), f.StartLine, f.Severity, oneLine(f.Category), f.Confidence, oneLine(f.Title), provider.Fence("", f.Scenario), provider.Fence("", f.Remediation))
	}
	return sb.String()
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
