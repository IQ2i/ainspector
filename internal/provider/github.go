package provider

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/google/go-github/v57/github"
	"golang.org/x/oauth2"
)

// GitHubProvider implements Provider for GitHub
type GitHubProvider struct {
	client  *github.Client
	owner   string
	repo    string
	headSHA string
}

// NewGitHubProvider creates a new GitHub provider
func NewGitHubProvider(owner, repo, token string) *GitHubProvider {
	var client *github.Client

	if token != "" {
		ts := oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: token},
		)
		tc := oauth2.NewClient(context.Background(), ts)
		client = github.NewClient(tc)
	} else {
		client = github.NewClient(nil)
	}

	return &GitHubProvider{
		client: client,
		owner:  owner,
		repo:   repo,
	}
}

// GetModifiedFiles returns all files modified in a pull request
func (p *GitHubProvider) GetModifiedFiles(ctx context.Context, number int) ([]ModifiedFile, error) {
	// Get PR details to get the head SHA
	pr, _, err := p.client.PullRequests.Get(ctx, p.owner, p.repo, number)
	if err != nil {
		return nil, fmt.Errorf("failed to get PR: %w", err)
	}
	p.headSHA = pr.GetHead().GetSHA()

	// Get all files in the PR with pagination
	var allFiles []*github.CommitFile
	opts := &github.ListOptions{PerPage: 100}

	for {
		files, resp, err := p.client.PullRequests.ListFiles(ctx, p.owner, p.repo, number, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list PR files: %w", err)
		}
		allFiles = append(allFiles, files...)

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	// Convert to ModifiedFile
	result := make([]ModifiedFile, 0, len(allFiles))
	for _, f := range allFiles {
		mf := ModifiedFile{
			Path:    f.GetFilename(),
			OldPath: f.GetPreviousFilename(),
			Status:  f.GetStatus(),
			Patch:   f.GetPatch(),
		}
		result = append(result, mf)
	}

	return result, nil
}

// GetFileContent returns the content of a file at the PR head
func (p *GitHubProvider) GetFileContent(ctx context.Context, path string) (string, error) {
	opts := &github.RepositoryContentGetOptions{
		Ref: p.headSHA,
	}

	content, _, _, err := p.client.Repositories.GetContents(ctx, p.owner, p.repo, path, opts)
	if err != nil {
		return "", fmt.Errorf("failed to get file content: %w", err)
	}

	if content == nil {
		return "", fmt.Errorf("file not found: %s", path)
	}

	// Content is base64 encoded
	decoded, err := base64.StdEncoding.DecodeString(*content.Content)
	if err != nil {
		return "", fmt.Errorf("failed to decode content: %w", err)
	}

	return string(decoded), nil
}

// PostComment posts a comment on the pull request
func (p *GitHubProvider) PostComment(ctx context.Context, number int, body string) error {
	comment := &github.IssueComment{
		Body: github.String(body),
	}

	_, _, err := p.client.Issues.CreateComment(ctx, p.owner, p.repo, number, comment)
	if err != nil {
		return fmt.Errorf("failed to post comment: %w", err)
	}

	return nil
}

// CreateReview creates a review with inline comments on specific lines
func (p *GitHubProvider) CreateReview(ctx context.Context, number int, comments []ReviewComment) error {
	if len(comments) == 0 {
		return nil
	}

	// Convert ReviewComment to GitHub DraftReviewComment
	ghComments := make([]*github.DraftReviewComment, 0, len(comments))
	for _, c := range comments {
		body := c.Body
		// Add suggestion block if there's a suggested code change
		if c.Suggestion != "" {
			body = fmt.Sprintf("%s\n\n```suggestion\n%s\n```", c.Body, c.Suggestion)
		}

		ghComment := &github.DraftReviewComment{
			Path: github.String(c.Path),
			Line: github.Int(c.Line),
			Body: github.String(body),
		}
		ghComments = append(ghComments, ghComment)
	}

	review := &github.PullRequestReviewRequest{
		CommitID: github.String(p.headSHA),
		Event:    github.String("COMMENT"),
		Comments: ghComments,
	}

	_, _, err := p.client.PullRequests.CreateReview(ctx, p.owner, p.repo, number, review)
	if err != nil {
		return fmt.Errorf("failed to create review: %w", err)
	}

	return nil
}
