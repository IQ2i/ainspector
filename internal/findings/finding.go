package findings

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityInfo     Severity = "info"
)

// Rank orders severities, an unknown one ranking below info
func (s Severity) Rank() int {
	switch s {
	case SeverityCritical:
		return 5
	case SeverityHigh:
		return 4
	case SeverityMedium:
		return 3
	case SeverityLow:
		return 2
	case SeverityInfo:
		return 1
	}
	return 0
}

// Finding is an issue reported by a review mode
type Finding struct {
	Mode        string   `json:"mode"`
	File        string   `json:"file"`
	StartLine   int      `json:"start_line"`
	EndLine     int      `json:"end_line"`
	Severity    Severity `json:"severity,omitempty"`
	Category    string   `json:"category,omitempty"`
	Title       string   `json:"title,omitempty"`
	Explanation string   `json:"explanation"`
	Remediation string   `json:"remediation,omitempty"`
	Suggestion  string   `json:"suggestion,omitempty"`
	Scenario    string   `json:"scenario,omitempty"`
	Confidence  float64  `json:"confidence,omitempty"`
	UnitHash    string   `json:"unit_hash"`
	Fingerprint string   `json:"fingerprint,omitempty"`
}

// Reported is what later runs remember of a finding: enough to recognize it and to tell the
// false positive filter what was already said
type Reported struct {
	Fingerprint string `json:"fp"`
	File        string `json:"file"`
	Line        int    `json:"line,omitempty"`
	Category    string `json:"category,omitempty"`
	Title       string `json:"title,omitempty"`
	// EndLine, Severity, Unit and UnitLine are only set on the findings a mode keeps open: they
	// are what the Code Quality report needs, the hash of the unit tells when they stop being
	// true, and the line where the unit started lets the lines follow it when it moves
	EndLine  int      `json:"end,omitempty"`
	Severity Severity `json:"sev,omitempty"`
	Unit     string   `json:"unit,omitempty"`
	UnitLine int      `json:"ul,omitempty"`
}

// maxOpenTitle bounds the text kept for a finding without title, as a state block is small
const maxOpenTitle = 300

// Report summarizes a finding for later runs
func (f Finding) Report() Reported {
	return Reported{Fingerprint: f.Fingerprint, File: f.File, Line: f.StartLine, Category: f.Category, Title: f.Title}
}

// Open summarizes a finding that is still true, for the Code Quality report of later runs.
// A finding without title (quality mode) keeps the start of its explanation instead.
// unitStart and unitName tell where and what the unit that produced it is.
func (f Finding) Open(unitStart int, unitName string) Reported {
	title := oneLine(f.Title)
	if title == "" {
		title = oneLine(f.Explanation)
		if r := []rune(title); len(r) > maxOpenTitle {
			title = string(r[:maxOpenTitle])
		}
	}
	return Reported{
		Fingerprint: CodeFingerprint(f, unitStart, unitName),
		File:        f.File,
		Line:        f.StartLine,
		EndLine:     f.EndLine,
		Category:    f.Category,
		Title:       title,
		Severity:    f.Severity,
		Unit:        f.UnitHash,
		UnitLine:    unitStart,
	}
}

// Policy tells how the findings of a mode are filtered before publication
type Policy struct {
	// MaxPublished caps the findings published per run, the others only reach the artifact.
	// Zero means no cap.
	MaxPublished  int
	MinSeverity   Severity
	MinConfidence float64
	// FalsePositive enables the second pass that discards false positives, nil to skip it
	FalsePositive *FPConfig
}

// Fingerprint identifies a finding independently of its line, so that it survives edits
// of the surrounding code. anchor tells where in the file it lives (the name of the function):
// without it, the same weakness reported in two actions of a controller would be one finding,
// and a new vulnerable action would be taken for one already reported.
func Fingerprint(f Finding, anchor string) string {
	sum := sha256.Sum256([]byte(f.Mode + ":" + f.File + ":" + anchor + ":" + f.Category + ":" + normalize(f.Title)))
	return hex.EncodeToString(sum[:])[:12]
}

// CodeFingerprint identifies a finding in the Code Quality report, where every finding needs
// one: a finding without fingerprint (quality mode) gets one from its place in its unit and its
// text, so that it survives the unit being moved down the file.
// Unlike Fingerprint it is not stored on the finding, so that deduplication is left as it was.
func CodeFingerprint(f Finding, unitStart int, unitName string) string {
	if f.Fingerprint != "" {
		return f.Fingerprint
	}
	line := f.StartLine
	if unitStart > 0 {
		line -= unitStart
	}
	sum := sha256.Sum256([]byte(f.Mode + ":" + f.File + ":" + unitName + ":" + strconv.Itoa(line) + ":" + normalize(f.Explanation)))
	return hex.EncodeToString(sum[:])[:12]
}

func normalize(title string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), " ")
}

// Threshold drops the findings below the severity and confidence the policy asks for
func Threshold(fs []Finding, p Policy) []Finding {
	var kept []Finding
	for _, f := range fs {
		if p.MinSeverity != "" && f.Severity.Rank() < p.MinSeverity.Rank() {
			continue
		}
		if p.MinConfidence > 0 && f.Confidence < p.MinConfidence {
			continue
		}
		kept = append(kept, f)
	}
	return kept
}

// Dedup drops the findings already reported by a previous run, and repeats within fs.
// A finding without fingerprint is always kept. The findings of a previous run that are still
// true are returned as repeated: they are not published again, but remain open.
func Dedup(fs []Finding, seen map[string]bool) (kept, repeated []Finding) {
	known := make(map[string]bool, len(seen))
	for fp := range seen {
		known[fp] = true
	}

	inRun := make(map[string]bool)
	for _, f := range fs {
		if f.Fingerprint != "" {
			if inRun[f.Fingerprint] {
				continue
			}
			inRun[f.Fingerprint] = true
			if known[f.Fingerprint] {
				repeated = append(repeated, f)
				continue
			}
		}
		kept = append(kept, f)
	}
	return kept, repeated
}

// Cap keeps the n most severe findings, then the most confident, and returns the others
// as overflow. n <= 0 means no cap.
func Cap(fs []Finding, n int) (published, overflow []Finding) {
	sorted := append([]Finding(nil), fs...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		switch {
		case a.Severity.Rank() != b.Severity.Rank():
			return a.Severity.Rank() > b.Severity.Rank()
		case a.Confidence != b.Confidence:
			return a.Confidence > b.Confidence
		case a.File != b.File:
			return a.File < b.File
		}
		return a.StartLine < b.StartLine
	})
	if n <= 0 || len(sorted) <= n {
		return sorted, nil
	}
	return sorted[:n], sorted[n:]
}
