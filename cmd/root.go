package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/iq2i/ainspector/internal/ci"
	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/config"
	"github.com/iq2i/ainspector/internal/findings"
	"github.com/iq2i/ainspector/internal/llm"
	"github.com/iq2i/ainspector/internal/llm/openai"
	"github.com/iq2i/ainspector/internal/modes"
	"github.com/iq2i/ainspector/internal/pipeline"
	"github.com/iq2i/ainspector/internal/provider"
	"github.com/iq2i/ainspector/internal/publish"
	"github.com/iq2i/ainspector/internal/state"
	"github.com/spf13/cobra"
)

var (
	version     = "0.1.0"
	forceReview bool
)

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
	reviewCmd.Flags().BoolVarP(&forceReview, "force", "f", false, "Force re-review of all functions, ignoring cache")
	rootCmd.AddCommand(reviewCmd)
	rootCmd.AddCommand(versionCmd)
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runReview(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	env, err := ci.Detect()
	if err != nil {
		return fmt.Errorf("CI detection failed: %w", err)
	}

	fmt.Printf("Detected %s CI environment\n", env.Provider)
	fmt.Printf("Repository: %s/%s, PR/MR: #%d\n", env.Owner, env.Repo, env.PRNumber)

	var p provider.Provider
	if env.Provider == "github" {
		p = provider.NewGitHubProvider(env.Owner, env.Repo, env.Token)
	} else {
		p = provider.NewGitLabProvider(env.ServerHost, env.Owner, env.Repo, env.Token)
	}

	projectRoot, err := os.Getwd()
	if err != nil {
		fmt.Printf("Warning: could not get working directory: %v\n", err)
		projectRoot = "."
	}

	return review(context.Background(), p, newLLMClient, cfg, env.PRNumber, projectRoot, forceReview)
}

func newLLMClient() (llm.Client, error) {
	apiURL := os.Getenv("LLM_BASE_URL")
	if apiURL == "" {
		apiURL = "https://api.openai.com"
	}

	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("LLM_API_KEY environment variable is required")
	}

	model := os.Getenv("LLM_MODEL")
	if model == "" {
		model = "gpt-4o"
	}

	return openai.New(apiURL, apiKey, model), nil
}

func review(ctx context.Context, p provider.Provider, newClient func() (llm.Client, error), cfg *config.Config, number int, projectRoot string, force bool) error {
	fmt.Printf("Fetching modified files...\n")
	files, err := p.GetModifiedFiles(ctx, number)
	if err != nil {
		return fmt.Errorf("failed to get modified files: %w", err)
	}

	fmt.Printf("Found %d modified files\n", len(files))

	collector := collect.New(p, cfg)
	defer collector.Close()
	functions, err := collector.Collect(ctx, collect.Request{Files: files})
	if err != nil {
		return fmt.Errorf("failed to extract functions: %w", err)
	}

	fmt.Printf("Extracted %d modified functions\n", len(functions))

	if len(functions) == 0 {
		fmt.Println("No functions to review")
		return nil
	}

	languagesMap := make(map[string]bool)
	for _, fn := range functions {
		languagesMap[fn.Language] = true
	}
	var languages []string
	for lang := range languagesMap {
		languages = append(languages, lang)
	}

	enabled, err := modes.Enabled([]string{"quality"}, cfg)
	if err != nil {
		return err
	}
	mode := enabled[0]

	functionsToReview := functions
	if !force {
		fmt.Println("Checking for previously reviewed functions...")
		existingComments, err := p.ListComments(ctx, number)
		if err != nil {
			fmt.Printf("Warning: could not fetch existing comments: %v\n", err)
		} else {
			self, err := p.CurrentUser(ctx)
			if err != nil {
				fmt.Printf("Warning: could not identify the token owner, trusting every existing comment: %v\n", err)
			}
			functionsToReview = state.LoadIndex(existingComments, self).FilterUnreviewed(mode, state.RulesDigest(cfg.Rules), functions)
			skipped := len(functions) - len(functionsToReview)
			if skipped > 0 {
				fmt.Printf("Skipped %d already reviewed functions\n", skipped)
			}
		}
	} else {
		fmt.Println("Force flag set - reviewing all modified functions")
	}

	if len(functionsToReview) == 0 {
		fmt.Println("All modified functions have already been reviewed")
		return nil
	}

	client, err := newClient()
	if err != nil {
		return err
	}

	fmt.Println("Generating project context...")
	projectContext, err := collect.GenerateProjectContext(ctx, client, projectRoot, languages, &cfg.Context)
	if err != nil {
		fmt.Printf("Warning: failed to generate project context: %v\n", err)
		projectContext = nil
	} else if projectContext.Description != "" {
		if projectContext.IsRaw {
			fmt.Printf("Project context loaded from %d configured file(s)\n", len(cfg.Context.Include))
		} else {
			fmt.Printf("Project context: %s\n", projectContext.Description)
		}
	}

	fmt.Printf("Reviewing %d functions with LLM...\n", len(functionsToReview))
	results := pipeline.ReviewUnits(ctx, client, mode, functionsToReview, projectContext)

	var published []findings.Finding
	for _, result := range results {
		published = append(published, result.Findings...)
	}

	fmt.Printf("Found %d issues (out of %d functions reviewed)\n", len(published), len(results))

	if len(published) == 0 {
		fmt.Println("No issues found, skipping review")
		return nil
	}

	fmt.Println("Posting review with inline suggestions...")
	run := publish.Run{Mode: mode.Name(), Units: functionsToReview, Published: published}
	if err := publish.NewInline(p, number).Publish(ctx, run); err != nil {
		return fmt.Errorf("failed to create review: %w", err)
	}

	fmt.Println("Review posted successfully!")
	return nil
}
