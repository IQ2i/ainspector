package collect

import (
	"context"
	"fmt"
	"strings"

	"github.com/iq2i/ainspector/internal/config"
	"github.com/iq2i/ainspector/internal/diff"
	"github.com/iq2i/ainspector/internal/parser"
	"github.com/iq2i/ainspector/internal/provider"
)

// Scope is the amount of context gathered around each reviewed unit
type Scope int

const (
	ScopeFunction Scope = iota
	ScopeFunctionClass
	ScopeFunctionClassRelated
)

// ParseScope reads a scope name as written in the configuration
func ParseScope(name string) (Scope, error) {
	for _, s := range []Scope{ScopeFunction, ScopeFunctionClass, ScopeFunctionClassRelated} {
		if s.String() == name {
			return s, nil
		}
	}
	return 0, fmt.Errorf("unknown scope %q", name)
}

func (s Scope) String() string {
	switch s {
	case ScopeFunction:
		return "function"
	case ScopeFunctionClass:
		return "function+class"
	case ScopeFunctionClassRelated:
		return "function+class+related"
	}
	return "unknown"
}

// UnitKind tells how a ReviewUnit was built
type UnitKind string

const (
	KindFunction UnitKind = "function"
	// KindHunk is a range of lines around the changes of a file that has no function to review
	KindHunk UnitKind = "hunk"
)

// maxHeaderBytes caps the class header sent with a unit
const maxHeaderBytes = 12000

// ReviewUnit is a piece of modified code sent to the LLM for review
type ReviewUnit struct {
	Kind       UnitKind `json:"kind"`
	Name       string   `json:"name"`
	StartLine  int      `json:"start_line"`
	EndLine    int      `json:"end_line"`
	Content    string   `json:"content"`
	Diff       string   `json:"diff"` // The diff/patch for this specific function
	FilePath   string   `json:"file_path"`
	Language   string   `json:"language"`
	ChangeType string   `json:"change_type"` // "added", "modified", "deleted"
	// Enclosing is the header of the class holding the function, set from ScopeFunctionClass
	Enclosing *ClassHeader `json:"enclosing,omitempty"`
	// Related holds the files that help judge the unit, set from ScopeFunctionClassRelated
	Related []RelatedFile `json:"related,omitempty"`
	// DeletedOnly is set when the unit was selected because lines were removed from it
	DeletedOnly bool `json:"deleted_only,omitempty"`
	// Notes report what was cut or left out while building the unit
	Notes []string `json:"notes,omitempty"`
}

// ClassHeader is the rendered header of the class enclosing a function
type ClassHeader struct {
	Name      string `json:"name"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Rendered  string `json:"rendered"`
}

// RelatedFile is a file of the project, other than the one under review, given as context
type RelatedFile struct {
	Path      string `json:"path"`
	Reason    string `json:"reason"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated,omitempty"`
}

// RelatedPolicy tells which related files are attached to a unit, and how many
type RelatedPolicy struct {
	// Presets name the resolvers to run (symfony, vue)
	Presets []string
	// MaxFiles and MaxBytes cap the related files of one unit
	MaxFiles, MaxBytes int
}

// HunkPolicy tells which changes become hunk units
type HunkPolicy struct {
	// Extensions of the files reviewed as hunks, since they have no functions (twig, yaml, env...)
	Extensions    []string
	Before, After int
	// Orphans also turns the changes made outside any function of a parsed file into hunks
	Orphans bool
}

// Request describes what to collect from the modified files of a PR/MR
type Request struct {
	Files []provider.ModifiedFile
	Scope Scope
	// MatchDeleted also selects the functions that only had lines removed
	MatchDeleted bool
	Hunks        HunkPolicy
	// Related is read by the pipeline, which attaches the files once the units are collected
	Related RelatedPolicy
}

// Collector builds review units from PR/MR files
type Collector struct {
	provider provider.Provider
	parser   *parser.Parser
	config   *config.Config
}

// New creates a new Collector
func New(p provider.Provider, cfg *config.Config) *Collector {
	return &Collector{
		provider: p,
		parser:   parser.NewParser(),
		config:   cfg,
	}
}

// Close releases resources
func (e *Collector) Close() {
	if e.parser != nil {
		e.parser.Close()
	}
}

// Collect extracts a review unit for every function that has modified lines
func (e *Collector) Collect(ctx context.Context, req Request) ([]ReviewUnit, error) {
	var result []ReviewUnit

	for _, file := range req.Files {
		// Skip deleted files
		if file.Status == "deleted" {
			continue
		}

		// Skip files matching ignore patterns from config
		if e.config != nil && e.config.ShouldIgnore(file.Path) {
			fmt.Printf("Skipping ignored file: %s\n", file.Path)
			continue
		}

		parsed := parser.IsSupported(file.Path)
		if !parsed && !hasExtension(file.Path, req.Hunks.Extensions) {
			continue
		}

		functions, err := e.extractFromFile(ctx, file, req, parsed)
		if err != nil {
			// Log error but continue with other files
			fmt.Printf("Warning: failed to extract functions from %s: %v\n", file.Path, err)
			continue
		}

		result = append(result, functions...)
	}

	return result, nil
}

func (e *Collector) extractFromFile(ctx context.Context, file provider.ModifiedFile, req Request, parsed bool) ([]ReviewUnit, error) {
	// Get file content
	content, err := e.provider.GetFileContent(ctx, file.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to get file content: %w", err)
	}

	// Parse the diff to get modified lines
	modifiedLines, err := diff.ParsePatch(file.Patch)
	if err != nil {
		return nil, fmt.Errorf("failed to parse patch: %w", err)
	}

	changeType := "modified"
	if file.Status == "added" {
		changeType = "added"
	}

	if isGenerated(file.Path) && (!parsed || req.Hunks.Orphans) {
		// Generated files never become hunks. A minified script still gets the units of its functions.
		if !parsed {
			return nil, nil
		}
		req.Hunks.Orphans = false
	}

	if !parsed {
		return hunkUnits(file, content, changeType, extensionLanguage(file.Path), changedLines(modifiedLines, nil, splitLines(content)), req.Hunks), nil
	}

	// Parse the file to get all functions
	functions, language, err := e.parser.Parse(file.Path, []byte(content))
	if err != nil {
		return nil, fmt.Errorf("failed to parse file: %w", err)
	}

	// Filter functions that have modified lines
	var result []ReviewUnit
	headers := make(map[uint]*ClassHeader)
	for _, fn := range functions {
		added := modifiedLines.HasModifiedLineInRange(fn.StartLine, fn.EndLine)
		deleted := req.MatchDeleted && modifiedLines.HasDeletionInRange(fn.StartLine, fn.EndLine)
		if !added && !deleted {
			continue
		}

		unit := ReviewUnit{
			Kind:        KindFunction,
			Name:        fn.Name,
			StartLine:   fn.StartLine,
			EndLine:     fn.EndLine,
			Content:     fn.Content,
			Diff:        diff.ExtractDiffForRange(file.Patch, fn.StartLine, fn.EndLine),
			FilePath:    file.Path,
			Language:    language,
			ChangeType:  changeType,
			DeletedOnly: !added,
		}
		if req.Scope >= ScopeFunctionClass && fn.Enclosing != nil {
			unit.Enclosing = e.classHeader(file.Path, content, fn.Enclosing, headers)
			if unit.Enclosing != nil && strings.HasSuffix(unit.Enclosing.Rendered, headerTruncated) {
				unit.Notes = append(unit.Notes, fmt.Sprintf("The header of class %s in %s was truncated to %d bytes", unit.Enclosing.Name, file.Path, maxHeaderBytes))
			}
		}
		result = append(result, unit)
	}

	if req.Hunks.Orphans {
		var covered [][2]int
		for _, fn := range functions {
			covered = append(covered, [2]int{fn.StartLine, fn.EndLine})
		}
		result = append(result, hunkUnits(file, content, changeType, language, changedLines(modifiedLines, covered, splitLines(content)), req.Hunks)...)
	}

	return result, nil
}

const headerTruncated = "\n// ... truncated"

// classHeader renders the header of a class once per file, however many of its methods changed
func (e *Collector) classHeader(path, content string, ref *parser.ClassRef, cache map[uint]*ClassHeader) *ClassHeader {
	if h, ok := cache[ref.StartByte]; ok {
		return h
	}

	var header *ClassHeader
	rendered, err := e.parser.RenderClassHeader(path, []byte(content), ref)
	if err != nil {
		fmt.Printf("Warning: failed to render the class header of %s in %s: %v\n", ref.Name, path, err)
	} else {
		if len(rendered) > maxHeaderBytes {
			rendered = cutAtLine(rendered, maxHeaderBytes) + headerTruncated
		}
		header = &ClassHeader{Name: ref.Name, StartLine: ref.StartLine, EndLine: ref.EndLine, Rendered: rendered}
	}
	cache[ref.StartByte] = header
	return header
}
