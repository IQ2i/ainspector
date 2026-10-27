package state

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/provider"
)

const (
	stateVersion = 1
	// legacyMode is the only mode that wrote legacy hashes
	legacyMode = "quality"
	// MaxHashes caps the hashes kept in a state block
	MaxHashes = 500
)

var stateRegex = regexp.MustCompile(`(?s)<!-- ainspector:state:([a-z0-9_-]+) (\{.*?\}) -->`)

// StateBlock is the state of one mode, stored in its summary comment
type StateBlock struct {
	V        int      `json:"v"`
	Scope    string   `json:"scope"`
	Ver      string   `json:"ver"`
	Units    []string `json:"units"`
	Findings []string `json:"findings"`
}

// FormatStateBlock renders the state block of a mode as an HTML comment
func FormatStateBlock(mode string, b StateBlock) string {
	b.V = stateVersion
	b.Units = capHashes(b.Units)
	b.Findings = capHashes(b.Findings)
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

func capHashes(hashes []string) []string {
	if len(hashes) > MaxHashes {
		return hashes[len(hashes)-MaxHashes:]
	}
	return hashes
}

// Index knows which units were already reviewed, from the comments of a PR/MR
type Index struct {
	markers map[string]bool
	blocks  map[string]StateBlock
}

// LoadIndex reads inline markers and state blocks from existing comments. When self is
// set, only the comments it wrote count: anyone else on the PR/MR could forge a marker
// to prevent a unit from being reviewed.
func LoadIndex(comments []provider.ExistingComment, self string) *Index {
	ix := &Index{markers: make(map[string]bool), blocks: make(map[string]StateBlock)}
	for _, c := range comments {
		if self != "" && !strings.EqualFold(c.Author, self) {
			continue
		}
		for _, h := range ExtractHashes(c.Body) {
			ix.markers[h] = true
		}
		for _, m := range stateRegex.FindAllStringSubmatch(c.Body, -1) {
			if b, ok := ParseStateBlock(m[1], c.Body); ok {
				ix.blocks[m[1]] = b
			}
		}
	}
	return ix
}

// Block returns the state block previously written for a mode
func (ix *Index) Block(mode string) (StateBlock, bool) {
	b, ok := ix.blocks[mode]
	return b, ok
}

// Len returns the number of distinct inline markers found
func (ix *Index) Len() int {
	return len(ix.markers)
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

// FilterUnreviewed returns only the units that were not reviewed yet
func (ix *Index) FilterUnreviewed(m ModeKey, rulesDigest string, units []collect.ReviewUnit) []collect.ReviewUnit {
	var result []collect.ReviewUnit
	for _, u := range units {
		if !ix.IsReviewed(m, rulesDigest, u) {
			result = append(result, u)
		}
	}
	return result
}
