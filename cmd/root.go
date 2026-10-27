package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/iq2i/ainspector/internal/ci"
	"github.com/iq2i/ainspector/internal/config"
	"github.com/iq2i/ainspector/internal/llm"
	"github.com/iq2i/ainspector/internal/pipeline"
	"github.com/iq2i/ainspector/internal/provider"
	"github.com/spf13/cobra"
)

var (
	version     = "0.1.0"
	forceReview bool
	dryRun      bool
	reviewModes []string
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

LLM backend, one of:
  ANTHROPIC_API_KEY - API key for the Anthropic API (default model: claude-sonnet-5-5)
  LLM_API_KEY       - API key for an OpenAI-compatible service
    LLM_BASE_URL    - API base URL (default: https://api.openai.com)
    LLM_MODEL       - model name (default: gpt-4o)
The Anthropic backend is used when ANTHROPIC_API_KEY is set, unless llm.provider says otherwise.

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
	reviewCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Run the review but print the comments instead of posting them")
	reviewCmd.Flags().StringSliceVar(&reviewModes, "mode", nil, "Review modes to run, comma separated (default: the modes enabled in the configuration)")
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return pipeline.Run(ctx, pipeline.Deps{
		Provider:    p,
		NewLLM:      func() (llm.Client, error) { return newLLMClient(cfg.LLM) },
		Config:      cfg,
		ProjectRoot: projectRoot,
		ReportDir:   projectRoot,
	}, pipeline.Options{
		Number:      env.PRNumber,
		CheckoutSHA: env.CommitSHA,
		Modes:       reviewModes,
		Force:       forceReview,
		DryRun:      dryRun,
	})
}
