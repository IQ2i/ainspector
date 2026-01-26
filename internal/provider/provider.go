package provider

import "context"

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
}

// ExistingComment represents a review comment already posted on the PR/MR
type ExistingComment struct {
	Path string // File path
	Line int    // Line number
	Body string // Comment body
}

// Provider is the interface for git hosting providers (GitHub, GitLab)
type Provider interface {
	// GetModifiedFiles returns all files modified in a PR/MR
	GetModifiedFiles(ctx context.Context, number int) ([]ModifiedFile, error)

	// GetFileContent returns the content of a file at the PR/MR head
	GetFileContent(ctx context.Context, path string) (string, error)

	// PostComment posts a general comment on the PR/MR
	PostComment(ctx context.Context, number int, body string) error

	// CreateReview creates a review with inline comments on specific lines
	CreateReview(ctx context.Context, number int, comments []ReviewComment) error

	// GetReviewComments returns all review comments on the PR/MR
	GetReviewComments(ctx context.Context, number int) ([]ExistingComment, error)
}
