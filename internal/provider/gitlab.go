package provider

import (
	"context"
	"fmt"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

// GitLabProvider implements Provider for GitLab
type GitLabProvider struct {
	client    *gitlab.Client
	projectID string
	headSHA   string
}

// NewGitLabProvider creates a new GitLab provider
func NewGitLabProvider(host, owner, repo, token string) *GitLabProvider {
	var client *gitlab.Client
	var err error

	baseURL := fmt.Sprintf("https://%s/api/v4", host)
	projectID := fmt.Sprintf("%s/%s", owner, repo)

	if token != "" {
		client, err = gitlab.NewClient(token, gitlab.WithBaseURL(baseURL))
	} else {
		client, err = gitlab.NewClient("", gitlab.WithBaseURL(baseURL))
	}

	if err != nil {
		// Return a provider that will fail on use
		return &GitLabProvider{projectID: projectID}
	}

	return &GitLabProvider{
		client:    client,
		projectID: projectID,
	}
}

// GetModifiedFiles returns all files modified in a merge request
func (p *GitLabProvider) GetModifiedFiles(ctx context.Context, number int) ([]ModifiedFile, error) {
	if p.client == nil {
		return nil, fmt.Errorf("GitLab client not initialized")
	}

	mrNumber := int64(number)

	// Get MR details to get the head SHA
	mr, _, err := p.client.MergeRequests.GetMergeRequest(p.projectID, mrNumber, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get MR: %w", err)
	}
	p.headSHA = mr.SHA

	// Get MR diffs with pagination
	var allDiffs []*gitlab.MergeRequestDiff
	opts := &gitlab.ListMergeRequestDiffsOptions{
		ListOptions: gitlab.ListOptions{
			PerPage: 100,
			Page:    1,
		},
	}

	for {
		diffs, resp, err := p.client.MergeRequests.ListMergeRequestDiffs(p.projectID, mrNumber, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to get MR diffs: %w", err)
		}
		allDiffs = append(allDiffs, diffs...)

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	// Convert to ModifiedFile
	result := make([]ModifiedFile, 0, len(allDiffs))
	for _, diff := range allDiffs {
		status := "modified"
		if diff.NewFile {
			status = "added"
		} else if diff.DeletedFile {
			status = "deleted"
		} else if diff.RenamedFile {
			status = "renamed"
		}

		mf := ModifiedFile{
			Path:    diff.NewPath,
			OldPath: diff.OldPath,
			Status:  status,
			Patch:   diff.Diff,
		}
		result = append(result, mf)
	}

	return result, nil
}

// GetFileContent returns the content of a file at the MR head
func (p *GitLabProvider) GetFileContent(ctx context.Context, path string) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("GitLab client not initialized")
	}

	opts := &gitlab.GetRawFileOptions{
		Ref: gitlab.Ptr(p.headSHA),
	}

	content, _, err := p.client.RepositoryFiles.GetRawFile(p.projectID, path, opts)
	if err != nil {
		return "", fmt.Errorf("failed to get file content: %w", err)
	}

	return string(content), nil
}

// PostComment posts a comment on the merge request
func (p *GitLabProvider) PostComment(ctx context.Context, number int, body string) error {
	if p.client == nil {
		return fmt.Errorf("GitLab client not initialized")
	}

	mrNumber := int64(number)
	opts := &gitlab.CreateMergeRequestNoteOptions{
		Body: gitlab.Ptr(body),
	}

	_, _, err := p.client.Notes.CreateMergeRequestNote(p.projectID, mrNumber, opts)
	if err != nil {
		return fmt.Errorf("failed to post comment: %w", err)
	}

	return nil
}

// CreateReview creates inline comments on specific lines of the merge request
func (p *GitLabProvider) CreateReview(ctx context.Context, number int, comments []ReviewComment) error {
	if p.client == nil {
		return fmt.Errorf("GitLab client not initialized")
	}

	if len(comments) == 0 {
		return nil
	}

	mrNumber := int64(number)

	// Get the MR to retrieve base and head SHAs
	mr, _, err := p.client.MergeRequests.GetMergeRequest(p.projectID, mrNumber, nil)
	if err != nil {
		return fmt.Errorf("failed to get MR: %w", err)
	}

	// Create a discussion for each comment
	for _, c := range comments {
		body := c.Body
		// Add suggestion block if there's a suggested code change
		if c.Suggestion != "" {
			body = fmt.Sprintf("%s\n\n```suggestion:-0+0\n%s\n```", c.Body, c.Suggestion)
		}

		line := int64(c.Line)
		position := &gitlab.PositionOptions{
			BaseSHA:      gitlab.Ptr(mr.DiffRefs.BaseSha),
			StartSHA:     gitlab.Ptr(mr.DiffRefs.StartSha),
			HeadSHA:      gitlab.Ptr(mr.DiffRefs.HeadSha),
			PositionType: gitlab.Ptr("text"),
			NewPath:      gitlab.Ptr(c.Path),
			NewLine:      &line,
		}

		opts := &gitlab.CreateMergeRequestDiscussionOptions{
			Body:     gitlab.Ptr(body),
			Position: position,
		}

		_, _, err := p.client.Discussions.CreateMergeRequestDiscussion(p.projectID, mrNumber, opts)
		if err != nil {
			// Log error but continue with other comments
			fmt.Printf("Warning: failed to create discussion for %s:%d: %v\n", c.Path, c.Line, err)
		}
	}

	return nil
}

// GetReviewComments returns all review comments on the merge request
func (p *GitLabProvider) GetReviewComments(ctx context.Context, number int) ([]ExistingComment, error) {
	if p.client == nil {
		return nil, fmt.Errorf("GitLab client not initialized")
	}

	mrNumber := int64(number)
	var allDiscussions []*gitlab.Discussion
	opts := &gitlab.ListMergeRequestDiscussionsOptions{
		ListOptions: gitlab.ListOptions{
			PerPage: 100,
			Page:    1,
		},
	}

	for {
		discussions, resp, err := p.client.Discussions.ListMergeRequestDiscussions(p.projectID, mrNumber, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list MR discussions: %w", err)
		}
		allDiscussions = append(allDiscussions, discussions...)

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	var result []ExistingComment
	for _, d := range allDiscussions {
		for _, note := range d.Notes {
			// Only include notes with position (inline comments)
			if note.Position != nil {
				result = append(result, ExistingComment{
					Path: note.Position.NewPath,
					Line: int(note.Position.NewLine),
					Body: note.Body,
				})
			}
		}
	}

	return result, nil
}
