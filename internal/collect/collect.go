package collect

import (
	"context"
	"fmt"

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

const KindFunction UnitKind = "function"

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
}

// Request describes what to collect from the modified files of a PR/MR
type Request struct {
	Files []provider.ModifiedFile
	Scope Scope
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
	if req.Scope != ScopeFunction {
		return nil, fmt.Errorf("scope %s is not supported yet", req.Scope)
	}

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

		// Skip unsupported file types
		if !parser.IsSupported(file.Path) {
			continue
		}

		functions, err := e.extractFromFile(ctx, file)
		if err != nil {
			// Log error but continue with other files
			fmt.Printf("Warning: failed to extract functions from %s: %v\n", file.Path, err)
			continue
		}

		result = append(result, functions...)
	}

	return result, nil
}

func (e *Collector) extractFromFile(ctx context.Context, file provider.ModifiedFile) ([]ReviewUnit, error) {
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

	// Parse the file to get all functions
	functions, language, err := e.parser.Parse(file.Path, []byte(content))
	if err != nil {
		return nil, fmt.Errorf("failed to parse file: %w", err)
	}

	// Filter functions that have modified lines
	var result []ReviewUnit
	for _, fn := range functions {
		if modifiedLines.HasModifiedLineInRange(fn.StartLine, fn.EndLine) {
			changeType := "modified"
			if file.Status == "added" {
				changeType = "added"
			}

			// Extract the diff specific to this function
			fnDiff := diff.ExtractDiffForRange(file.Patch, fn.StartLine, fn.EndLine)

			result = append(result, ReviewUnit{
				Kind:       KindFunction,
				Name:       fn.Name,
				StartLine:  fn.StartLine,
				EndLine:    fn.EndLine,
				Content:    fn.Content,
				Diff:       fnDiff,
				FilePath:   file.Path,
				Language:   language,
				ChangeType: changeType,
			})
		}
	}

	return result, nil
}
