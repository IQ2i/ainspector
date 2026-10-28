package publish

import (
	"encoding/json"
	"strings"

	"github.com/iq2i/ainspector/internal/findings"
)

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

// WriteCodeQuality writes the report that the GitLab Code Quality widget of a merge request
// reads: a JSON array with one entry per open finding of every mode that was published to it.
// Besides the findings of the run, it repeats those that an earlier run reported and that still
// hold, since the widget shows the report of the last pipeline only.
func WriteCodeQuality(dir, path string, runs []Run) error {
	entries := []qualityEntry{}
	seen := make(map[string]bool)
	for _, run := range runs {
		for _, r := range append(run.openFindings(), run.Carried...) {
			if r.Fingerprint == "" || seen[r.Fingerprint] {
				continue
			}
			seen[r.Fingerprint] = true
			entries = append(entries, newQualityEntry(run.Mode, r))
		}
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(dir, path, append(data, '\n'))
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
