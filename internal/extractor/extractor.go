package extractor

import (
	"context"
	"fmt"

	"github.com/iq2i/ainspector/internal/config"
	"github.com/iq2i/ainspector/internal/diff"
	"github.com/iq2i/ainspector/internal/parser"
	"github.com/iq2i/ainspector/internal/provider"
)

// ExtractedFunction represents a function that was modified in a PR/MR
type ExtractedFunction struct {
	Name       string `json:"name"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	Content    string `json:"content"`
	Diff       string `json:"diff"` // The diff/patch for this specific function
	FilePath   string `json:"file_path"`
	Language   string `json:"language"`
	ChangeType string `json:"change_type"` // "added", "modified", "deleted"
}

// Extractor extracts modified functions from PR/MR files
type Extractor struct {
	provider provider.Provider
	parser   *parser.Parser
	config   *config.Config
}

// New creates a new Extractor
func New(p provider.Provider, cfg *config.Config) *Extractor {
	return &Extractor{
		provider: p,
		parser:   parser.NewParser(),
		config:   cfg,
	}
}

// Close releases resources
func (e *Extractor) Close() {
	if e.parser != nil {
		e.parser.Close()
	}
}

// ExtractModifiedFunctions extracts all functions that have modified lines
func (e *Extractor) ExtractModifiedFunctions(ctx context.Context, files []provider.ModifiedFile) ([]ExtractedFunction, error) {
	var result []ExtractedFunction

	for _, file := range files {
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

func (e *Extractor) extractFromFile(ctx context.Context, file provider.ModifiedFile) ([]ExtractedFunction, error) {
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
	var result []ExtractedFunction
	for _, fn := range functions {
		if modifiedLines.HasModifiedLineInRange(fn.StartLine, fn.EndLine) {
			changeType := "modified"
			if file.Status == "added" {
				changeType = "added"
			}

			// Extract the diff specific to this function
			fnDiff := diff.ExtractDiffForRange(file.Patch, fn.StartLine, fn.EndLine)

			result = append(result, ExtractedFunction{
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
