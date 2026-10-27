package provider

import (
	"context"
	"fmt"
	"strconv"

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
	return newGitLabProvider(fmt.Sprintf("https://%s/api/v4", host), fmt.Sprintf("%s/%s", owner, repo), token)
}

func newGitLabProvider(baseURL, projectID, token string) *GitLabProvider {
	client, err := gitlab.NewClient(token, gitlab.WithBaseURL(baseURL))
	if err != nil {
		// Return a provider that will fail on use
		return &GitLabProvider{projectID: projectID}
	}

	return &GitLabProvider{
		client:    client,
		projectID: projectID,
	}
}

// GetChangeInfo returns the metadata of the merge request
func (p *GitLabProvider) GetChangeInfo(ctx context.Context, number int) (ChangeInfo, error) {
	if p.client == nil {
		return ChangeInfo{}, fmt.Errorf("GitLab client not initialized")
	}

	mr, _, err := p.client.MergeRequests.GetMergeRequest(p.projectID, int64(number), nil, gitlab.WithContext(ctx))
	if err != nil {
		return ChangeInfo{}, fmt.Errorf("failed to get MR: %w", err)
	}

	info := ChangeInfo{
		Number:   number,
		Title:    mr.Title,
		Labels:   []string(mr.Labels),
		BaseSHA:  mr.DiffRefs.BaseSha,
		StartSHA: mr.DiffRefs.StartSha,
		HeadSHA:  mr.DiffRefs.HeadSha,
	}
	if mr.Author != nil {
		info.Author = mr.Author.Username
	}
	return info, nil
}

// GetModifiedFiles returns all files modified in a merge request
func (p *GitLabProvider) GetModifiedFiles(ctx context.Context, number int) ([]ModifiedFile, error) {
	if p.client == nil {
		return nil, fmt.Errorf("GitLab client not initialized")
	}

	mrNumber := int64(number)

	// Get MR details to get the head SHA
	mr, _, err := p.client.MergeRequests.GetMergeRequest(p.projectID, mrNumber, nil, gitlab.WithContext(ctx))
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
		diffs, resp, err := p.client.MergeRequests.ListMergeRequestDiffs(p.projectID, mrNumber, opts, gitlab.WithContext(ctx))
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

	content, _, err := p.client.RepositoryFiles.GetRawFile(p.projectID, path, opts, gitlab.WithContext(ctx))
	if err != nil {
		return "", fmt.Errorf("failed to get file content: %w", err)
	}

	return string(content), nil
}

// PostComment posts a comment on the merge request and returns the note ID
func (p *GitLabProvider) PostComment(ctx context.Context, number int, body string) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("GitLab client not initialized")
	}

	opts := &gitlab.CreateMergeRequestNoteOptions{
		Body: gitlab.Ptr(body),
	}

	note, _, err := p.client.Notes.CreateMergeRequestNote(p.projectID, int64(number), opts, gitlab.WithContext(ctx))
	if err != nil {
		return "", fmt.Errorf("failed to post comment: %w", err)
	}

	return strconv.FormatInt(note.ID, 10), nil
}

// UpdateComment replaces the body of a merge request note
func (p *GitLabProvider) UpdateComment(ctx context.Context, number int, id, body string) error {
	if p.client == nil {
		return fmt.Errorf("GitLab client not initialized")
	}

	noteID, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid note ID %q: %w", id, err)
	}

	opts := &gitlab.UpdateMergeRequestNoteOptions{Body: gitlab.Ptr(body)}
	if _, _, err := p.client.Notes.UpdateMergeRequestNote(p.projectID, int64(number), noteID, opts, gitlab.WithContext(ctx)); err != nil {
		return fmt.Errorf("failed to update comment: %w", err)
	}

	return nil
}

// CreateReview creates a positioned discussion for each comment of the merge request
func (p *GitLabProvider) CreateReview(ctx context.Context, number int, comments []ReviewComment) ([]CommentResult, error) {
	if p.client == nil {
		return nil, fmt.Errorf("GitLab client not initialized")
	}

	if len(comments) == 0 {
		return nil, nil
	}

	info, err := p.GetChangeInfo(ctx, number)
	if err != nil {
		return nil, err
	}

	results := make([]CommentResult, 0, len(comments))
	for _, c := range comments {
		body := c.Body
		if c.Suggestion != "" {
			body = c.Body + "\n\n" + Fence("suggestion:-0+0", c.Suggestion)
		}

		line := int64(c.Line)
		opts := &gitlab.CreateMergeRequestDiscussionOptions{
			Body: gitlab.Ptr(body),
			Position: &gitlab.PositionOptions{
				BaseSHA:      gitlab.Ptr(info.BaseSHA),
				StartSHA:     gitlab.Ptr(info.StartSHA),
				HeadSHA:      gitlab.Ptr(info.HeadSHA),
				PositionType: gitlab.Ptr("text"),
				NewPath:      gitlab.Ptr(c.Path),
				NewLine:      &line,
			},
		}

		_, _, err := p.client.Discussions.CreateMergeRequestDiscussion(p.projectID, int64(number), opts, gitlab.WithContext(ctx))
		results = append(results, CommentResult{Comment: c, Err: err})
	}

	return results, nil
}

// CurrentUser returns the username of the token owner
func (p *GitLabProvider) CurrentUser(ctx context.Context) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("GitLab client not initialized")
	}

	user, _, err := p.client.Users.CurrentUser(gitlab.WithContext(ctx))
	if err != nil {
		return "", fmt.Errorf("failed to get current user: %w", err)
	}

	return user.Username, nil
}

// ListComments returns the notes of all discussions of the merge request, system notes excluded
func (p *GitLabProvider) ListComments(ctx context.Context, number int) ([]ExistingComment, error) {
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
		discussions, resp, err := p.client.Discussions.ListMergeRequestDiscussions(p.projectID, mrNumber, opts, gitlab.WithContext(ctx))
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
			if note.System {
				continue
			}

			c := ExistingComment{ID: strconv.FormatInt(note.ID, 10), Author: note.Author.Username, Body: note.Body}
			if note.Position != nil {
				c.Path = note.Position.NewPath
				c.Line = int(note.Position.NewLine)
				c.Positioned = true
			}
			result = append(result, c)
		}
	}

	return result, nil
}
