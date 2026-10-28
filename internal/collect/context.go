package collect

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/iq2i/ainspector/internal/config"
)

// ProjectContext holds the project files given to the LLM with every request
type ProjectContext struct {
	// Description is the content of the files, each one under a "=== path ===" header
	Description string
	Languages   []string
	// Notes tell what the size limit cut, to report in the review summary
	Notes []string
	// ChangedFiles lists the files of the PR/MR, for the modes that reason about what changed
	// alongside the code (tests next to a controller)
	ChangedFiles []string
}

// minPartialBytes is the room a file needs to be worth including truncated
const minPartialBytes = 200

const truncationMarker = "\n... (truncated)\n"

// Files looked for when the project does not choose its own. Instructions come first as they
// carry the project rules, then the README, then the dependency manifests: when the size limit
// cuts the context, the least useful files go first. Lock and checksum files are left out.
var (
	instructionFiles = []string{"CLAUDE.md", "AGENTS.md", ".cursorrules", ".github/copilot-instructions.md"}
	readmeFile       = "README.md"
	manifestPatterns = []string{
		"go.mod",
		"package.json", "tsconfig.json",
		"requirements.txt", "pyproject.toml", "setup.py", "Pipfile",
		"composer.json",
		"Cargo.toml",
		"Gemfile",
		"pom.xml", "build.gradle",
		"*.csproj", "packages.config",
	}
)

type contextFile struct {
	path, content string
}

// GenerateProjectContext reads the project files that describe the project: the ones listed
// in context.include, or else the well-known instruction, README and manifest files.
// The result never exceeds context.max_bytes.
func GenerateProjectContext(projectRoot string, languages []string, contextConfig *config.ContextConfig) (*ProjectContext, error) {
	limit := config.DefaultContextMaxBytes
	if contextConfig != nil {
		limit = contextConfig.Limit()
	}

	var files []contextFile
	if contextConfig != nil && len(contextConfig.Include) > 0 {
		configured, warnings, err := contextConfig.CollectContextFiles(projectRoot)
		if err != nil {
			return nil, fmt.Errorf("failed to collect context files: %w", err)
		}
		for _, warning := range warnings {
			fmt.Printf("Warning: %s\n", warning)
		}

		paths := make([]string, 0, len(configured))
		for path := range configured {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			files = append(files, contextFile{path, configured[path]})
		}
	} else {
		var warnings []string
		var err error
		if files, warnings, err = findContextFiles(projectRoot, limit); err != nil {
			return nil, fmt.Errorf("failed to find context files: %w", err)
		}
		for _, warning := range warnings {
			fmt.Printf("Warning: %s\n", warning)
		}
	}

	description, notes := renderContext(files, limit)
	return &ProjectContext{Description: description, Languages: languages, Notes: notes}, nil
}

// findContextFiles returns the well-known project files that exist, in priority order. A file
// that resolves outside the project is skipped with a warning, the others that cannot be read
// are skipped silently.
func findContextFiles(projectRoot string, limit int) ([]contextFile, []string, error) {
	var files []contextFile
	var warnings []string
	add := func(relPath string) {
		content, err := config.ReadProjectFile(projectRoot, relPath, limit)
		if err != nil {
			if errors.Is(err, config.ErrOutsideRoot) || errors.Is(err, config.ErrSensitive) {
				warnings = append(warnings, fmt.Sprintf("context file ignored, it resolves outside the project or to a secret: %q", relPath))
			}
			return
		}
		files = append(files, contextFile{filepath.ToSlash(relPath), string(content)})
	}

	for _, name := range instructionFiles {
		add(name)
	}
	add(readmeFile)

	var manifests []string
	for _, pattern := range manifestPatterns {
		matches, err := filepath.Glob(filepath.Join(projectRoot, pattern))
		if err != nil {
			return nil, nil, err
		}
		for _, match := range matches {
			if rel, err := filepath.Rel(projectRoot, match); err == nil {
				manifests = append(manifests, rel)
			}
		}
	}
	sort.Strings(manifests)
	for _, rel := range manifests {
		add(rel)
	}

	return files, warnings, nil
}

// renderContext concatenates the files up to limit bytes. The file that crosses the limit is
// cut when enough room remains to be useful, the following ones are left out.
func renderContext(files []contextFile, limit int) (string, []string) {
	var sb strings.Builder

	for i, f := range files {
		header := "=== " + f.path + " ===\n"
		entry := header + f.content + "\n\n"
		if sb.Len()+len(entry) <= limit {
			sb.WriteString(entry)
			continue
		}

		cut := ""
		omitted := files[i:]
		if room := limit - sb.Len() - len(header) - len(truncationMarker); room >= minPartialBytes {
			sb.WriteString(header)
			sb.WriteString(cutAtRune(f.content, room))
			sb.WriteString(truncationMarker)
			cut = f.path + " truncated"
			omitted = files[i+1:]
		}

		note := fmt.Sprintf("Project context limited to %d bytes", limit)
		if cut != "" {
			note += ": " + cut
		}
		if len(omitted) > 0 {
			names := make([]string, len(omitted))
			for n, o := range omitted {
				names[n] = o.path
			}
			note += "; left out: " + strings.Join(names, ", ")
		}
		return sb.String(), []string{note}
	}

	return sb.String(), nil
}

// cutAtRune truncates s to at most n bytes without splitting a multi-byte character
func cutAtRune(s string, n int) string {
	if n >= len(s) {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
