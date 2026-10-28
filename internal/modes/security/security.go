package security

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/findings"
	"github.com/iq2i/ainspector/internal/llm"
)

// minConfidence is the confidence under which a finding is not worth a comment
const minConfidence = 0.5

// Config holds what the security mode takes from the configuration
type Config struct {
	Model         string
	Scope         collect.Scope
	MaxFindings   int
	Hunks         collect.HunkPolicy
	Related       collect.RelatedPolicy
	FalsePositive findings.FPConfig
}

// Mode reviews the changes for vulnerabilities
type Mode struct {
	cfg Config
}

// New creates the security mode. An empty model leaves the choice to the client.
func New(cfg Config) *Mode {
	return &Mode{cfg: cfg}
}

func (m *Mode) Name() string { return "security" }

func (m *Mode) Scope() collect.Scope { return m.cfg.Scope }

func (m *Mode) Version() string { return "security/2" }

func (m *Mode) Rules() []string { return nil }

// Accepts takes every unit: functions, functions only touched by deletions, and hunks
func (m *Mode) Accepts(collect.ReviewUnit) bool { return true }

// Needs asks for the units quality never reviews: deletions, and the changes outside functions.
// Related files are only asked for by the scope that gives them.
func (m *Mode) Needs() collect.Request {
	hunks := m.cfg.Hunks
	hunks.Orphans = true
	req := collect.Request{MatchDeleted: true, Hunks: hunks}
	if m.cfg.Scope == collect.ScopeFunctionClassRelated {
		req.Related = m.cfg.Related
	}
	return req
}

func (m *Mode) BuildRequest(u collect.ReviewUnit, pc *collect.ProjectContext) llm.Request {
	return llm.Request{
		Model:    m.cfg.Model,
		System:   buildSystemBlocks(u.Language, pc),
		Messages: []llm.Message{{Role: "user", Content: buildUserPrompt(u)}},
		Schema:   &llm.Schema{Name: "security_review", JSON: reviewSchema},
	}
}

func (m *Mode) Parse(resp llm.Response, u collect.ReviewUnit) ([]findings.Finding, error) {
	if resp.JSON == nil {
		return nil, fmt.Errorf("the reply holds no structured output")
	}
	var doc reviewResponse
	if err := json.Unmarshal(resp.JSON, &doc); err != nil {
		return nil, fmt.Errorf("invalid review document: %w", err)
	}

	var result []findings.Finding
	for _, r := range doc.Findings {
		// A finding is only worth reading with the way to exploit it
		if strings.TrimSpace(r.Scenario) == "" {
			continue
		}

		f := findings.Finding{
			Mode:        m.Name(),
			File:        u.FilePath,
			StartLine:   r.Line,
			EndLine:     r.EndLine,
			Severity:    findings.Severity(r.Severity),
			Category:    r.Category,
			Title:       strings.TrimSpace(r.Title),
			Remediation: strings.TrimSpace(r.Remediation),
			Suggestion:  r.Suggestion,
			Scenario:    strings.TrimSpace(r.Scenario),
			Confidence:  min(max(r.Confidence, 0), 1),
		}
		if f.StartLine <= 0 {
			f.StartLine = u.StartLine
		}
		if f.EndLine < f.StartLine {
			f.EndLine = f.StartLine
		}
		if f.Severity.Rank() == 0 {
			f.Severity = findings.SeverityInfo
		}
		if !slices.Contains(categories, f.Category) {
			f.Category = "other"
		}
		if f.Title == "" {
			f.Title = f.Category
		}
		// The name of a hunk holds line numbers, which move
		anchor := ""
		if u.Kind == collect.KindFunction {
			anchor = u.Name
		}
		f.Fingerprint = findings.Fingerprint(f, anchor)
		result = append(result, f)
	}
	return result, nil
}

func (m *Mode) FilterPolicy() findings.Policy {
	fp := m.cfg.FalsePositive
	return findings.Policy{
		MaxPublished:  m.cfg.MaxFindings,
		MinSeverity:   findings.SeverityLow,
		MinConfidence: minConfidence,
		FalsePositive: &fp,
	}
}
