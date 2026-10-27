// Package related finds the files that help judge a review unit: the voter behind an
// #[IsGranted] attribute, the repository of an entity, the parents of a Vue component.
// Resolution is textual and driven by naming conventions, never by static analysis.
package related

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/iq2i/ainspector/internal/collect"
)

// Repo gives read access to the local checkout of the project
type Repo interface {
	// Read returns a file of the project, an error when it is missing or out of reach
	Read(path string) (string, error)
	// Glob lists the files matching a pattern, relative to the project root and sorted
	Glob(pattern string) ([]string, error)
	// Grep lists the files matching one of the globs (all files when none) whose content
	// matches the regular expression
	Grep(pattern string, globs ...string) ([]string, error)
}

// Resolver finds the related files of the units it knows about. It returns nothing for any other.
type Resolver interface {
	Name() string
	Resolve(ctx context.Context, u *collect.ReviewUnit, repo Repo) ([]collect.RelatedFile, error)
}

// minPartialBytes is the room a file needs to be worth including truncated
const minPartialBytes = 200

const truncationMarker = "\n... (truncated)"

// Load reads a file of the project as a related file, reporting whether it exists
func Load(repo Repo, path, reason string) (collect.RelatedFile, bool) {
	content, err := repo.Read(path)
	if err != nil {
		return collect.RelatedFile{}, false
	}
	return collect.RelatedFile{Path: path, Reason: reason, Content: content}, true
}

// Attach runs the resolvers on every unit and keeps what the policy allows, in the order the
// resolvers found it. What was left out is reported in the notes of the unit.
func Attach(ctx context.Context, units []collect.ReviewUnit, repo Repo, resolvers []Resolver, p collect.RelatedPolicy) {
	for i := range units {
		if ctx.Err() != nil {
			return
		}
		u := &units[i]

		var found []collect.RelatedFile
		for _, r := range resolvers {
			files, err := r.Resolve(ctx, u, repo)
			if err != nil {
				fmt.Printf("Warning: related files of %s (%s): %v\n", u.FilePath, r.Name(), err)
				continue
			}
			found = append(found, files...)
		}

		kept, omitted, truncated := limit(found, u.FilePath, p)
		u.Related = kept
		if omitted > 0 || truncated > 0 {
			u.Notes = append(u.Notes, fmt.Sprintf("Related files of %s in %s: %d left out and %d truncated to stay within %d files and %d bytes",
				u.Name, u.FilePath, omitted, truncated, p.MaxFiles, p.MaxBytes))
		}
		if len(kept) > 0 {
			fmt.Printf("Attached %d related files to %s in %s\n", len(kept), u.Name, u.FilePath)
		}
	}
}

// limit drops the unit itself and duplicates, then applies the caps on files and bytes. The file
// that crosses the byte cap is truncated when enough room remains, the next ones are left out.
func limit(files []collect.RelatedFile, self string, p collect.RelatedPolicy) (kept []collect.RelatedFile, omitted, truncated int) {
	seen := map[string]bool{self: true}
	used := 0
	for _, f := range files {
		if seen[f.Path] {
			continue
		}
		seen[f.Path] = true

		if p.MaxFiles > 0 && len(kept) >= p.MaxFiles {
			omitted++
			continue
		}
		if p.MaxBytes > 0 && used+len(f.Content) > p.MaxBytes {
			room := p.MaxBytes - used
			if room < minPartialBytes {
				omitted++
				continue
			}
			f.Content = cut(f.Content, room-len(truncationMarker)) + truncationMarker
			f.Truncated = true
			truncated++
		}
		used += len(f.Content)
		kept = append(kept, f)
	}
	return kept, omitted, truncated
}

// cut shortens s to at most n bytes, on a line boundary when there is one and never inside a rune
func cut(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	s = s[:n]
	if i := strings.LastIndex(s, "\n"); i > 0 {
		return s[:i]
	}
	return s
}
