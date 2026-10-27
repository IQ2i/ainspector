package pipeline

import (
	"context"
	"errors"
	"fmt"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/config"
	"github.com/iq2i/ainspector/internal/llm"
	"github.com/iq2i/ainspector/internal/modes"
	"github.com/iq2i/ainspector/internal/provider"
	"github.com/iq2i/ainspector/internal/publish"
	"github.com/iq2i/ainspector/internal/state"
)

// Deps are the collaborators of a run
type Deps struct {
	Provider    provider.Provider
	NewLLM      func() (llm.Client, error)
	Config      *config.Config
	ProjectRoot string
}

// Options tune a run
type Options struct {
	Number int
	Modes  []string
	Force  bool
	// DryRun prints what would be published without writing anything to the PR/MR
	DryRun bool
}

// Run reviews a PR/MR with each requested mode
func Run(ctx context.Context, deps Deps, opts Options) error {
	cfg := deps.Config

	names := opts.Modes
	if len(names) == 0 {
		names = cfg.EnabledModes()
	}
	enabled, err := modes.Enabled(names, cfg)
	if err != nil {
		return err
	}
	if len(enabled) == 0 {
		fmt.Println("No review mode enabled")
		return nil
	}

	if cfg.HasChangeFilters() {
		info, err := deps.Provider.GetChangeInfo(ctx, opts.Number)
		if err != nil {
			return fmt.Errorf("failed to get change info: %w", err)
		}
		if ignored, reason := cfg.ShouldIgnoreChange(info.Author, info.Title, info.Labels); ignored {
			fmt.Printf("Skipping review: %s\n", reason)
			return nil
		}
	}

	fmt.Printf("Fetching modified files...\n")
	files, err := deps.Provider.GetModifiedFiles(ctx, opts.Number)
	if err != nil {
		return fmt.Errorf("failed to get modified files: %w", err)
	}

	fmt.Printf("Found %d modified files\n", len(files))

	collector := collect.New(deps.Provider, cfg)
	defer collector.Close()
	units, err := collector.Collect(ctx, collect.Request{Files: files, Scope: maxScope(enabled)})
	if err != nil {
		return fmt.Errorf("failed to extract functions: %w", err)
	}

	fmt.Printf("Extracted %d modified functions\n", len(units))

	if len(units) == 0 {
		fmt.Println("No functions to review")
		return nil
	}

	self, err := deps.Provider.CurrentUser(ctx)
	if err != nil {
		fmt.Printf("Warning: could not identify the token owner: %v\n", err)
		self = ""
	}

	var index *state.Index
	if opts.Force {
		fmt.Println("Force flag set - reviewing all modified functions")
	} else {
		fmt.Println("Checking for previously reviewed functions...")
		existing, err := deps.Provider.ListComments(ctx, opts.Number)
		if err != nil {
			fmt.Printf("Warning: could not fetch existing comments: %v\n", err)
		} else {
			if self == "" {
				fmt.Println("Warning: trusting every existing comment")
			}
			index = state.LoadIndex(existing, self)
		}
	}

	// Created on first use: nothing is needed when every unit was already reviewed
	var budget *llm.Budget
	var projectContext *collect.ProjectContext

	var errs []error
	for _, mode := range enabled {
		toReview := units
		if index != nil {
			toReview = index.FilterUnreviewed(mode, state.RulesDigest(mode.Rules()), units)
			if skipped := len(units) - len(toReview); skipped > 0 {
				fmt.Printf("Skipped %d already reviewed functions\n", skipped)
			}
		}

		if len(toReview) == 0 {
			fmt.Println("All modified functions have already been reviewed")
			continue
		}

		if budget == nil {
			client, err := deps.NewLLM()
			if err != nil {
				return err
			}
			budget = llm.WithBudget(client, cfg.LLM.TokenBudget(), newPricing(cfg), logLine)
			projectContext = loadProjectContext(deps.ProjectRoot, cfg, units)
		}

		fmt.Printf("Reviewing %d functions with LLM...\n", len(toReview))
		before := budget.Totals()
		results := ReviewUnits(ctx, budget, mode, toReview, projectContext)
		run := newRun(mode, toReview, results, budget.Totals().Sub(before), projectContext)

		fmt.Printf("Found %d issues (out of %d functions reviewed)\n", len(run.Published), len(run.Reviewed))
		if n := len(run.Skipped); n > 0 {
			fmt.Printf("Warning: %d functions were not reviewed, the token budget was reached\n", n)
		}
		fmt.Printf("Tokens used by %s: %d in, %d out, %d read from cache, %d written to cache\n", mode.Name(),
			run.Usage.Input, run.Usage.Output, run.Usage.CacheRead, run.Usage.CacheWrite)

		if err := publishRun(ctx, deps.Provider, opts, self, mode, run); err != nil {
			fmt.Printf("Warning: mode %s: %v\n", mode.Name(), err)
			errs = append(errs, fmt.Errorf("mode %s: %w", mode.Name(), err))
		}
	}

	return errors.Join(errs...)
}

// newRun gathers what a mode produced and consumed, to publish it
func newRun(mode modes.Mode, units []collect.ReviewUnit, results []UnitResult, used llm.Totals, pc *collect.ProjectContext) publish.Run {
	run := publish.Run{
		Mode:           mode.Name(),
		Scope:          mode.Scope().String(),
		Version:        mode.Version(),
		Units:          units,
		Usage:          used.Usage,
		Cost:           used.Cost,
		CostIncomplete: used.UnpricedCalls > 0,
	}
	if pc != nil {
		run.Notes = pc.Notes
	}

	for _, r := range results {
		if run.Model == "" {
			run.Model = r.Model
		}

		label := fmt.Sprintf("%s (%s)", r.Unit.Name, r.Unit.FilePath)
		switch {
		case r.Skipped:
			run.Skipped = append(run.Skipped, label)
		case r.Err != nil:
			run.Failed = append(run.Failed, label)
		default:
			run.Reviewed = append(run.Reviewed, r.Hash)
			run.Published = append(run.Published, r.Findings...)
		}
	}
	return run
}

func publishRun(ctx context.Context, p provider.Provider, opts Options, self string, mode modes.Mode, run publish.Run) error {
	policy := mode.PublishPolicy()

	if opts.DryRun {
		for _, f := range run.Published {
			fmt.Printf("Dry run: would comment on %s:%d\n%s\n", f.File, f.StartLine, publish.BodyFor(f))
		}
		if policy.Summary {
			fmt.Printf("Dry run: would publish the summary:\n%s\n", publish.SummaryBody(run, nil))
		}
		return nil
	}

	var errs []error

	if policy.Inline && len(run.Published) > 0 {
		fmt.Println("Posting review with inline suggestions...")
		if err := publish.NewInline(p, opts.Number).Publish(ctx, run); err != nil {
			errs = append(errs, fmt.Errorf("failed to create review: %w", err))
		} else {
			fmt.Println("Review posted successfully!")
		}
	}

	// The summary carries the state of later runs: without the identity of the token owner
	// it could not tell its own comment from a forged one. Not worth failing the job over.
	switch {
	case !policy.Summary:
	case self == "":
		fmt.Printf("Warning: summary of %s skipped, the token owner is unknown\n", mode.Name())
	default:
		if err := publish.NewSummary(p, opts.Number, self).Publish(ctx, run); err != nil {
			errs = append(errs, fmt.Errorf("failed to publish the summary: %w", err))
		}
	}

	return errors.Join(errs...)
}

func newPricing(cfg *config.Config) llm.Pricing {
	overrides := make(map[string]llm.Price, len(cfg.LLM.Prices))
	for model, p := range cfg.LLM.Prices {
		overrides[model] = llm.Price(p)
	}
	return llm.NewPricing(overrides)
}

func logLine(format string, args ...any) {
	fmt.Printf(format+"\n", args...)
}

func loadProjectContext(root string, cfg *config.Config, units []collect.ReviewUnit) *collect.ProjectContext {
	languages := make(map[string]bool)
	var ordered []string
	for _, u := range units {
		if !languages[u.Language] {
			languages[u.Language] = true
			ordered = append(ordered, u.Language)
		}
	}

	pc, err := collect.GenerateProjectContext(root, ordered, &cfg.Context)
	if err != nil {
		fmt.Printf("Warning: failed to load project context: %v\n", err)
		return nil
	}

	if pc.Description != "" {
		fmt.Printf("Project context loaded (%d bytes)\n", len(pc.Description))
	}
	for _, note := range pc.Notes {
		fmt.Printf("Warning: %s\n", note)
	}
	return pc
}

func maxScope(ms []modes.Mode) collect.Scope {
	var scope collect.Scope
	for _, m := range ms {
		if m.Scope() > scope {
			scope = m.Scope()
		}
	}
	return scope
}
