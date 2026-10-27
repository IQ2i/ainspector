package findings

type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityInfo     Severity = "info"
)

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

// Policy tells how the findings of a mode are filtered before publication
type Policy struct {
	MaxPublished  int
	MinSeverity   Severity
	MinConfidence float64
}
