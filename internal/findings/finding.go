package findings

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
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
}

// Report summarizes a finding for later runs
func (f Finding) Report() Reported {
	return Reported{Fingerprint: f.Fingerprint, File: f.File, Line: f.StartLine, Category: f.Category, Title: f.Title}
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
// A finding without fingerprint is always kept.
func Dedup(fs []Finding, seen map[string]bool) []Finding {
	known := make(map[string]bool, len(seen))
	for fp := range seen {
		known[fp] = true
	}

	var kept []Finding
	for _, f := range fs {
		if f.Fingerprint != "" {
			if known[f.Fingerprint] {
				continue
			}
			known[f.Fingerprint] = true
		}
		kept = append(kept, f)
	}
	return kept
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
