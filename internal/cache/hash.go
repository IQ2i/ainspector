package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"

	"github.com/iq2i/ainspector/internal/extractor"
)

const (
	// HashPrefix is the marker prefix used to identify ainspector hashes in comments
	HashPrefix = "<!-- ainspector:fn:"
	// HashSuffix is the marker suffix
	HashSuffix = " -->"
	// HashLength is the length of the short hash (like git short SHA)
	HashLength = 12
)

var hashRegex = regexp.MustCompile(`<!-- ainspector:fn:([a-f0-9]{12}) -->`)

// FunctionHash generates a unique hash for an extracted function.
// The hash is based on file path, function name, content, and diff to ensure
// that any change to the function or its modifications triggers a re-review.
func FunctionHash(fn *extractor.ExtractedFunction) string {
	data := fn.FilePath + ":" + fn.Name + ":" + fn.Content + ":" + fn.Diff
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])[:HashLength]
}

// FormatHashMarker creates the HTML comment marker for embedding in comments.
// The marker is invisible in rendered markdown on GitHub/GitLab.
func FormatHashMarker(hash string) string {
	return HashPrefix + hash + HashSuffix
}

// ExtractHash extracts the hash from a comment body.
// Returns empty string if no valid hash marker is found.
func ExtractHash(commentBody string) string {
	matches := hashRegex.FindStringSubmatch(commentBody)
	if len(matches) < 2 {
		return ""
	}
	return matches[1]
}
