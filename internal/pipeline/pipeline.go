package pipeline

import (
	"context"
	"errors"
	"fmt"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/config"
	"github.com/iq2i/ainspector/internal/findings"
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
		names = []string{"quality"}
	}
	enabled, err := modes.Enabled(names, cfg)
	if err != nil {
		return err
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

	var index *state.Index
	if opts.Force {
		fmt.Println("Force flag set - reviewing all modified functions")
	} else {
		fmt.Println("Checking for previously reviewed functions...")
		existing, err := deps.Provider.ListComments(ctx, opts.Number)
		if err != nil {
			fmt.Printf("Warning: could not fetch existing comments: %v\n", err)
		} else {
			self, err := deps.Provider.CurrentUser(ctx)
			if err != nil {
				fmt.Printf("Warning: could not identify the token owner, trusting every existing comment: %v\n", err)
			}
			index = state.LoadIndex(existing, self)
		}
	}

	// Created on first use: nothing is needed when every unit was already reviewed
	var client llm.Client
	var projectContext *collect.ProjectContext

	var errs []error
	for _, mode := range enabled {
		toReview := units
		if index != nil {
			toReview = index.FilterUnreviewed(mode, state.RulesDigest(cfg.Rules), units)
			if skipped := len(units) - len(toReview); skipped > 0 {
				fmt.Printf("Skipped %d already reviewed functions\n", skipped)
			}
		}

		if len(toReview) == 0 {
			fmt.Println("All modified functions have already been reviewed")
			continue
		}

		if client == nil {
			if client, err = deps.NewLLM(); err != nil {
				return err
			}
			projectContext = loadProjectContext(ctx, client, deps.ProjectRoot, cfg, units)
		}

		fmt.Printf("Reviewing %d functions with LLM...\n", len(toReview))
		results := ReviewUnits(ctx, client, mode, toReview, projectContext)

		var published []findings.Finding
		for _, r := range results {
			published = append(published, r.Findings...)
		}

		fmt.Printf("Found %d issues (out of %d functions reviewed)\n", len(published), len(results))

		if len(published) == 0 {
			fmt.Println("No issues found, skipping review")
			continue
		}

		run := publish.Run{Mode: mode.Name(), Units: toReview, Published: published}
		if err := publishRun(ctx, deps.Provider, opts, mode, run); err != nil {
			fmt.Printf("Warning: mode %s: %v\n", mode.Name(), err)
			errs = append(errs, fmt.Errorf("mode %s: %w", mode.Name(), err))
		}
	}

	return errors.Join(errs...)
}

func publishRun(ctx context.Context, p provider.Provider, opts Options, mode modes.Mode, run publish.Run) error {
	if opts.DryRun {
		for _, f := range run.Published {
			fmt.Printf("Dry run: would comment on %s:%d\n%s\n", f.File, f.StartLine, publish.BodyFor(f))
		}
		return nil
	}

	if !mode.PublishPolicy().Inline {
		return nil
	}

	fmt.Println("Posting review with inline suggestions...")
	if err := publish.NewInline(p, opts.Number).Publish(ctx, run); err != nil {
		return fmt.Errorf("failed to create review: %w", err)
	}

	fmt.Println("Review posted successfully!")
	return nil
}

func loadProjectContext(ctx context.Context, client llm.Client, root string, cfg *config.Config, units []collect.ReviewUnit) *collect.ProjectContext {
	languages := make(map[string]bool)
	var ordered []string
	for _, u := range units {
		if !languages[u.Language] {
			languages[u.Language] = true
			ordered = append(ordered, u.Language)
		}
	}

	fmt.Println("Generating project context...")
	pc, err := collect.GenerateProjectContext(ctx, client, root, ordered, &cfg.Context)
	if err != nil {
		fmt.Printf("Warning: failed to generate project context: %v\n", err)
		return nil
	}

	if pc.Description != "" {
		if pc.IsRaw {
			fmt.Printf("Project context loaded from %d configured file(s)\n", len(cfg.Context.Include))
		} else {
			fmt.Printf("Project context: %s\n", pc.Description)
		}
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
