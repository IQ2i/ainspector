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

const baseSystemPrompt = `You are an expert code reviewer. You will receive a function along with the diff showing only the modified lines.

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

// languageSpecificRules maps language names to their specific review rules
var languageSpecificRules = map[string]string{
	"go": `
LANGUAGE-SPECIFIC CHECKS FOR GO:
- Verify all errors are properly handled (no ignored errors)
- Check for potential nil pointer dereferences
- Ensure goroutines won't leak (proper cleanup/cancellation)
- Verify context is properly propagated in function signatures
- Check for race conditions in concurrent code
- Ensure defer statements are used correctly (not in loops unless intended)
- Verify proper use of channels (close on sender side, check for closed channels)`,

	"javascript": `
LANGUAGE-SPECIFIC CHECKS FOR JAVASCRIPT:
- Verify async functions properly await promises
- Check for unhandled promise rejections
- Ensure variables are properly scoped (avoid var, prefer const/let)
- Check for potential null/undefined access
- Verify proper error handling in async/await blocks
- Check for memory leaks (event listeners, timers not cleaned up)
- Ensure proper use of === instead of ==`,

	"typescript": `
LANGUAGE-SPECIFIC CHECKS FOR TYPESCRIPT:
- Verify async functions properly await promises
- Check for unhandled promise rejections
- Ensure proper TypeScript types (avoid 'any' type unless necessary)
- Check for potential null/undefined access (use optional chaining)
- Verify proper error handling in async/await blocks
- Check for memory leaks (event listeners, timers not cleaned up)
- Ensure type assertions are safe and necessary`,

	"python": `
LANGUAGE-SPECIFIC CHECKS FOR PYTHON:
- Verify proper exception handling (catch specific exceptions, not bare except)
- Check for resource leaks (use context managers/with statements)
- Ensure mutable default arguments are not used
- Check for proper iterator usage (avoid modifying during iteration)
- Verify None checks before attribute access
- Check for SQL injection vulnerabilities (use parameterized queries)
- Ensure proper use of async/await in async functions`,

	"rust": `
LANGUAGE-SPECIFIC CHECKS FOR RUST:
- Verify proper error handling with Result type
- Check for potential panics (unwrap, expect usage)
- Ensure proper lifetime annotations where needed
- Verify ownership and borrowing rules are followed
- Check for potential race conditions even with Rust's safety
- Ensure proper use of Option type (avoid unwrap on None)
- Verify unsafe blocks are necessary and sound`,

	"java": `
LANGUAGE-SPECIFIC CHECKS FOR JAVA:
- Verify proper exception handling (don't catch and ignore)
- Check for resource leaks (use try-with-resources)
- Ensure proper null checks before dereferencing
- Verify thread safety in concurrent code
- Check for SQL injection vulnerabilities (use PreparedStatement)
- Ensure proper equals() and hashCode() implementation in collections
- Verify proper use of Optional instead of null returns`,

	"c": `
LANGUAGE-SPECIFIC CHECKS FOR C:
- Verify all malloc/calloc have corresponding free
- Check for buffer overflows (array bounds, strcpy vs strncpy)
- Ensure proper null pointer checks before dereferencing
- Verify no use-after-free bugs
- Check for integer overflows in arithmetic
- Ensure proper initialization of variables
- Verify format string vulnerabilities (printf, scanf)`,

	"cpp": `
LANGUAGE-SPECIFIC CHECKS FOR C++:
- Verify RAII principles (use smart pointers, no raw new/delete)
- Check for proper exception safety
- Ensure proper const correctness
- Verify no dangling references or iterators
- Check for proper move semantics usage
- Ensure virtual destructors in base classes
- Verify no memory leaks (use unique_ptr, shared_ptr)`,

	"csharp": `
LANGUAGE-SPECIFIC CHECKS FOR C#:
- Verify proper disposal of IDisposable (use using statements)
- Check for null reference exceptions (use null-conditional operators)
- Ensure async methods properly await tasks
- Verify proper exception handling (specific catch blocks)
- Check for SQL injection vulnerabilities (use parameterized queries)
- Ensure proper thread safety with async code
- Verify LINQ queries are efficient (avoid multiple enumeration)`,

	"php": `
LANGUAGE-SPECIFIC CHECKS FOR PHP:
- Verify SQL injection prevention (use prepared statements)
- Check for XSS vulnerabilities (proper output escaping)
- Ensure proper error handling (try-catch blocks)
- Verify null coalescing and null-safe operators usage
- Check for CSRF protection in forms
- Ensure proper password hashing (password_hash, not md5/sha1)
- Verify file upload security (type, size validation)`,

	"ruby": `
LANGUAGE-SPECIFIC CHECKS FOR RUBY:
- Verify SQL injection prevention (use ActiveRecord parameters)
- Check for XSS vulnerabilities (proper output escaping)
- Ensure proper exception handling (rescue specific errors)
- Verify nil checks before method calls (use safe navigation &.)
- Check for mass assignment vulnerabilities (strong parameters)
- Ensure proper symbol/string usage for memory efficiency
- Verify thread safety in multi-threaded code`,

	"bash": `
LANGUAGE-SPECIFIC CHECKS FOR BASH:
- Verify proper quoting of variables to prevent word splitting
- Check for command injection vulnerabilities
- Ensure proper error handling (set -e, set -u, set -o pipefail)
- Verify proper use of [[ ]] instead of [ ] for tests
- Check for race conditions in file operations
- Ensure proper cleanup in trap handlers
- Verify safe handling of user input`,
}

// buildSystemPrompt creates a language-specific system prompt with optional project context
func buildSystemPrompt(language string, projectContext *ProjectContext) string {
	prompt := baseSystemPrompt

	// Add project context if available
	if projectContext != nil && projectContext.Description != "" {
		if projectContext.IsRaw {
			// Raw file contents from config
			prompt += "\n\n=== PROJECT CONTEXT ===\n" + projectContext.Description
		} else {
			// LLM-generated summary (legacy)
			prompt += "\n\nPROJECT CONTEXT:\n" + projectContext.Description
		}
	}

	// Add language-specific rules
	if rules, ok := languageSpecificRules[language]; ok {
		prompt += "\n" + rules
	}

	return prompt
}

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
// If projectContext is provided, it will be included in the system prompt for better context.
func ReviewFunctions(ctx context.Context, client *Client, functions []extractor.ExtractedFunction, projectContext *ProjectContext) []ReviewResult {
	results := make([]ReviewResult, 0, len(functions))

	for _, fn := range functions {
		result := ReviewResult{Function: fn}

		systemPrompt := buildSystemPrompt(fn.Language, projectContext)
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
