package diff

import (
	"testing"
)

func TestParsePatch_EmptyPatch(t *testing.T) {
	result, err := ParsePatch("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Added) != 0 {
		t.Errorf("expected 0 added lines, got %d", len(result.Added))
	}
	if len(result.Deleted) != 0 {
		t.Errorf("expected 0 deleted lines, got %d", len(result.Deleted))
	}
}

func TestParsePatch_AddedLines(t *testing.T) {
	patch := `@@ -1,3 +1,5 @@
 line1
+added1
+added2
 line2
 line3`

	result, err := ParsePatch(patch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedAdded := []int{2, 3}
	if len(result.Added) != len(expectedAdded) {
		t.Fatalf("expected %d added lines, got %d", len(expectedAdded), len(result.Added))
	}
	for i, line := range expectedAdded {
		if result.Added[i] != line {
			t.Errorf("expected added line %d, got %d", line, result.Added[i])
		}
	}
}

func TestParsePatch_DeletedLines(t *testing.T) {
	patch := `@@ -1,5 +1,3 @@
 line1
-deleted1
-deleted2
 line2
 line3`

	result, err := ParsePatch(patch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedDeleted := []int{2, 3}
	if len(result.Deleted) != len(expectedDeleted) {
		t.Fatalf("expected %d deleted lines, got %d", len(expectedDeleted), len(result.Deleted))
	}
	for i, line := range expectedDeleted {
		if result.Deleted[i] != line {
			t.Errorf("expected deleted line %d, got %d", line, result.Deleted[i])
		}
	}
}

func TestParsePatch_MixedChanges(t *testing.T) {
	patch := `@@ -1,4 +1,4 @@
 line1
-old_line
+new_line
 line3
 line4`

	result, err := ParsePatch(patch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Added) != 1 || result.Added[0] != 2 {
		t.Errorf("expected added line 2, got %v", result.Added)
	}
	if len(result.Deleted) != 1 || result.Deleted[0] != 2 {
		t.Errorf("expected deleted line 2, got %v", result.Deleted)
	}
}

func TestParsePatch_MultipleHunks(t *testing.T) {
	patch := `@@ -1,3 +1,4 @@
 line1
+added_at_2
 line2
 line3
@@ -10,3 +11,4 @@
 line10
+added_at_12
 line11
 line12`

	result, err := ParsePatch(patch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Added) != 2 {
		t.Fatalf("expected 2 added lines, got %d", len(result.Added))
	}
	if result.Added[0] != 2 {
		t.Errorf("expected first added line at 2, got %d", result.Added[0])
	}
	if result.Added[1] != 12 {
		t.Errorf("expected second added line at 12, got %d", result.Added[1])
	}
}

func TestParsePatch_WithFullDiffHeaders(t *testing.T) {
	patch := `--- a/file.go
+++ b/file.go
@@ -1,3 +1,4 @@
 line1
+added
 line2
 line3`

	result, err := ParsePatch(patch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Added) != 1 || result.Added[0] != 2 {
		t.Errorf("expected added line 2, got %v", result.Added)
	}
}

func TestHasModifiedLineInRange(t *testing.T) {
	tests := []struct {
		name      string
		added     []int
		startLine int
		endLine   int
		expected  bool
	}{
		{
			name:      "line in range",
			added:     []int{5, 10, 15},
			startLine: 8,
			endLine:   12,
			expected:  true,
		},
		{
			name:      "line at start of range",
			added:     []int{5},
			startLine: 5,
			endLine:   10,
			expected:  true,
		},
		{
			name:      "line at end of range",
			added:     []int{10},
			startLine: 5,
			endLine:   10,
			expected:  true,
		},
		{
			name:      "line before range",
			added:     []int{3},
			startLine: 5,
			endLine:   10,
			expected:  false,
		},
		{
			name:      "line after range",
			added:     []int{15},
			startLine: 5,
			endLine:   10,
			expected:  false,
		},
		{
			name:      "empty added lines",
			added:     []int{},
			startLine: 5,
			endLine:   10,
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &ModifiedLines{Added: tt.added}
			result := m.HasModifiedLineInRange(tt.startLine, tt.endLine)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestExtractDiffForRange_EmptyPatch(t *testing.T) {
	result := ExtractDiffForRange("", 1, 10)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestExtractDiffForRange_NoOverlap(t *testing.T) {
	patch := `@@ -1,3 +1,4 @@
 line1
+added
 line2
 line3`

	result := ExtractDiffForRange(patch, 100, 200)
	if result != "" {
		t.Errorf("expected empty string for non-overlapping range, got %q", result)
	}
}

func TestExtractDiffForRange_FullOverlap(t *testing.T) {
	patch := `@@ -1,3 +1,4 @@
 line1
+added_line
 line2
 line3`

	result := ExtractDiffForRange(patch, 1, 10)
	if result != "+added_line" {
		t.Errorf("expected '+added_line', got %q", result)
	}
}

func TestExtractDiffForRange_PartialOverlap(t *testing.T) {
	patch := `@@ -1,5 +1,6 @@
 line1
+added_at_2
 line2
+added_at_4
 line3
 line4`

	// Only get changes in lines 3-6
	result := ExtractDiffForRange(patch, 3, 6)
	if result != "+added_at_4" {
		t.Errorf("expected '+added_at_4', got %q", result)
	}
}

func TestExtractDiffForRange_WithDeletions(t *testing.T) {
	patch := `@@ -1,4 +1,4 @@
 line1
-old_line
+new_line
 line3
 line4`

	result := ExtractDiffForRange(patch, 2, 2)
	// Should include both deletion and addition at line 2
	expected := "-old_line\n+new_line"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestExtractDiffForRange_MultipleHunks(t *testing.T) {
	patch := `@@ -1,3 +1,4 @@
 line1
+added_first
 line2
 line3
@@ -10,3 +11,4 @@
 line10
+added_second
 line11
 line12`

	// Get only the second hunk
	result := ExtractDiffForRange(patch, 11, 15)
	if result != "+added_second" {
		t.Errorf("expected '+added_second', got %q", result)
	}
}

func TestParsePatch_InvalidPatch(t *testing.T) {
	// go-diff is quite lenient, but completely malformed input should be handled
	patch := "not a valid patch at all"

	// This shouldn't panic, though it may not return useful data
	result, err := ParsePatch(patch)
	if err != nil {
		// Error is acceptable for invalid input
		return
	}
	// If no error, result should be empty
	if result == nil {
		t.Error("expected non-nil result")
	}
}
