package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

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

// GetChangeInfo returns the metadata of the pull request
func (p *GitHubProvider) GetChangeInfo(ctx context.Context, number int) (ChangeInfo, error) {
	pr, _, err := p.client.PullRequests.Get(ctx, p.owner, p.repo, number)
	if err != nil {
		return ChangeInfo{}, fmt.Errorf("failed to get PR: %w", err)
	}

	labels := make([]string, 0, len(pr.Labels))
	for _, l := range pr.Labels {
		labels = append(labels, l.GetName())
	}

	return ChangeInfo{
		Number:  number,
		Author:  pr.GetUser().GetLogin(),
		Title:   pr.GetTitle(),
		Labels:  labels,
		BaseSHA: pr.GetBase().GetSHA(),
		HeadSHA: pr.GetHead().GetSHA(),
	}, nil
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

	// A directory comes back without a content, and a symbolic link or a submodule added by the
	// PR as an entry without one
	if content == nil || content.GetType() != "file" {
		return "", fmt.Errorf("not a regular file: %s", path)
	}

	text, err := content.GetContent()
	if err != nil {
		return "", fmt.Errorf("failed to decode content: %w", err)
	}

	return text, nil
}

// PostComment posts a comment on the pull request and returns its ID
func (p *GitHubProvider) PostComment(ctx context.Context, number int, body string) (string, error) {
	comment := &github.IssueComment{
		Body: github.String(body),
	}

	created, _, err := p.client.Issues.CreateComment(ctx, p.owner, p.repo, number, comment)
	if err != nil {
		return "", fmt.Errorf("failed to post comment: %w", err)
	}

	return strconv.FormatInt(created.GetID(), 10), nil
}

// UpdateComment replaces the body of a general pull request comment
func (p *GitHubProvider) UpdateComment(ctx context.Context, number int, id, body string) error {
	commentID, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid comment ID %q: %w", id, err)
	}

	_, _, err = p.client.Issues.EditComment(ctx, p.owner, p.repo, commentID, &github.IssueComment{Body: github.String(body)})
	if err != nil {
		return fmt.Errorf("failed to update comment: %w", err)
	}

	return nil
}

// CreateReview creates a review with inline comments on specific lines.
// GitHub accepts or rejects a whole review: when it is rejected, each comment is retried
// in a review of its own so that one invalid line does not hide the others.
func (p *GitHubProvider) CreateReview(ctx context.Context, number int, comments []ReviewComment) ([]CommentResult, error) {
	if len(comments) == 0 {
		return nil, nil
	}

	err := p.createReview(ctx, number, comments)
	if err == nil {
		return resultsFor(comments, nil), nil
	}
	if len(comments) == 1 {
		return resultsFor(comments, err), nil
	}

	results := make([]CommentResult, 0, len(comments))
	for _, c := range comments {
		results = append(results, CommentResult{Comment: c, Err: p.createReview(ctx, number, []ReviewComment{c})})
	}
	return results, nil
}

func resultsFor(comments []ReviewComment, err error) []CommentResult {
	results := make([]CommentResult, len(comments))
	for i, c := range comments {
		results[i] = CommentResult{Comment: c, Err: err}
	}
	return results
}

func (p *GitHubProvider) createReview(ctx context.Context, number int, comments []ReviewComment) error {
	ghComments := make([]*github.DraftReviewComment, 0, len(comments))
	for _, c := range comments {
		body := c.Body
		if c.Suggestion != "" {
			body = c.Body + "\n\n" + Fence("suggestion", c.Suggestion)
		}

		draft := &github.DraftReviewComment{
			Path: github.String(c.Path),
			Line: github.Int(c.Line),
			Body: github.String(body),
		}
		if c.replacesRange() {
			draft.StartLine = github.Int(c.Line)
			draft.Line = github.Int(c.EndLine)
		}
		ghComments = append(ghComments, draft)
	}

	review := &github.PullRequestReviewRequest{
		CommitID: github.String(p.headSHA),
		Event:    github.String("COMMENT"),
		Comments: ghComments,
	}

	if _, _, err := p.client.PullRequests.CreateReview(ctx, p.owner, p.repo, number, review); err != nil {
		return fmt.Errorf("failed to create review: %w", err)
	}
	return nil
}

// actionsBot is the identity of GITHUB_TOKEN, which cannot call the /user endpoint
const actionsBot = "github-actions[bot]"

// CurrentUser returns the login the token acts as
func (p *GitHubProvider) CurrentUser(ctx context.Context) (string, error) {
	user, resp, err := p.client.Users.Get(ctx, "")
	if err != nil {
		if resp != nil && (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound) {
			return actionsBot, nil
		}
		return "", fmt.Errorf("failed to get current user: %w", err)
	}
	return user.GetLogin(), nil
}

// ListComments returns the review comments and the general comments of the pull request
func (p *GitHubProvider) ListComments(ctx context.Context, number int) ([]ExistingComment, error) {
	var result []ExistingComment

	reviewOpts := &github.PullRequestListCommentsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}
	for {
		comments, resp, err := p.client.PullRequests.ListComments(ctx, p.owner, p.repo, number, reviewOpts)
		if err != nil {
			return nil, fmt.Errorf("failed to list PR comments: %w", err)
		}
		for _, c := range comments {
			result = append(result, ExistingComment{
				ID:         strconv.FormatInt(c.GetID(), 10),
				Author:     c.GetUser().GetLogin(),
				Path:       c.GetPath(),
				Line:       c.GetLine(),
				Body:       c.GetBody(),
				Positioned: true,
			})
		}

		if resp.NextPage == 0 {
			break
		}
		reviewOpts.Page = resp.NextPage
	}

	issueOpts := &github.IssueListCommentsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}
	for {
		comments, resp, err := p.client.Issues.ListComments(ctx, p.owner, p.repo, number, issueOpts)
		if err != nil {
			return nil, fmt.Errorf("failed to list issue comments: %w", err)
		}
		for _, c := range comments {
			result = append(result, ExistingComment{
				ID:     strconv.FormatInt(c.GetID(), 10),
				Author: c.GetUser().GetLogin(),
				Body:   c.GetBody(),
			})
		}

		if resp.NextPage == 0 {
			break
		}
		issueOpts.Page = resp.NextPage
	}

	return result, nil
}

// ListReactions tallies the +1 and -1 reactions of review comments
func (p *GitHubProvider) ListReactions(ctx context.Context, number int, ids []string) (map[string]ReactionCount, error) {
	result := make(map[string]ReactionCount, len(ids))
	var errs []error
	for _, id := range ids {
		commentID, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid comment ID %q", id))
			continue
		}

		var count ReactionCount
		opts := &github.ListOptions{PerPage: 100}
		for {
			reactions, resp, err := p.client.Reactions.ListPullRequestCommentReactions(ctx, p.owner, p.repo, commentID, opts)
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to list the reactions of comment %s: %w", id, err))
				count = ReactionCount{}
				break
			}
			for _, r := range reactions {
				switch r.GetContent() {
				case "+1":
					count.Up++
				case "-1":
					count.Down++
				}
			}
			if resp.NextPage == 0 {
				result[id] = count
				break
			}
			opts.Page = resp.NextPage
		}
	}
	return result, errors.Join(errs...)
}
