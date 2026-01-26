package cache

import (
	"testing"

	"github.com/iq2i/ainspector/internal/extractor"
)

func TestFunctionHash_Deterministic(t *testing.T) {
	fn := &extractor.ExtractedFunction{
		Name:     "TestFunc",
		FilePath: "main.go",
		Content:  "func TestFunc() {}",
		Diff:     "+func TestFunc() {}",
	}

	hash1 := FunctionHash(fn)
	hash2 := FunctionHash(fn)

	if hash1 != hash2 {
		t.Errorf("Hash not deterministic: got %s and %s", hash1, hash2)
	}

	if len(hash1) != HashLength {
		t.Errorf("Hash length = %d, want %d", len(hash1), HashLength)
	}
}

func TestFunctionHash_DifferentContent(t *testing.T) {
	fn1 := &extractor.ExtractedFunction{
		Name:     "TestFunc",
		FilePath: "main.go",
		Content:  "func TestFunc() {}",
		Diff:     "+func TestFunc() {}",
	}

	fn2 := &extractor.ExtractedFunction{
		Name:     "TestFunc",
		FilePath: "main.go",
		Content:  "func TestFunc() { return }",
		Diff:     "+func TestFunc() { return }",
	}

	hash1 := FunctionHash(fn1)
	hash2 := FunctionHash(fn2)

	if hash1 == hash2 {
		t.Error("Different content should produce different hashes")
	}
}

func TestFunctionHash_DifferentPath(t *testing.T) {
	fn1 := &extractor.ExtractedFunction{
		Name:     "TestFunc",
		FilePath: "main.go",
		Content:  "func TestFunc() {}",
		Diff:     "+func TestFunc() {}",
	}

	fn2 := &extractor.ExtractedFunction{
		Name:     "TestFunc",
		FilePath: "other.go",
		Content:  "func TestFunc() {}",
		Diff:     "+func TestFunc() {}",
	}

	hash1 := FunctionHash(fn1)
	hash2 := FunctionHash(fn2)

	if hash1 == hash2 {
		t.Error("Different file paths should produce different hashes")
	}
}

func TestFunctionHash_DifferentDiff(t *testing.T) {
	fn1 := &extractor.ExtractedFunction{
		Name:     "TestFunc",
		FilePath: "main.go",
		Content:  "func TestFunc() { a := 1; b := 2 }",
		Diff:     "+a := 1",
	}

	fn2 := &extractor.ExtractedFunction{
		Name:     "TestFunc",
		FilePath: "main.go",
		Content:  "func TestFunc() { a := 1; b := 2 }",
		Diff:     "+b := 2",
	}

	hash1 := FunctionHash(fn1)
	hash2 := FunctionHash(fn2)

	if hash1 == hash2 {
		t.Error("Different diffs should produce different hashes")
	}
}

func TestFormatHashMarker(t *testing.T) {
	hash := "abc123def456"
	marker := FormatHashMarker(hash)
	expected := "<!-- ainspector:fn:abc123def456 -->"

	if marker != expected {
		t.Errorf("FormatHashMarker = %q, want %q", marker, expected)
	}
}

func TestExtractHash_ValidMarker(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected string
	}{
		{
			name:     "marker at end",
			body:     "Some review comment\n\n<!-- ainspector:fn:abc123def456 -->",
			expected: "abc123def456",
		},
		{
			name:     "marker in middle",
			body:     "Comment <!-- ainspector:fn:123456789abc --> more text",
			expected: "123456789abc",
		},
		{
			name:     "marker only",
			body:     "<!-- ainspector:fn:fedcba987654 -->",
			expected: "fedcba987654",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := ExtractHash(tt.body)
			if hash != tt.expected {
				t.Errorf("ExtractHash = %q, want %q", hash, tt.expected)
			}
		})
	}
}

func TestExtractHash_NoMarker(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"empty string", ""},
		{"no marker", "Just a regular comment"},
		{"partial marker", "<!-- ainspector:fn: -->"},
		{"wrong prefix", "<!-- other:fn:abc123def456 -->"},
		{"short hash", "<!-- ainspector:fn:abc123 -->"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := ExtractHash(tt.body)
			if hash != "" {
				t.Errorf("ExtractHash = %q, want empty string", hash)
			}
		})
	}
}

func TestTracker_LoadFromComments(t *testing.T) {
	tracker := NewTracker()

	comments := []ReviewedComment{
		{Path: "main.go", Line: 10, Hash: "abc123def456", Body: "Review 1"},
		{Path: "main.go", Line: 20, Hash: "fedcba987654", Body: "Review 2"},
		{Path: "other.go", Line: 5, Hash: "", Body: "Comment without hash"},
	}

	tracker.LoadFromComments(comments)

	if tracker.ReviewedCount() != 2 {
		t.Errorf("ReviewedCount = %d, want 2", tracker.ReviewedCount())
	}
}

func TestTracker_FilterUnreviewed(t *testing.T) {
	tracker := NewTracker()

	// Create a function and compute its hash
	fn1 := extractor.ExtractedFunction{
		Name:     "Func1",
		FilePath: "main.go",
		Content:  "func Func1() {}",
		Diff:     "+func Func1() {}",
	}
	hash1 := FunctionHash(&fn1)

	// Load the hash as already reviewed
	tracker.LoadFromComments([]ReviewedComment{
		{Hash: hash1},
	})

	// Create another function that hasn't been reviewed
	fn2 := extractor.ExtractedFunction{
		Name:     "Func2",
		FilePath: "main.go",
		Content:  "func Func2() {}",
		Diff:     "+func Func2() {}",
	}

	functions := []extractor.ExtractedFunction{fn1, fn2}
	unreviewed := tracker.FilterUnreviewed(functions)

	if len(unreviewed) != 1 {
		t.Errorf("FilterUnreviewed returned %d functions, want 1", len(unreviewed))
	}

	if len(unreviewed) > 0 && unreviewed[0].Name != "Func2" {
		t.Errorf("FilterUnreviewed returned %s, want Func2", unreviewed[0].Name)
	}
}

func TestTracker_IsReviewed(t *testing.T) {
	tracker := NewTracker()

	fn := &extractor.ExtractedFunction{
		Name:     "TestFunc",
		FilePath: "main.go",
		Content:  "func TestFunc() {}",
		Diff:     "+func TestFunc() {}",
	}

	// Initially not reviewed
	if tracker.IsReviewed(fn) {
		t.Error("Function should not be reviewed initially")
	}

	// Mark as reviewed
	hash := FunctionHash(fn)
	tracker.LoadFromComments([]ReviewedComment{{Hash: hash}})

	// Now should be reviewed
	if !tracker.IsReviewed(fn) {
		t.Error("Function should be reviewed after loading hash")
	}
}
