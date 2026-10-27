package publish

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/iq2i/ainspector/internal/findings"
)

// Report is a publisher that gathers what every mode produced and writes a file once
type Report interface {
	Publisher
	Write() error
}

// maxQualityDescription bounds the description of an entry of the Code Quality report
const maxQualityDescription = 500

type qualityEntry struct {
	Description string   `json:"description"`
	CheckName   string   `json:"check_name"`
	Fingerprint string   `json:"fingerprint"`
	Severity    string   `json:"severity"`
	Categories  []string `json:"categories,omitempty"`
	Location    struct {
		Path  string `json:"path"`
		Lines struct {
			Begin int `json:"begin"`
			End   int `json:"end,omitempty"`
		} `json:"lines"`
	} `json:"location"`
}

type codeQuality struct {
	dir, path string

	mu      sync.Mutex
	entries []qualityEntry
	seen    map[string]bool
}

// NewCodeQuality builds the report that the GitLab Code Quality widget of a merge request reads:
// a JSON array with one entry per open finding of every mode that was published to it. Besides
// the findings of the run, it repeats those that an earlier run reported and that still hold,
// since the widget shows the report of the last pipeline only.
func NewCodeQuality(dir, path string) Report {
	return &codeQuality{dir: dir, path: path, seen: make(map[string]bool)}
}

func (c *codeQuality) Publish(_ context.Context, run Run) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	open := append(run.openFindings(), run.Carried...)

	for _, r := range open {
		if r.Fingerprint == "" || c.seen[r.Fingerprint] {
			continue
		}
		c.seen[r.Fingerprint] = true
		c.entries = append(c.entries, newQualityEntry(run.Mode, r))
	}
	return nil
}

func (c *codeQuality) Write() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	entries := c.entries
	if entries == nil {
		entries = []qualityEntry{}
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(c.dir, c.path, append(data, '\n'))
}

func newQualityEntry(mode string, r findings.Reported) qualityEntry {
	e := qualityEntry{
		Description: truncateRunes(oneLine(r.Title), maxQualityDescription),
		CheckName:   "ainspector/" + mode,
		Fingerprint: r.Fingerprint,
		Severity:    qualitySeverity(r.Severity),
	}
	if e.Description == "" {
		e.Description = "ainspector " + mode + " finding"
	}
	if r.Category != "" {
		e.CheckName += "/" + r.Category
	}
	if mode == "security" {
		e.Categories = []string{"Security"}
	}

	begin := max(r.Line, 1)
	e.Location.Path = strings.TrimLeft(strings.TrimPrefix(r.File, "./"), "/")
	e.Location.Lines.Begin = begin
	if r.EndLine > begin {
		e.Location.Lines.End = r.EndLine
	}
	return e
}

// qualitySeverity maps a severity to the ones GitLab knows. A finding without severity comes
// from the quality mode, whose findings are suggestions.
func qualitySeverity(s findings.Severity) string {
	switch s {
	case findings.SeverityCritical:
		return "blocker"
	case findings.SeverityHigh:
		return "critical"
	case findings.SeverityMedium:
		return "major"
	case findings.SeverityInfo:
		return "info"
	}
	return "minor"
}

func truncateRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
