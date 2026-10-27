package collect

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/iq2i/ainspector/internal/diff"
	"github.com/iq2i/ainspector/internal/provider"
)

// maxHunkBytes caps the content and the diff of a hunk unit
const maxHunkBytes = 24000

// hasExtension tells whether a path is one of the extensions, with or without the dot. A name
// like .env.local is recognized by its prefix since its extension is "local".
func hasExtension(path string, extensions []string) bool {
	base := strings.ToLower(filepath.Base(path))
	ext := filepath.Ext(base)
	for _, e := range extensions {
		e = "." + strings.ToLower(strings.TrimPrefix(e, "."))
		if ext == e || base == e || strings.HasPrefix(base, e+".") {
			return true
		}
	}
	return false
}

func extensionLanguage(path string) string {
	base := strings.ToLower(filepath.Base(path))
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		return "env"
	}
	switch ext := strings.TrimPrefix(filepath.Ext(base), "."); ext {
	case "yml":
		return "yaml"
	case "":
		return "text"
	default:
		return ext
	}
}

// changedLines lists the lines of the new file touched by a patch, added ones and those where
// something was removed, leaving out added blank lines and the ranges already covered by another
// unit. A deletion is located by the line that took its place, which may well be blank: blank
// deletions are already left out of DeletedAt.
func changedLines(m *diff.ModifiedLines, covered [][2]int, fileLines []string) []int {
	var lines []int
	for i, group := range [][]int{m.Added, m.DeletedAt} {
		for _, line := range group {
			if i == 0 && line >= 1 && line <= len(fileLines) && strings.TrimSpace(fileLines[line-1]) == "" {
				continue
			}
			inside := false
			for _, r := range covered {
				if line >= r[0] && line <= r[1] {
					inside = true
					break
				}
			}
			if !inside {
				lines = append(lines, line)
			}
		}
	}
	sort.Ints(lines)
	return lines
}

func splitLines(content string) []string {
	return strings.Split(strings.TrimSuffix(content, "\n"), "\n")
}

// hunkUnits groups changed lines in ranges extended by a few lines of context, merges those
// that overlap, and returns one unit per range
func hunkUnits(file provider.ModifiedFile, content, changeType, language string, lines []int, policy HunkPolicy) []ReviewUnit {
	if len(lines) == 0 {
		return nil
	}
	fileLines := splitLines(content)

	var ranges [][2]int
	for _, line := range lines {
		start := max(1, line-policy.Before)
		end := min(len(fileLines), line+policy.After)
		if n := len(ranges); n > 0 && start <= ranges[n-1][1]+1 {
			ranges[n-1][1] = max(ranges[n-1][1], end)
			continue
		}
		ranges = append(ranges, [2]int{start, end})
	}

	var units []ReviewUnit
	for _, r := range ranges {
		if r[0] > r[1] {
			continue
		}
		body := strings.Join(fileLines[r[0]-1:r[1]], "\n")
		unit := ReviewUnit{
			Kind:       KindHunk,
			Name:       fmt.Sprintf("lines %d-%d", r[0], r[1]),
			StartLine:  r[0],
			EndLine:    r[1],
			Content:    body,
			Diff:       diff.ExtractDiffForRange(file.Patch, r[0], r[1]),
			FilePath:   file.Path,
			Language:   language,
			ChangeType: changeType,
		}
		// A new file is one hunk from end to end: its diff is as long as its content
		if len(body) > maxHunkBytes || len(unit.Diff) > maxHunkBytes {
			unit.Content = cutAtLine(body, maxHunkBytes)
			unit.Diff = cutAtLine(unit.Diff, maxHunkBytes)
			unit.Notes = append(unit.Notes, fmt.Sprintf("%s lines %d-%d were truncated to %d bytes", file.Path, r[0], r[1], maxHunkBytes))
		}
		units = append(units, unit)
	}
	return units
}

// cutAtLine shortens s to at most max bytes, on a line boundary when there is one
func cutAtLine(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := s[:max]
	if i := strings.LastIndex(cut, "\n"); i > 0 {
		return cut[:i]
	}
	return cut
}
