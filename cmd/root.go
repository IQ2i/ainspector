package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/iq2i/ainspector/internal/ci"
	"github.com/iq2i/ainspector/internal/config"
	"github.com/iq2i/ainspector/internal/extractor"
	"github.com/iq2i/ainspector/internal/llm"
	"github.com/iq2i/ainspector/internal/provider"
	"github.com/spf13/cobra"
)

var version = "0.1.0"

var rootCmd = &cobra.Command{
	Use:   "ainspector",
	Short: "AI-powered code review tool for GitHub PRs and GitLab MRs",
	Long:  `ainspector analyzes pull requests and merge requests to extract modified functions for AI-powered code review.`,
}

var reviewCmd = &cobra.Command{
	Use:   "review",
	Short: "Review a pull request or merge request",
	Long: `Analyzes a GitHub Pull Request or GitLab Merge Request and extracts functions that contain modified lines.

This command automatically detects the CI environment (GitHub Actions or GitLab CI) and posts the review as a comment on the PR/MR.

Required environment variables:
  LLM_API_KEY     - API key for the LLM service

Optional environment variables:
  LLM_BASE_URL    - LLM API base URL (default: https://api.openai.com)
  LLM_MODEL       - LLM model name (default: gpt-4o)

For GitHub Actions:
  GITHUB_TOKEN    - GitHub API token (usually provided automatically)

For GitLab CI:
  GITLAB_TOKEN    - GitLab API token (or CI_JOB_TOKEN)`,
	Args: cobra.NoArgs,
	RunE: runReview,
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("ainspector %s\n", version)
	},
}

func init() {
	rootCmd.AddCommand(reviewCmd)
	rootCmd.AddCommand(versionCmd)
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runReview(cmd *cobra.Command, args []string) error {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Detect CI environment
	env, err := ci.Detect()
	if err != nil {
		return fmt.Errorf("CI detection failed: %w", err)
	}

	fmt.Printf("Detected %s CI environment\n", env.Provider)
	fmt.Printf("Repository: %s/%s, PR/MR: #%d\n", env.Owner, env.Repo, env.PRNumber)

	// Create provider based on detected environment
	var p provider.Provider
	if env.Provider == "github" {
		p = provider.NewGitHubProvider(env.Owner, env.Repo, env.Token)
	} else {
		p = provider.NewGitLabProvider(env.ServerHost, env.Owner, env.Repo, env.Token)
	}

	ctx := context.Background()

	// Get modified files
	fmt.Printf("Fetching modified files...\n")
	files, err := p.GetModifiedFiles(ctx, env.PRNumber)
	if err != nil {
		return fmt.Errorf("failed to get modified files: %w", err)
	}

	fmt.Printf("Found %d modified files\n", len(files))

	// Extract functions
	ext := extractor.New(p, cfg)
	defer ext.Close()
	functions, err := ext.ExtractModifiedFunctions(ctx, files)
	if err != nil {
		return fmt.Errorf("failed to extract functions: %w", err)
	}

	fmt.Printf("Extracted %d modified functions\n", len(functions))

	if len(functions) == 0 {
		fmt.Println("No functions to review")
		return nil
	}

	// Get LLM config from environment variables
	apiURL := os.Getenv("LLM_BASE_URL")
	if apiURL == "" {
		apiURL = "https://api.openai.com"
	}

	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("LLM_API_KEY environment variable is required")
	}

	model := os.Getenv("LLM_MODEL")
	if model == "" {
		model = "gpt-4o"
	}

	// Review with LLM
	client := llm.NewClient(apiURL, apiKey, model)
	fmt.Printf("Reviewing with LLM (%s)...\n", model)

	results := llm.ReviewFunctions(ctx, client, functions)

	// Convert results to review comments
	var comments []provider.ReviewComment
	for _, result := range results {
		if !result.HasIssues() {
			continue
		}

		for _, suggestion := range result.Suggestions {
			comment := provider.ReviewComment{
				Path:       result.Function.FilePath,
				Line:       suggestion.Line,
				Body:       suggestion.Description,
				Suggestion: suggestion.Code,
			}
			comments = append(comments, comment)
		}
	}

	fmt.Printf("Found %d issues (out of %d functions reviewed)\n", len(comments), len(results))

	// Skip posting if no issues found
	if len(comments) == 0 {
		fmt.Println("No issues found, skipping review")
		return nil
	}

	// Post inline review comments
	fmt.Println("Posting review with inline suggestions...")
	if err := p.CreateReview(ctx, env.PRNumber, comments); err != nil {
		return fmt.Errorf("failed to create review: %w", err)
	}

	fmt.Println("Review posted successfully!")
	return nil
}
