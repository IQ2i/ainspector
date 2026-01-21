package ci

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Helper to set environment variables and clean them up after test
func setEnv(t *testing.T, envs map[string]string) func() {
	t.Helper()
	originalVals := make(map[string]string)
	originalSet := make(map[string]bool)

	for key, value := range envs {
		originalVals[key], originalSet[key] = os.LookupEnv(key)
		_ = os.Setenv(key, value)
	}

	return func() {
		for key := range envs {
			if originalSet[key] {
				_ = os.Setenv(key, originalVals[key])
			} else {
				_ = os.Unsetenv(key)
			}
		}
	}
}

// clearCIEnvVars clears all CI-related environment variables
func clearCIEnvVars(t *testing.T) func() {
	t.Helper()
	envVars := []string{
		"GITHUB_ACTIONS", "GITHUB_REPOSITORY", "GITHUB_REF", "GITHUB_TOKEN", "GITHUB_EVENT_PATH",
		"GITLAB_CI", "CI_PROJECT_PATH", "CI_MERGE_REQUEST_IID", "GITLAB_TOKEN", "CI_JOB_TOKEN", "CI_SERVER_HOST",
	}

	originalVals := make(map[string]string)
	originalSet := make(map[string]bool)

	for _, key := range envVars {
		originalVals[key], originalSet[key] = os.LookupEnv(key)
		_ = os.Unsetenv(key)
	}

	return func() {
		for _, key := range envVars {
			if originalSet[key] {
				_ = os.Setenv(key, originalVals[key])
			} else {
				_ = os.Unsetenv(key)
			}
		}
	}
}

func TestDetect_GitHub(t *testing.T) {
	cleanup := clearCIEnvVars(t)
	defer cleanup()

	envCleanup := setEnv(t, map[string]string{
		"GITHUB_ACTIONS":    "true",
		"GITHUB_REPOSITORY": "owner/repo",
		"GITHUB_REF":        "refs/pull/123/merge",
		"GITHUB_TOKEN":      "test-token",
	})
	defer envCleanup()

	env, err := Detect()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if env.Provider != "github" {
		t.Errorf("expected provider 'github', got %s", env.Provider)
	}
	if env.Owner != "owner" {
		t.Errorf("expected owner 'owner', got %s", env.Owner)
	}
	if env.Repo != "repo" {
		t.Errorf("expected repo 'repo', got %s", env.Repo)
	}
	if env.PRNumber != 123 {
		t.Errorf("expected PR number 123, got %d", env.PRNumber)
	}
	if env.Token != "test-token" {
		t.Errorf("expected token 'test-token', got %s", env.Token)
	}
	if env.ServerHost != "github.com" {
		t.Errorf("expected server host 'github.com', got %s", env.ServerHost)
	}
}

func TestDetect_GitLab(t *testing.T) {
	cleanup := clearCIEnvVars(t)
	defer cleanup()

	envCleanup := setEnv(t, map[string]string{
		"GITLAB_CI":            "true",
		"CI_PROJECT_PATH":      "group/project",
		"CI_MERGE_REQUEST_IID": "456",
		"GITLAB_TOKEN":         "gitlab-token",
		"CI_SERVER_HOST":       "gitlab.example.com",
	})
	defer envCleanup()

	env, err := Detect()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if env.Provider != "gitlab" {
		t.Errorf("expected provider 'gitlab', got %s", env.Provider)
	}
	if env.Owner != "group" {
		t.Errorf("expected owner 'group', got %s", env.Owner)
	}
	if env.Repo != "project" {
		t.Errorf("expected repo 'project', got %s", env.Repo)
	}
	if env.PRNumber != 456 {
		t.Errorf("expected PR number 456, got %d", env.PRNumber)
	}
	if env.Token != "gitlab-token" {
		t.Errorf("expected token 'gitlab-token', got %s", env.Token)
	}
	if env.ServerHost != "gitlab.example.com" {
		t.Errorf("expected server host 'gitlab.example.com', got %s", env.ServerHost)
	}
}

func TestDetect_NoCIEnvironment(t *testing.T) {
	cleanup := clearCIEnvVars(t)
	defer cleanup()

	_, err := Detect()
	if err == nil {
		t.Fatal("expected error when no CI environment is detected")
	}
	if err.Error() != "not running in a supported CI environment (GitHub Actions or GitLab CI)" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestDetect_GitHubPrecedence(t *testing.T) {
	cleanup := clearCIEnvVars(t)
	defer cleanup()

	// Set both GitHub and GitLab env vars
	envCleanup := setEnv(t, map[string]string{
		"GITHUB_ACTIONS":       "true",
		"GITHUB_REPOSITORY":    "gh-owner/gh-repo",
		"GITHUB_REF":           "refs/pull/1/merge",
		"GITHUB_TOKEN":         "gh-token",
		"GITLAB_CI":            "true",
		"CI_PROJECT_PATH":      "gl-owner/gl-repo",
		"CI_MERGE_REQUEST_IID": "2",
		"GITLAB_TOKEN":         "gl-token",
	})
	defer envCleanup()

	env, err := Detect()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// GitHub should take precedence
	if env.Provider != "github" {
		t.Errorf("expected GitHub to take precedence, got %s", env.Provider)
	}
}

func TestDetectGitHub_MissingRepository(t *testing.T) {
	cleanup := clearCIEnvVars(t)
	defer cleanup()

	envCleanup := setEnv(t, map[string]string{
		"GITHUB_ACTIONS": "true",
		"GITHUB_REF":     "refs/pull/123/merge",
		"GITHUB_TOKEN":   "token",
	})
	defer envCleanup()

	_, err := Detect()
	if err == nil {
		t.Fatal("expected error for missing GITHUB_REPOSITORY")
	}
}

func TestDetectGitHub_InvalidRepositoryFormat(t *testing.T) {
	cleanup := clearCIEnvVars(t)
	defer cleanup()

	envCleanup := setEnv(t, map[string]string{
		"GITHUB_ACTIONS":    "true",
		"GITHUB_REPOSITORY": "invalid-format",
		"GITHUB_REF":        "refs/pull/123/merge",
		"GITHUB_TOKEN":      "token",
	})
	defer envCleanup()

	_, err := Detect()
	if err == nil {
		t.Fatal("expected error for invalid repository format")
	}
}

func TestDetectGitHub_MissingToken(t *testing.T) {
	cleanup := clearCIEnvVars(t)
	defer cleanup()

	envCleanup := setEnv(t, map[string]string{
		"GITHUB_ACTIONS":    "true",
		"GITHUB_REPOSITORY": "owner/repo",
		"GITHUB_REF":        "refs/pull/123/merge",
	})
	defer envCleanup()

	_, err := Detect()
	if err == nil {
		t.Fatal("expected error for missing GITHUB_TOKEN")
	}
}

func TestDetectGitHub_PRNumberFromRef(t *testing.T) {
	cleanup := clearCIEnvVars(t)
	defer cleanup()

	tests := []struct {
		ref      string
		expected int
	}{
		{"refs/pull/1/merge", 1},
		{"refs/pull/123/merge", 123},
		{"refs/pull/99999/merge", 99999},
	}

	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			envCleanup := setEnv(t, map[string]string{
				"GITHUB_ACTIONS":    "true",
				"GITHUB_REPOSITORY": "owner/repo",
				"GITHUB_REF":        tt.ref,
				"GITHUB_TOKEN":      "token",
			})
			defer envCleanup()

			env, err := Detect()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if env.PRNumber != tt.expected {
				t.Errorf("expected PR number %d, got %d", tt.expected, env.PRNumber)
			}
		})
	}
}

func TestDetectGitHub_PRNumberFromEventFile(t *testing.T) {
	cleanup := clearCIEnvVars(t)
	defer cleanup()

	// Create a temporary event file
	tmpDir := t.TempDir()
	eventFile := filepath.Join(tmpDir, "event.json")

	eventData := map[string]interface{}{
		"pull_request": map[string]interface{}{
			"number": 789,
		},
	}
	data, _ := json.Marshal(eventData)
	_ = os.WriteFile(eventFile, data, 0644)

	envCleanup := setEnv(t, map[string]string{
		"GITHUB_ACTIONS":    "true",
		"GITHUB_REPOSITORY": "owner/repo",
		"GITHUB_REF":        "refs/heads/main", // Not a PR ref
		"GITHUB_EVENT_PATH": eventFile,
		"GITHUB_TOKEN":      "token",
	})
	defer envCleanup()

	env, err := Detect()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.PRNumber != 789 {
		t.Errorf("expected PR number 789 from event file, got %d", env.PRNumber)
	}
}

func TestDetectGitHub_PRNumberFromEventFileTopLevel(t *testing.T) {
	cleanup := clearCIEnvVars(t)
	defer cleanup()

	// Create a temporary event file with top-level number
	tmpDir := t.TempDir()
	eventFile := filepath.Join(tmpDir, "event.json")

	eventData := map[string]interface{}{
		"number": 555,
	}
	data, _ := json.Marshal(eventData)
	_ = os.WriteFile(eventFile, data, 0644)

	envCleanup := setEnv(t, map[string]string{
		"GITHUB_ACTIONS":    "true",
		"GITHUB_REPOSITORY": "owner/repo",
		"GITHUB_REF":        "refs/heads/main",
		"GITHUB_EVENT_PATH": eventFile,
		"GITHUB_TOKEN":      "token",
	})
	defer envCleanup()

	env, err := Detect()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.PRNumber != 555 {
		t.Errorf("expected PR number 555 from event file, got %d", env.PRNumber)
	}
}

func TestDetectGitHub_NoPRNumber(t *testing.T) {
	cleanup := clearCIEnvVars(t)
	defer cleanup()

	envCleanup := setEnv(t, map[string]string{
		"GITHUB_ACTIONS":    "true",
		"GITHUB_REPOSITORY": "owner/repo",
		"GITHUB_REF":        "refs/heads/main", // Not a PR ref
		"GITHUB_TOKEN":      "token",
	})
	defer envCleanup()

	_, err := Detect()
	if err == nil {
		t.Fatal("expected error when PR number cannot be determined")
	}
}

func TestDetectGitLab_MissingProjectPath(t *testing.T) {
	cleanup := clearCIEnvVars(t)
	defer cleanup()

	envCleanup := setEnv(t, map[string]string{
		"GITLAB_CI":            "true",
		"CI_MERGE_REQUEST_IID": "123",
		"GITLAB_TOKEN":         "token",
	})
	defer envCleanup()

	_, err := Detect()
	if err == nil {
		t.Fatal("expected error for missing CI_PROJECT_PATH")
	}
}

func TestDetectGitLab_InvalidProjectPath(t *testing.T) {
	cleanup := clearCIEnvVars(t)
	defer cleanup()

	envCleanup := setEnv(t, map[string]string{
		"GITLAB_CI":            "true",
		"CI_PROJECT_PATH":      "invalid",
		"CI_MERGE_REQUEST_IID": "123",
		"GITLAB_TOKEN":         "token",
	})
	defer envCleanup()

	_, err := Detect()
	if err == nil {
		t.Fatal("expected error for invalid project path format")
	}
}

func TestDetectGitLab_MissingMRIID(t *testing.T) {
	cleanup := clearCIEnvVars(t)
	defer cleanup()

	envCleanup := setEnv(t, map[string]string{
		"GITLAB_CI":       "true",
		"CI_PROJECT_PATH": "group/project",
		"GITLAB_TOKEN":    "token",
	})
	defer envCleanup()

	_, err := Detect()
	if err == nil {
		t.Fatal("expected error for missing CI_MERGE_REQUEST_IID")
	}
}

func TestDetectGitLab_InvalidMRIID(t *testing.T) {
	cleanup := clearCIEnvVars(t)
	defer cleanup()

	envCleanup := setEnv(t, map[string]string{
		"GITLAB_CI":            "true",
		"CI_PROJECT_PATH":      "group/project",
		"CI_MERGE_REQUEST_IID": "not-a-number",
		"GITLAB_TOKEN":         "token",
	})
	defer envCleanup()

	_, err := Detect()
	if err == nil {
		t.Fatal("expected error for invalid MR IID")
	}
}

func TestDetectGitLab_MissingToken(t *testing.T) {
	cleanup := clearCIEnvVars(t)
	defer cleanup()

	envCleanup := setEnv(t, map[string]string{
		"GITLAB_CI":            "true",
		"CI_PROJECT_PATH":      "group/project",
		"CI_MERGE_REQUEST_IID": "123",
	})
	defer envCleanup()

	_, err := Detect()
	if err == nil {
		t.Fatal("expected error for missing token")
	}
}

func TestDetectGitLab_FallbackToJobToken(t *testing.T) {
	cleanup := clearCIEnvVars(t)
	defer cleanup()

	envCleanup := setEnv(t, map[string]string{
		"GITLAB_CI":            "true",
		"CI_PROJECT_PATH":      "group/project",
		"CI_MERGE_REQUEST_IID": "123",
		"CI_JOB_TOKEN":         "job-token",
	})
	defer envCleanup()

	env, err := Detect()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.Token != "job-token" {
		t.Errorf("expected job token fallback, got %s", env.Token)
	}
}

func TestDetectGitLab_PreferGitLabToken(t *testing.T) {
	cleanup := clearCIEnvVars(t)
	defer cleanup()

	envCleanup := setEnv(t, map[string]string{
		"GITLAB_CI":            "true",
		"CI_PROJECT_PATH":      "group/project",
		"CI_MERGE_REQUEST_IID": "123",
		"GITLAB_TOKEN":         "gitlab-token",
		"CI_JOB_TOKEN":         "job-token",
	})
	defer envCleanup()

	env, err := Detect()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.Token != "gitlab-token" {
		t.Errorf("expected GITLAB_TOKEN to be preferred, got %s", env.Token)
	}
}

func TestDetectGitLab_DefaultServerHost(t *testing.T) {
	cleanup := clearCIEnvVars(t)
	defer cleanup()

	envCleanup := setEnv(t, map[string]string{
		"GITLAB_CI":            "true",
		"CI_PROJECT_PATH":      "group/project",
		"CI_MERGE_REQUEST_IID": "123",
		"GITLAB_TOKEN":         "token",
	})
	defer envCleanup()

	env, err := Detect()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.ServerHost != "gitlab.com" {
		t.Errorf("expected default server host 'gitlab.com', got %s", env.ServerHost)
	}
}

func TestEnvironment_Fields(t *testing.T) {
	env := Environment{
		Provider:   "github",
		Owner:      "test-owner",
		Repo:       "test-repo",
		PRNumber:   42,
		Token:      "secret",
		ServerHost: "github.com",
	}

	if env.Provider != "github" {
		t.Errorf("provider mismatch")
	}
	if env.Owner != "test-owner" {
		t.Errorf("owner mismatch")
	}
	if env.Repo != "test-repo" {
		t.Errorf("repo mismatch")
	}
	if env.PRNumber != 42 {
		t.Errorf("pr number mismatch")
	}
	if env.Token != "secret" {
		t.Errorf("token mismatch")
	}
	if env.ServerHost != "github.com" {
		t.Errorf("server host mismatch")
	}
}
