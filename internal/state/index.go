package state

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/findings"
	"github.com/iq2i/ainspector/internal/provider"
)

const (
	stateVersion = 1
	// legacyMode is the only mode that wrote legacy hashes
	legacyMode = "quality"
	// MaxHashes caps the hashes kept in a state block
	MaxHashes = 500
	// MaxReportedBytes caps the size of the findings remembered in a state block
	MaxReportedBytes = 8000
	// MaxOpenBytes caps the size of the open findings remembered in a state block
	MaxOpenBytes = 16000
)

var stateRegex = regexp.MustCompile(`(?s)<!-- ainspector:state:([a-z0-9_-]+) (\{.*?\}) -->`)

// StateBlock is the state of one mode, stored in its summary comment
type StateBlock struct {
	V        int      `json:"v"`
	Scope    string   `json:"scope"`
	Ver      string   `json:"ver"`
	Units    []string `json:"units"`
	Findings []string `json:"findings"`
	// Reported tells what the findings were, to hand them to the false positive filter
	Reported []findings.Reported `json:"reported,omitempty"`
	// Open holds the findings still true after the last run, so that the Code Quality report
	// of a run that skips their units can repeat them
	Open []findings.Reported `json:"open,omitempty"`
}

// FormatStateBlock renders the state block of a mode as an HTML comment
func FormatStateBlock(mode string, b StateBlock) string {
	b.V = stateVersion
	b.Units = capHashes(b.Units)
	b.Findings = capHashes(b.Findings)
	b.Reported = capReported(b.Reported)
	b.Open = capOpen(b.Open)
	if b.Units == nil {
		b.Units = []string{}
	}
	if b.Findings == nil {
		b.Findings = []string{}
	}
	data, _ := json.Marshal(b)
	return fmt.Sprintf("<!-- ainspector:state:%s %s -->", mode, data)
}

// ParseStateBlock extracts the state block of a mode from a comment body
func ParseStateBlock(mode, body string) (StateBlock, bool) {
	for _, m := range stateRegex.FindAllStringSubmatch(body, -1) {
		if m[1] != mode {
			continue
		}
		var b StateBlock
		if err := json.Unmarshal([]byte(m[2]), &b); err != nil || b.V != stateVersion {
			return StateBlock{}, false
		}
		return b, true
	}
	return StateBlock{}, false
}

// MergeHashes returns the union of previous and current, oldest first, keeping the last MaxHashes
func MergeHashes(previous, current []string) []string {
	seen := make(map[string]bool, len(previous)+len(current))
	var merged []string
	for _, h := range append(append([]string{}, previous...), current...) {
		if !seen[h] {
			seen[h] = true
			merged = append(merged, h)
		}
	}
	return capHashes(merged)
}

// MergeReported returns the union of previous and current by fingerprint, oldest first,
// keeping the most recent ones that fit in MaxReportedBytes
func MergeReported(previous, current []findings.Reported) []findings.Reported {
	seen := make(map[string]bool, len(previous)+len(current))
	var merged []findings.Reported
	for _, r := range append(append([]findings.Reported{}, previous...), current...) {
		if !seen[r.Fingerprint] {
			seen[r.Fingerprint] = true
			merged = append(merged, r)
		}
	}
	return capReported(merged)
}

// MergeOpen returns carried then current, a finding of current replacing the one of carried
// that has the same fingerprint
func MergeOpen(carried, current []findings.Reported) []findings.Reported {
	index := make(map[string]int, len(carried)+len(current))
	var merged []findings.Reported
	for _, r := range append(append([]findings.Reported{}, carried...), current...) {
		if i, ok := index[r.Fingerprint]; ok {
			merged[i] = r
			continue
		}
		index[r.Fingerprint] = len(merged)
		merged = append(merged, r)
	}
	return capOpen(merged)
}

// capOpen drops the least severe findings, the oldest first among equals, until the rest fits
// in MaxOpenBytes. The order of the survivors is kept.
func capOpen(rs []findings.Reported) []findings.Reported {
	sizes := make([]int, len(rs))
	total := 0
	for i, r := range rs {
		data, _ := json.Marshal(r)
		sizes[i] = len(data) + 1
		total += sizes[i]
	}
	if total <= MaxOpenBytes {
		return rs
	}

	order := make([]int, len(rs))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		ra, rb := rs[order[a]].Severity.Rank(), rs[order[b]].Severity.Rank()
		if ra != rb {
			return ra > rb
		}
		return order[a] > order[b]
	})
	keep := make(map[int]bool)
	size := 0
	for _, i := range order {
		if size += sizes[i]; size > MaxOpenBytes {
			break
		}
		keep[i] = true
	}
	var kept []findings.Reported
	for i, r := range rs {
		if keep[i] {
			kept = append(kept, r)
		}
	}
	return kept
}

func capReported(rs []findings.Reported) []findings.Reported {
	size := 0
	for i := len(rs) - 1; i >= 0; i-- {
		data, _ := json.Marshal(rs[i])
		if size += len(data) + 1; size > MaxReportedBytes {
			return rs[i+1:]
		}
	}
	return rs
}

func capHashes(hashes []string) []string {
	if len(hashes) > MaxHashes {
		return hashes[len(hashes)-MaxHashes:]
	}
	return hashes
}

// Index knows which units were already reviewed, from the comments of a PR/MR
type Index struct {
	markers  map[string]bool
	findings map[string]bool
	blocks   map[string]StateBlock
}

// SummaryMarker identifies the summary comment of a mode, the only one that holds its state block
func SummaryMarker(mode string) string {
	return "<!-- ainspector:summary:" + mode + " -->"
}

// LoadIndex reads inline markers and state blocks from existing comments. When self is
// set, only the comments it wrote count: anyone else on the PR/MR could forge a marker
// to prevent a unit from being reviewed. A state block only counts in the summary comment of
// its mode: on GitHub self may be an identity shared with other workflows.
func LoadIndex(comments []provider.ExistingComment, self string) *Index {
	ix := &Index{markers: make(map[string]bool), findings: make(map[string]bool), blocks: make(map[string]StateBlock)}
	for _, c := range comments {
		if self != "" && !strings.EqualFold(c.Author, self) {
			continue
		}
		for _, h := range ExtractHashes(c.Body) {
			ix.markers[h] = true
		}
		for _, fp := range ExtractFingerprints(c.Body) {
			ix.findings[fp] = true
		}
		for _, m := range stateRegex.FindAllStringSubmatch(c.Body, -1) {
			if !strings.HasPrefix(strings.TrimSpace(c.Body), SummaryMarker(m[1])) {
				continue
			}
			if b, ok := ParseStateBlock(m[1], c.Body); ok {
				ix.blocks[m[1]] = b
			}
		}
	}
	return ix
}

// ReportedFindings returns the fingerprints of the findings a mode already reported, from its
// inline comments and its state block, and what the block remembers of them
func (ix *Index) ReportedFindings(mode string) (map[string]bool, []findings.Reported) {
	seen := make(map[string]bool, len(ix.findings))
	for fp := range ix.findings {
		seen[fp] = true
	}
	b, ok := ix.blocks[mode]
	if !ok {
		return seen, nil
	}
	for _, fp := range b.Findings {
		seen[fp] = true
	}
	return seen, b.Reported
}

// OpenFindings returns what the last run of a mode left open, as far as the units that were
// skipped this time are concerned: the findings of units that are reviewed again are replaced
// by the ones of the new review. skipped maps the hash of each of those units to the line where
// it starts now, as a unit that did not change may have moved: the lines follow it. A block of
// another version or scope is not trusted, as its unit hashes could not match anymore.
func (ix *Index) OpenFindings(m ModeKey, skipped map[string]int) []findings.Reported {
	b, ok := ix.blocks[m.Name()]
	if !ok || b.Ver != m.Version() || b.Scope != m.Scope().String() {
		return nil
	}
	var open []findings.Reported
	for _, r := range b.Open {
		start, ok := skipped[r.Unit]
		if !ok {
			continue
		}
		if r.UnitLine > 0 && start > 0 {
			delta := start - r.UnitLine
			r.Line += delta
			if r.EndLine > 0 {
				r.EndLine += delta
			}
			r.UnitLine = start
		}
		open = append(open, r)
	}
	return open
}

// IsReviewed tells whether a unit was already reviewed by a mode, whichever hash formula was used
func (ix *Index) IsReviewed(m ModeKey, rulesDigest string, u collect.ReviewUnit) bool {
	hash := UnitHash(m, u, rulesDigest)
	if ix.markers[hash] {
		return true
	}
	// The legacy hash ignores the mode: it only stands for the review that used to exist
	if m.Name() == legacyMode && ix.markers[LegacyFunctionHash(u)] {
		return true
	}
	if b, ok := ix.blocks[m.Name()]; ok {
		for _, h := range b.Units {
			if h == hash {
				return true
			}
		}
	}
	return false
}

// Partition splits the units of a mode into those that were not reviewed yet and those that
// were. The latter come as a map from their hash to the line where each starts now, which is
// what OpenFindings needs to follow a unit that moved.
func (ix *Index) Partition(m ModeKey, rulesDigest string, units []collect.ReviewUnit) (toReview []collect.ReviewUnit, skipped map[string]int) {
	skipped = make(map[string]int)
	for _, u := range units {
		if ix.IsReviewed(m, rulesDigest, u) {
			skipped[UnitHash(m, u, rulesDigest)] = u.StartLine
		} else {
			toReview = append(toReview, u)
		}
	}
	return toReview, skipped
}
