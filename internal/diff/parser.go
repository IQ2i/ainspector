package diff

import (
	"strings"

	"github.com/sourcegraph/go-diff/diff"
)

// ModifiedLines represents the lines that were modified in a file
type ModifiedLines struct {
	Added   []int // Line numbers of added lines (in new file)
	Deleted []int // Line numbers of deleted lines (in old file)
}

// ParsePatch parses a unified diff patch and returns the modified line numbers
func ParsePatch(patch string) (*ModifiedLines, error) {
	if patch == "" {
		return &ModifiedLines{}, nil
	}

	// go-diff expects a full diff format, so we need to add headers if missing
	if !strings.HasPrefix(patch, "---") {
		patch = "--- a/file\n+++ b/file\n" + patch
	}

	fileDiffs, err := diff.ParseMultiFileDiff([]byte(patch))
	if err != nil {
		return nil, err
	}

	result := &ModifiedLines{
		Added:   make([]int, 0),
		Deleted: make([]int, 0),
	}

	for _, fd := range fileDiffs {
		for _, hunk := range fd.Hunks {
			newLine := int(hunk.NewStartLine)
			oldLine := int(hunk.OrigStartLine)

			lines := strings.Split(string(hunk.Body), "\n")
			for _, line := range lines {
				if len(line) == 0 {
					continue
				}

				switch line[0] {
				case '+':
					result.Added = append(result.Added, newLine)
					newLine++
				case '-':
					result.Deleted = append(result.Deleted, oldLine)
					oldLine++
				case ' ':
					// Context line
					newLine++
					oldLine++
				default:
					// No prefix means context line too
					newLine++
					oldLine++
				}
			}
		}
	}

	return result, nil
}

// HasModifiedLineInRange checks if any modified line falls within the given range
func (m *ModifiedLines) HasModifiedLineInRange(startLine, endLine int) bool {
	for _, line := range m.Added {
		if line >= startLine && line <= endLine {
			return true
		}
	}
	return false
}

// ExtractDiffForRange extracts the portion of a patch that affects the given line range
func ExtractDiffForRange(patch string, startLine, endLine int) string {
	if patch == "" {
		return ""
	}

	// go-diff expects a full diff format, so we need to add headers if missing
	if !strings.HasPrefix(patch, "---") {
		patch = "--- a/file\n+++ b/file\n" + patch
	}

	fileDiffs, err := diff.ParseMultiFileDiff([]byte(patch))
	if err != nil {
		return ""
	}

	var result strings.Builder
	for _, fd := range fileDiffs {
		for _, hunk := range fd.Hunks {
			hunkStart := int(hunk.NewStartLine)
			hunkEnd := hunkStart + int(hunk.NewLines) - 1

			// Check if this hunk overlaps with our range
			if hunkEnd >= startLine && hunkStart <= endLine {
				// Extract only the lines that fall within our range
				lines := strings.Split(string(hunk.Body), "\n")
				newLine := int(hunk.NewStartLine)

				for _, line := range lines {
					if len(line) == 0 {
						continue
					}

					inRange := newLine >= startLine && newLine <= endLine

					switch line[0] {
					case '+':
						if inRange {
							result.WriteString(line + "\n")
						}
						newLine++
					case '-':
						// Deleted lines don't have a new line number, but we include them
						// if they're near our range for context
						if inRange || (newLine >= startLine && newLine <= endLine+1) {
							result.WriteString(line + "\n")
						}
					case ' ':
						newLine++
					default:
						newLine++
					}
				}
			}
		}
	}

	return strings.TrimSpace(result.String())
}
