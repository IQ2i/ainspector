package provider

import (
	"testing"
)

func TestModifiedFile_Fields(t *testing.T) {
	mf := ModifiedFile{
		Path:       "src/main.go",
		OldPath:    "src/old_main.go",
		Status:     "renamed",
		Patch:      "@@ -1,3 +1,4 @@",
		RawContent: "package main",
	}

	if mf.Path != "src/main.go" {
		t.Errorf("expected path 'src/main.go', got %s", mf.Path)
	}
	if mf.OldPath != "src/old_main.go" {
		t.Errorf("expected old path 'src/old_main.go', got %s", mf.OldPath)
	}
	if mf.Status != "renamed" {
		t.Errorf("expected status 'renamed', got %s", mf.Status)
	}
	if mf.Patch != "@@ -1,3 +1,4 @@" {
		t.Errorf("expected patch '@@ -1,3 +1,4 @@', got %s", mf.Patch)
	}
	if mf.RawContent != "package main" {
		t.Errorf("expected raw content 'package main', got %s", mf.RawContent)
	}
}

func TestModifiedFile_StatusValues(t *testing.T) {
	statuses := []string{"added", "modified", "deleted", "renamed"}

	for _, status := range statuses {
		mf := ModifiedFile{Status: status}
		if mf.Status != status {
			t.Errorf("expected status %s, got %s", status, mf.Status)
		}
	}
}

func TestNewGitHubProvider(t *testing.T) {
	p := NewGitHubProvider("owner", "repo", "token")

	if p == nil {
		t.Fatal("expected non-nil provider")
	}
	if p.owner != "owner" {
		t.Errorf("expected owner 'owner', got %s", p.owner)
	}
	if p.repo != "repo" {
		t.Errorf("expected repo 'repo', got %s", p.repo)
	}
	if p.client == nil {
		t.Error("expected client to be initialized")
	}
}

func TestNewGitHubProvider_NoToken(t *testing.T) {
	p := NewGitHubProvider("owner", "repo", "")

	if p == nil {
		t.Fatal("expected non-nil provider")
	}
	if p.client == nil {
		t.Error("expected client to be initialized even without token")
	}
}

func TestNewGitLabProvider(t *testing.T) {
	p := NewGitLabProvider("gitlab.com", "owner", "repo", "token")

	if p == nil {
		t.Fatal("expected non-nil provider")
	}
	if p.projectID != "owner/repo" {
		t.Errorf("expected projectID 'owner/repo', got %s", p.projectID)
	}
	if p.client == nil {
		t.Error("expected client to be initialized")
	}
}

func TestNewGitLabProvider_NoToken(t *testing.T) {
	p := NewGitLabProvider("gitlab.com", "owner", "repo", "")

	if p == nil {
		t.Fatal("expected non-nil provider")
	}
	if p.projectID != "owner/repo" {
		t.Errorf("expected projectID 'owner/repo', got %s", p.projectID)
	}
}

func TestNewGitLabProvider_SelfHosted(t *testing.T) {
	p := NewGitLabProvider("gitlab.example.com", "group", "project", "token")

	if p == nil {
		t.Fatal("expected non-nil provider")
	}
	if p.projectID != "group/project" {
		t.Errorf("expected projectID 'group/project', got %s", p.projectID)
	}
}

func TestGitHubProvider_ImplementsInterface(t *testing.T) {
	var _ Provider = (*GitHubProvider)(nil)
}

func TestGitLabProvider_ImplementsInterface(t *testing.T) {
	var _ Provider = (*GitLabProvider)(nil)
}
