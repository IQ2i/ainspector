package pipeline

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

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
	// MaxTokens caps the tokens of a run whatever the configuration says, zero means no cap.
	// The configuration comes from the checkout of the change and its author could raise its own.
	MaxTokens int
	// Secrets are the credentials of the run, hidden from the errors of the LLM backend
	Secrets []string
	// ReportDir is where the Code Quality report and the artifact are written, defaults to ProjectRoot
	ReportDir string
}

// Options tune a run
type Options struct {
	Number int
	Modes  []string
	Force  bool
	// CheckoutSHA is the commit checked out by the job, to tell when it is not the head of the change
	CheckoutSHA string
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
		return stopped(ctx, deps, opts, nil, "no review mode is enabled")
	}

	var cached *provider.ChangeInfo
	changeInfo := func() (provider.ChangeInfo, error) {
		if cached != nil {
			return *cached, nil
		}
		got, err := deps.Provider.GetChangeInfo(ctx, opts.Number)
		if err != nil {
			return provider.ChangeInfo{}, err
		}
		cached = &got
		return got, nil
	}

	if cfg.HasChangeFilters() {
		info, err := changeInfo()
		if err != nil {
			return fmt.Errorf("failed to get change info: %w", err)
		}
		if ignored, reason := cfg.ShouldIgnoreChange(info.Author, info.Title, info.Labels); ignored {
			fmt.Printf("Skipping review: %s\n", reason)
			return stopped(ctx, deps, opts, nil, reason)
		}
	}

	reportDir := deps.ReportDir
	if reportDir == "" {
		reportDir = deps.ProjectRoot
	}
	out := newReports(reportDir, cfg, publish.Change{Number: opts.Number, CheckoutSHA: opts.CheckoutSHA})

	fmt.Printf("Fetching modified files...\n")
	files, err := deps.Provider.GetModifiedFiles(ctx, opts.Number)
	if err != nil {
		return fmt.Errorf("failed to get modified files: %w", err)
	}

	files, unsafe := dropUnsafePaths(files)
	var changeNotes []string
	if name := changedConfig(files); name != "" {
		note := fmt.Sprintf("This change modifies %s: the settings of its new version apply to this review", name)
		fmt.Printf("Warning: %s\n", note)
		changeNotes = append(changeNotes, note)
	}
	if unsafe > 0 {
		changeNotes = append(changeNotes, fmt.Sprintf("%d %s skipped, the name holds control characters", unsafe, plural(unsafe, "file")))
	}
	if note := hiddenNote(files); note != "" {
		fmt.Printf("Warning: %s\n", note)
		changeNotes = append(changeNotes, note)
	}
	fmt.Printf("Found %d modified files\n", len(files))

	collector := collect.New(deps.Provider, cfg)
	defer collector.Close()
	req := collectRequest(files, enabled)
	units, err := collector.Collect(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to extract functions: %w", err)
	}

	fmt.Printf("Extracted %d modified units\n", len(units))

	if len(units) == 0 {
		fmt.Println("No code to review")
		var hidden []string
		if note := hiddenNote(files); note != "" {
			hidden = append(hidden, note)
		}
		return errors.Join(out.write(), stopped(ctx, deps, opts, files, "no file of the change is left to review", hidden...))
	}

	if len(req.Related.Presets) > 0 {
		if err := attachRelated(ctx, deps, opts, units, req.Related, changeInfo); err != nil {
			return err
		}
	}

	self, err := deps.Provider.CurrentUser(ctx)
	if err != nil {
		fmt.Printf("Warning: could not identify the token owner: %v\n", err)
		self = ""
	}

	var index *state.Index
	var feedback map[string][]publish.Feedback
	if opts.Force {
		fmt.Println("Force flag set - reviewing all modified units")
	} else {
		fmt.Println("Checking for previously reviewed units...")
		existing, err := deps.Provider.ListComments(ctx, opts.Number)
		if err != nil {
			fmt.Printf("Warning: could not fetch existing comments: %v\n", err)
		} else {
			if self == "" {
				fmt.Println("Warning: trusting every existing comment")
			}
			index = state.LoadIndex(existing, self)
			feedback = collectFeedback(ctx, deps.Provider, opts.Number, existing, self)
		}
	}

	// Created on first use: nothing is needed when every unit was already reviewed
	var budget *llm.Budget
	var projectContext *collect.ProjectContext

	var errs []error
	for _, mode := range enabled {
		candidates := unitsFor(mode, units)
		toReview := candidates
		var carried []findings.Reported
		if index != nil {
			digest := state.RulesDigest(mode.Rules())
			toReview = index.FilterUnreviewed(mode, digest, candidates)
			if skipped := len(candidates) - len(toReview); skipped > 0 {
				fmt.Printf("Skipped %d already reviewed units\n", skipped)
			}
			carried = index.OpenFindings(mode, skippedHashes(mode, digest, candidates, toReview))
		}
		alreadyReviewed := len(candidates) - len(toReview)

		if len(toReview) == 0 {
			fmt.Printf("Nothing left to review for %s\n", mode.Name())
			run := publish.Run{Mode: mode.Name(), Scope: mode.Scope().String(), Version: mode.Version(), Carried: carried, AlreadyReviewed: alreadyReviewed, Notes: slices.Clone(changeNotes), Feedback: feedback[mode.Name()]}
			if err := out.add(ctx, mode.PublishPolicy(), run); err != nil {
				errs = append(errs, fmt.Errorf("mode %s: %w", mode.Name(), err))
			}
			continue
		}

		if budget == nil {
			client, err := deps.NewLLM()
			if err != nil {
				return err
			}
			limit := cfg.LLM.TokenBudget()
			if deps.MaxTokens > 0 {
				limit = min(limit, deps.MaxTokens)
			}
			client = llm.WithRedaction(client, deps.Secrets...)
			budget = llm.WithBudget(client, limit, newPricing(cfg), logLine)
			projectContext = loadProjectContext(deps.ProjectRoot, cfg, units)
			projectContext.ChangedFiles = changedPaths(files)
		}

		fmt.Printf("Reviewing %d units with LLM (%s)...\n", len(toReview), mode.Name())
		before := budget.Totals()
		results := ReviewUnits(ctx, budget, mode, toReview, projectContext)
		outcome := settleFindings(ctx, budget, mode, results, index)
		run := newRun(mode, toReview, results, outcome, budget.Totals().Sub(before), projectContext)
		run.Carried = carried
		run.AlreadyReviewed = alreadyReviewed
		run.Feedback = feedback[mode.Name()]
		run.Notes = append(slices.Clone(changeNotes), run.Notes...)

		fmt.Printf("Found %d issues (out of %d units reviewed)\n", len(run.Published), len(run.Reviewed))
		if n := len(run.Skipped); n > 0 {
			fmt.Printf("Warning: %d units were not reviewed, the token budget was reached\n", n)
		}
		fmt.Printf("Tokens used by %s: %d in, %d out, %d read from cache, %d written to cache\n", mode.Name(),
			run.Usage.Input, run.Usage.Output, run.Usage.CacheRead, run.Usage.CacheWrite)

		if err := publishRun(ctx, deps.Provider, opts, self, mode, run); err != nil {
			fmt.Printf("Warning: mode %s: %v\n", mode.Name(), err)
			errs = append(errs, fmt.Errorf("mode %s: %w", mode.Name(), err))
		}
		if err := out.add(ctx, mode.PublishPolicy(), run); err != nil {
			errs = append(errs, fmt.Errorf("mode %s: %w", mode.Name(), err))
		}
	}

	if budget != nil {
		total := budget.Totals()
		fmt.Printf("Total for the run: %d tokens in, %d out, %d read from cache, %d written to cache, estimated cost %s\n",
			total.Usage.Input, total.Usage.Output, total.Usage.CacheRead, total.Usage.CacheWrite,
			publish.FormatCost(total.Cost, total.UnpricedCalls > 0))
	}

	if err := out.write(); err != nil {
		fmt.Printf("Warning: %v\n", err)
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// skippedHashes returns the candidates that a previous run already reviewed, by hash, with the
// line where each starts now
func skippedHashes(mode modes.Mode, digest string, candidates, toReview []collect.ReviewUnit) map[string]int {
	pending := make(map[string]bool, len(toReview))
	for _, u := range toReview {
		pending[state.UnitHash(mode, u, digest)] = true
	}
	hashes := make(map[string]int)
	for _, u := range candidates {
		if h := state.UnitHash(mode, u, digest); !pending[h] {
			hashes[h] = u.StartLine
		}
	}
	return hashes
}

// newRun gathers what a mode produced and consumed, to publish it
func newRun(mode modes.Mode, units []collect.ReviewUnit, results []UnitResult, outcome outcome, used llm.Totals, pc *collect.ProjectContext) publish.Run {
	run := publish.Run{
		Mode:           mode.Name(),
		Scope:          mode.Scope().String(),
		Version:        mode.Version(),
		Units:          units,
		Published:      outcome.published,
		Overflow:       outcome.overflow,
		Unjudged:       outcome.unjudged,
		Repeated:       outcome.repeated,
		Verdicts:       outcome.verdicts,
		Discarded:      outcome.discarded,
		FPModel:        outcome.fpModel,
		Usage:          used.Usage,
		Cost:           used.Cost,
		CostIncomplete: used.UnpricedCalls > 0,
	}
	if pc != nil {
		run.Notes = append(run.Notes, pc.Notes...)
	}
	run.Notes = append(run.Notes, outcome.notes...)
	for _, u := range units {
		for _, note := range u.Notes {
			if !slices.Contains(run.Notes, note) {
				run.Notes = append(run.Notes, note)
			}
		}
	}

	for _, r := range results {
		if run.Model == "" {
			run.Model = r.Model
		}

		label := fmt.Sprintf("%s (%s)", r.Unit.Name, r.Unit.FilePath)
		status := publish.StatusReviewed
		switch {
		case r.Skipped:
			status = publish.StatusSkipped
			run.Skipped = append(run.Skipped, label)
		case r.Err != nil:
			status = publish.StatusFailed
			run.Failed = append(run.Failed, label)
		default:
			run.Reviewed = append(run.Reviewed, r.Hash)
		}
		run.Statuses = append(run.Statuses, publish.UnitStatus{Hash: r.Hash, Status: status})
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

// loadProjectContext never returns nil: a context that could not be read is an empty one
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
		return &collect.ProjectContext{}
	}

	if pc.Description != "" {
		fmt.Printf("Project context loaded (%d bytes)\n", len(pc.Description))
	}
	for _, note := range pc.Notes {
		fmt.Printf("Warning: %s\n", note)
	}
	return pc
}

// defaultFPModel judges the findings on the Anthropic backend, as a stronger model than the one
// that produced them. Other backends have their own model names: they keep their default.
const defaultFPModel = "claude-opus-5-5"

// fpInputFactor bounds what the false positive filter receives to this many times the findings
// a mode may publish, so that it can still discard some and be replaced by others
const fpInputFactor = 3

// outcome is what remains of the findings of a mode once filtered
type outcome struct {
	published, overflow []findings.Finding
	// unjudged were cut before the false positive filter
	unjudged []findings.Finding
	// repeated are the findings of a previous run that are still true
	repeated  []findings.Finding
	verdicts  []findings.Verdict
	discarded int
	fpModel   string
	notes     []string
}

// settleFindings applies the policy of a mode to what its units reported: thresholds, findings
// already reported by a previous run, false positive filter, then the cap on what is published
func settleFindings(ctx context.Context, client llm.Client, mode modes.Mode, results []UnitResult, index *state.Index) outcome {
	var all []findings.Finding
	units := make(map[string]collect.ReviewUnit)
	for _, r := range results {
		if r.Err != nil {
			continue
		}
		all = append(all, r.Findings...)
		units[r.Hash] = r.Unit
	}

	policy := mode.FilterPolicy()
	all = findings.Threshold(all, policy)

	var seen map[string]bool
	var previous []findings.Reported
	if index != nil {
		seen, previous = index.ReportedFindings(mode.Name())
	}
	all, repeated := findings.Dedup(all, seen)

	// Only the most severe findings are worth a call to the strongest model: the others could
	// not be published anyway
	var unjudged []findings.Finding
	if policy.MaxPublished > 0 {
		all, unjudged = findings.Cap(all, fpInputFactor*policy.MaxPublished)
	}

	var out outcome
	if fp := policy.FalsePositive; fp != nil && len(all) > 0 {
		cfg := *fp
		if cfg.Model == "" && client.Backend() == "anthropic" {
			cfg.Model = defaultFPModel
		}

		kept, verdicts, err := findings.FilterFalsePositives(ctx, client, cfg, units, previous, all)
		if err != nil {
			fmt.Printf("Warning: %v, the findings of %s are published unfiltered\n", err, mode.Name())
			out.notes = append(out.notes, fmt.Sprintf("The false positive filter failed, the findings were not filtered: %v", err))
		} else {
			for _, v := range verdicts {
				if !v.Keep {
					fmt.Printf("Discarded as a false positive: %s (%s)\n", oneLine(all[v.ID].Title), oneLine(v.Reason))
				}
			}
			out.verdicts = verdicts
			out.discarded = len(all) - len(kept)
			out.fpModel = cfg.Model
			all = kept
		}
	}

	out.published = all
	if policy.MaxPublished > 0 {
		out.published, out.overflow = findings.Cap(all, policy.MaxPublished)
	}
	out.unjudged = unjudged
	out.repeated = repeated
	return out
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// collectRequest asks for the widest scope and the extra units that the enabled modes need
func collectRequest(files []provider.ModifiedFile, enabled []modes.Mode) collect.Request {
	req := collect.Request{Files: files, Scope: maxScope(enabled)}
	for _, m := range enabled {
		needs := m.Needs()
		req.MatchDeleted = req.MatchDeleted || needs.MatchDeleted
		h := &req.Hunks
		for _, ext := range needs.Hunks.Extensions {
			if !slices.Contains(h.Extensions, ext) {
				h.Extensions = append(h.Extensions, ext)
			}
		}
		h.Orphans = h.Orphans || needs.Hunks.Orphans
		h.Before = max(h.Before, needs.Hunks.Before)
		h.After = max(h.After, needs.Hunks.After)
		req.Related = mergeRelated(req.Related, needs.Related)
	}
	return req
}

// unitsFor keeps the units a mode reviews, with the context its scope allows
func unitsFor(m modes.Mode, units []collect.ReviewUnit) []collect.ReviewUnit {
	var result []collect.ReviewUnit
	for _, u := range units {
		if !m.Accepts(u) {
			continue
		}
		if m.Scope() < collect.ScopeFunctionClass {
			u.Enclosing = nil
		}
		if m.Scope() < collect.ScopeFunctionClassRelated {
			u.Related = nil
		}
		result = append(result, u)
	}
	return result
}

// changedConfig returns the configuration file the change modifies, empty when it leaves it alone
func changedConfig(files []provider.ModifiedFile) string {
	for _, f := range files {
		for _, p := range []string{f.Path, f.OldPath} {
			if config.IsConfigFile(p) {
				return p
			}
		}
	}
	return ""
}

// stopped is called when a run ends before reviewing anything. If the change modifies the
// configuration file, or if the caller has notes to give, the author may have caused it (a
// pattern that ignores the change, a mode turned off, code under a vendor directory), and the
// participants must be able to see it: a notice is posted on the PR/MR. Failing to post it never
// fails the run. The files are fetched when the caller has none.
func stopped(ctx context.Context, deps Deps, opts Options, files []provider.ModifiedFile, reason string, notes ...string) error {
	if files == nil {
		var err error
		if files, err = deps.Provider.GetModifiedFiles(ctx, opts.Number); err != nil {
			fmt.Printf("Warning: could not check whether the change modifies the configuration: %v\n", err)
			return nil
		}
	}
	kept, _ := dropUnsafePaths(files)
	name := changedConfig(kept)
	if name == "" && len(notes) == 0 {
		return nil
	}

	fmt.Printf("Warning: the review stopped (%s) and the change may be the cause\n", reason)
	if opts.DryRun {
		return nil
	}
	self, err := deps.Provider.CurrentUser(ctx)
	if err == nil {
		err = publish.PostNotice(ctx, deps.Provider, opts.Number, self, name, reason, notes)
	}
	if err != nil {
		fmt.Printf("Warning: %v\n", err)
	}
	return nil
}

// dropUnsafePaths removes the files whose name holds a control character, and counts them:
// printed to the log of the job it would forge lines or workflow commands
func dropUnsafePaths(files []provider.ModifiedFile) ([]provider.ModifiedFile, int) {
	kept := files[:0:0]
	for _, f := range files {
		if strings.ContainsFunc(f.Path+f.OldPath, unicode.IsControl) {
			fmt.Printf("Warning: skipping %q, its name holds control characters\n", f.Path)
			continue
		}
		kept = append(kept, f)
	}
	return kept, len(files) - len(kept)
}

// maxHiddenListed caps the paths named in the note about vendored files
const maxHiddenListed = 20

// hiddenNote lists the files that are never reviewed because they sit below a vendor or
// node_modules directory that is not at the root of the project. Nothing in the configuration
// can bring them back, so the change could hide code there: the participants must be told.
func hiddenNote(files []provider.ModifiedFile) string {
	var paths []string
	for _, f := range files {
		if f.Status != "deleted" && config.IsNestedVendoredPath(f.Path) {
			paths = append(paths, f.Path)
		}
	}
	if len(paths) == 0 {
		return ""
	}
	listed := paths[:min(len(paths), maxHiddenListed)]
	note := "Not reviewed, below a nested vendor or node_modules directory: " + strings.Join(listed, ", ")
	if more := len(paths) - len(listed); more > 0 {
		note += fmt.Sprintf(" (and %d more)", more)
	}
	return note
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func changedPaths(files []provider.ModifiedFile) []string {
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	return paths
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
