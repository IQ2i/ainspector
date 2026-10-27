package state

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"github.com/iq2i/ainspector/internal/collect"
)

const (
	// markerPrefix identifies ainspector unit hashes in comments
	markerPrefix = "<!-- ainspector:fn:"
	markerSuffix = " -->"
	// HashLength is the length of the short hash (like git short SHA)
	HashLength = 12
)

var markerRegex = regexp.MustCompile(`<!-- ainspector:fn:([a-f0-9]{12}) -->`)

// ModeKey is what identifies a review mode in a unit hash
type ModeKey interface {
	Name() string
	Scope() collect.Scope
	Version() string
}

func shortHash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])[:HashLength]
}

// UnitHash identifies a unit for one mode. It changes with the mode version, its scope
// and the project rules, so that any of them invalidates previous reviews.
func UnitHash(m ModeKey, u collect.ReviewUnit, rulesDigest string) string {
	return shortHash(m.Name(), m.Scope().String(), m.Version(), rulesDigest, u.FilePath, u.Name, u.Content, u.Diff)
}

// RulesDigest summarizes the project rules given to a mode
func RulesDigest(rules []string) string {
	return shortHash(rules...)
}

// LegacyFunctionHash is the hash written before unit hashes existed. It is only read
// to recognize old comments, and is still the one written by the quality mode.
func LegacyFunctionHash(u collect.ReviewUnit) string {
	data := u.FilePath + ":" + u.Name + ":" + u.Content + ":" + u.Diff
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])[:HashLength]
}

// FormatMarker creates the HTML comment marker for embedding in comments.
// The marker is invisible in rendered markdown on GitHub/GitLab.
func FormatMarker(hash string) string {
	return markerPrefix + hash + markerSuffix
}

// ExtractHashes returns the hashes of all markers found in a comment body
func ExtractHashes(body string) []string {
	var hashes []string
	for _, m := range markerRegex.FindAllStringSubmatch(body, -1) {
		hashes = append(hashes, m[1])
	}
	return hashes
}
