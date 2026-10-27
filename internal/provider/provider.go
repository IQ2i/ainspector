package provider

import (
	"context"
	"strings"
)

// ModifiedFile represents a file that was modified in a PR/MR
type ModifiedFile struct {
	Path       string // Current file path
	OldPath    string // Previous path (for renames)
	Status     string // added, modified, deleted, renamed
	Patch      string // Unified diff patch
	RawContent string // Full file content (populated lazily)
}

// ReviewComment represents an inline review comment with an optional code suggestion
type ReviewComment struct {
	Path       string // File path
	Line       int    // Line number where the comment applies
	Body       string // Comment body (may include suggestion block)
	Suggestion string // Optional: suggested code replacement
	// EndLine is the last line the suggestion replaces. Zero or Line means the line of the
	// comment alone.
	EndLine int
}

// replacesRange tells whether the suggestion of a comment replaces several lines
func (c ReviewComment) replacesRange() bool {
	return c.Suggestion != "" && c.EndLine > c.Line
}

// ChangeInfo describes the PR/MR being reviewed
type ChangeInfo struct {
	Number   int
	Author   string
	Title    string
	Labels   []string
	BaseSHA  string
	StartSHA string
	HeadSHA  string
}

// CommentResult is the outcome of posting one review comment
type CommentResult struct {
	Comment ReviewComment
	Err     error
}

// ExistingComment represents a comment already posted on the PR/MR
type ExistingComment struct {
	ID         string
	Author     string // Login of the author, empty when unknown
	Path       string // File path, empty for general comments
	Line       int    // Line number, 0 for general comments
	Body       string
	Positioned bool // true for inline comments attached to a line
}

// Provider is the interface for git hosting providers (GitHub, GitLab)
type Provider interface {
	// GetChangeInfo returns the metadata of the PR/MR
	GetChangeInfo(ctx context.Context, number int) (ChangeInfo, error)

	// GetModifiedFiles returns all files modified in a PR/MR
	GetModifiedFiles(ctx context.Context, number int) ([]ModifiedFile, error)

	// GetFileContent returns the content of a file at the PR/MR head
	GetFileContent(ctx context.Context, path string) (string, error)

	// PostComment posts a general comment on the PR/MR and returns its ID
	PostComment(ctx context.Context, number int, body string) (string, error)

	// UpdateComment replaces the body of a general comment
	UpdateComment(ctx context.Context, number int, id, body string) error

	// CreateReview posts inline comments and reports the result of each one.
	// A comment that cannot be posted never prevents the others from being posted.
	CreateReview(ctx context.Context, number int, comments []ReviewComment) ([]CommentResult, error)

	// CurrentUser returns the login the token acts as
	CurrentUser(ctx context.Context) (string, error)

	// ListComments returns inline and general comments of the PR/MR
	ListComments(ctx context.Context, number int) ([]ExistingComment, error)
}

// Fence wraps code in a Markdown fence longer than any run of backticks it contains,
// so that LLM output cannot close the block early.
func Fence(info, code string) string {
	longest, run := 0, 0
	for _, r := range code {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", max(3, longest+1))
	return fence + info + "\n" + code + "\n" + fence
}
