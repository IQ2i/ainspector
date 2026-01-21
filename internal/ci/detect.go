package ci

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Environment represents the detected CI environment
type Environment struct {
	Provider   string // "github" or "gitlab"
	Owner      string // Repository owner
	Repo       string // Repository name
	PRNumber   int    // Pull request / Merge request number
	Token      string // API token
	ServerHost string // Server host (for self-hosted instances)
}

// Detect detects the CI environment from environment variables
func Detect() (*Environment, error) {
	// Check for GitHub Actions
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		return detectGitHub()
	}

	// Check for GitLab CI
	if os.Getenv("GITLAB_CI") == "true" {
		return detectGitLab()
	}

	return nil, fmt.Errorf("not running in a supported CI environment (GitHub Actions or GitLab CI)")
}

// detectGitHub detects GitHub Actions environment
func detectGitHub() (*Environment, error) {
	// Get repository (format: owner/repo)
	repository := os.Getenv("GITHUB_REPOSITORY")
	if repository == "" {
		return nil, fmt.Errorf("GITHUB_REPOSITORY not set")
	}

	parts := strings.SplitN(repository, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid GITHUB_REPOSITORY format: %s", repository)
	}

	// Get PR number from GITHUB_REF (refs/pull/123/merge) or event file
	prNumber, err := getGitHubPRNumber()
	if err != nil {
		return nil, err
	}

	// Get token
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("GITHUB_TOKEN not set")
	}

	return &Environment{
		Provider:   "github",
		Owner:      parts[0],
		Repo:       parts[1],
		PRNumber:   prNumber,
		Token:      token,
		ServerHost: "github.com",
	}, nil
}

// getGitHubPRNumber extracts the PR number from GitHub Actions environment
func getGitHubPRNumber() (int, error) {
	// First, try to get from GITHUB_REF (refs/pull/123/merge)
	ref := os.Getenv("GITHUB_REF")
	if strings.HasPrefix(ref, "refs/pull/") {
		re := regexp.MustCompile(`refs/pull/(\d+)/`)
		matches := re.FindStringSubmatch(ref)
		if len(matches) == 2 {
			return strconv.Atoi(matches[1])
		}
	}

	// Try to get from event file
	eventPath := os.Getenv("GITHUB_EVENT_PATH")
	if eventPath != "" {
		data, err := os.ReadFile(eventPath)
		if err == nil {
			var event struct {
				PullRequest struct {
					Number int `json:"number"`
				} `json:"pull_request"`
				Number int `json:"number"`
			}
			if err := json.Unmarshal(data, &event); err == nil {
				if event.PullRequest.Number > 0 {
					return event.PullRequest.Number, nil
				}
				if event.Number > 0 {
					return event.Number, nil
				}
			}
		}
	}

	return 0, fmt.Errorf("could not determine PR number: not running in a pull_request event")
}

// detectGitLab detects GitLab CI environment
func detectGitLab() (*Environment, error) {
	// Get project path (format: owner/repo)
	projectPath := os.Getenv("CI_PROJECT_PATH")
	if projectPath == "" {
		return nil, fmt.Errorf("CI_PROJECT_PATH not set")
	}

	parts := strings.SplitN(projectPath, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid CI_PROJECT_PATH format: %s", projectPath)
	}

	// Get MR number
	mrIID := os.Getenv("CI_MERGE_REQUEST_IID")
	if mrIID == "" {
		return nil, fmt.Errorf("CI_MERGE_REQUEST_IID not set: not running in a merge_request pipeline")
	}

	mrNumber, err := strconv.Atoi(mrIID)
	if err != nil {
		return nil, fmt.Errorf("invalid CI_MERGE_REQUEST_IID: %s", mrIID)
	}

	// Get server host (for self-hosted instances)
	serverHost := os.Getenv("CI_SERVER_HOST")
	if serverHost == "" {
		serverHost = "gitlab.com"
	}

	// Get token (prefer GITLAB_TOKEN, fallback to CI_JOB_TOKEN)
	token := os.Getenv("GITLAB_TOKEN")
	if token == "" {
		token = os.Getenv("CI_JOB_TOKEN")
	}
	if token == "" {
		return nil, fmt.Errorf("GITLAB_TOKEN or CI_JOB_TOKEN not set")
	}

	return &Environment{
		Provider:   "gitlab",
		Owner:      parts[0],
		Repo:       parts[1],
		PRNumber:   mrNumber,
		Token:      token,
		ServerHost: serverHost,
	}, nil
}
