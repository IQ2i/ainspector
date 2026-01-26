package cache

import (
	"github.com/iq2i/ainspector/internal/extractor"
)

// ReviewedComment represents a previously posted review comment with its hash
type ReviewedComment struct {
	Path string
	Line int
	Hash string
	Body string
}

// Tracker tracks which functions have already been reviewed in a PR/MR
type Tracker struct {
	reviewed map[string]bool // map[hash]bool
}

// NewTracker creates a new review tracker
func NewTracker() *Tracker {
	return &Tracker{
		reviewed: make(map[string]bool),
	}
}

// LoadFromComments populates the tracker from existing PR/MR comments.
// It extracts hash markers from comment bodies to identify previously reviewed functions.
func (t *Tracker) LoadFromComments(comments []ReviewedComment) {
	for _, c := range comments {
		if c.Hash != "" {
			t.reviewed[c.Hash] = true
		}
	}
}

// IsReviewed checks if a function has already been reviewed
func (t *Tracker) IsReviewed(fn *extractor.ExtractedFunction) bool {
	hash := FunctionHash(fn)
	return t.reviewed[hash]
}

// FilterUnreviewed returns only functions that haven't been reviewed yet
func (t *Tracker) FilterUnreviewed(functions []extractor.ExtractedFunction) []extractor.ExtractedFunction {
	var result []extractor.ExtractedFunction
	for _, fn := range functions {
		if !t.IsReviewed(&fn) {
			result = append(result, fn)
		}
	}
	return result
}

// ReviewedCount returns the number of functions that have been reviewed
func (t *Tracker) ReviewedCount() int {
	return len(t.reviewed)
}
