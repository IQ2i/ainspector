package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/iq2i/ainspector/internal/extractor"
)

// LGTMMarker is returned by the LLM when there are no issues to report.
const LGTMMarker = "LGTM"

const systemPrompt = `You are an expert code reviewer. You will receive a function along with the diff showing only the modified lines.

IMPORTANT: Focus your review ONLY on the changes shown in the diff. The full function is provided for context only - do not review unchanged code.

For the modified lines, identify ONLY actual issues:
- Bugs or logic errors
- Security vulnerabilities
- Serious performance problems
- Violations of language best practices
- Code that could cause runtime errors

RESPONSE FORMAT:
If there are NO issues, respond with exactly: LGTM

If there ARE issues, respond with a JSON object in this exact format:
{
  "issues": [
    {
      "line": <line number in the file where the issue is>,
      "description": "<brief description of the issue>",
      "suggestion": "<corrected code to replace the problematic line(s), or empty string if no fix suggested>"
    }
  ]
}

IMPORTANT RULES:
- The "line" field must be an actual line number from the file (between the function's start and end lines)
- The "suggestion" field should contain the corrected code that can replace the problematic code
- Do NOT comment on code style, formatting, or minor improvements
- Do NOT give positive feedback or praise
- Only report problems that should be fixed
- Respond in the same language as the code comments, or in English if there are no comments`

// Suggestion represents a code suggestion for a specific issue.
type Suggestion struct {
	Line        int    `json:"line"`
	Description string `json:"description"`
	Code        string `json:"suggestion"`
}

// ReviewResponse is the structured response from the LLM.
type ReviewResponse struct {
	Issues []Suggestion `json:"issues"`
}

// ReviewResult contains the review result for a function.
type ReviewResult struct {
	Function    extractor.ExtractedFunction
	Suggestions []Suggestion
	RawReview   string // Original response for debugging
	Error       error
}

// HasIssues returns true if the review contains actual issues to report.
// Returns false if the LLM responded with LGTM or if there was an error.
func (r *ReviewResult) HasIssues() bool {
	if r.Error != nil {
		return false
	}
	return len(r.Suggestions) > 0
}

// ReviewFunctions reviews each function using the LLM and returns the results.
func ReviewFunctions(ctx context.Context, client *Client, functions []extractor.ExtractedFunction) []ReviewResult {
	results := make([]ReviewResult, 0, len(functions))

	for _, fn := range functions {
		result := ReviewResult{Function: fn}

		userPrompt := buildUserPrompt(&fn)
		messages := []ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		}

		review, err := client.Complete(ctx, messages)
		if err != nil {
			result.Error = err
			results = append(results, result)
			continue
		}

		result.RawReview = review
		result.Suggestions = parseReviewResponse(review)
		results = append(results, result)
	}

	return results
}

// parseReviewResponse parses the LLM response into structured suggestions.
func parseReviewResponse(response string) []Suggestion {
	trimmed := strings.TrimSpace(response)

	// Check for LGTM response
	if trimmed == LGTMMarker {
		return nil
	}

	// Try to parse as JSON
	var reviewResp ReviewResponse
	if err := json.Unmarshal([]byte(trimmed), &reviewResp); err != nil {
		// If JSON parsing fails, try to extract JSON from the response
		// (LLM might add extra text before/after the JSON)
		startIdx := strings.Index(trimmed, "{")
		endIdx := strings.LastIndex(trimmed, "}")
		if startIdx >= 0 && endIdx > startIdx {
			jsonStr := trimmed[startIdx : endIdx+1]
			if err := json.Unmarshal([]byte(jsonStr), &reviewResp); err != nil {
				// If still failing, return empty (treat as no issues)
				return nil
			}
		} else {
			return nil
		}
	}

	return reviewResp.Issues
}

func buildUserPrompt(fn *extractor.ExtractedFunction) string {
	diffSection := ""
	if fn.Diff != "" {
		diffSection = fmt.Sprintf("\n\n## Changes (REVIEW THESE):\n```diff\n%s\n```", fn.Diff)
	}

	return fmt.Sprintf("Review the changes in this %s function:\n\nFile: %s\nFunction: %s (lines %d-%d)\nChange type: %s%s\n\n## Full function (for context only, DO NOT review unchanged code):\n```%s\n%s\n```",
		fn.Language,
		fn.FilePath,
		fn.Name,
		fn.StartLine,
		fn.EndLine,
		fn.ChangeType,
		diffSection,
		fn.Language,
		fn.Content,
	)
}
