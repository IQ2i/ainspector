package pipeline

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/config"
	"github.com/iq2i/ainspector/internal/findings"
	"github.com/iq2i/ainspector/internal/llm"
	"github.com/iq2i/ainspector/internal/modes"
	"github.com/iq2i/ainspector/internal/parser"
	"github.com/iq2i/ainspector/internal/provider"
	"github.com/iq2i/ainspector/internal/publish"
	"github.com/iq2i/ainspector/internal/state"
	"github.com/iq2i/ainspector/internal/text"
)

// Deps are the collaborators of a run
type Deps struct {
	Provider    provider.Provider
	NewLLM      func() (llm.Client, error)
	Config      *config.Config
	ProjectRoot string
	// MaxTokens is the token cap set by the environment of the job, zero selects the default cap.
	// The configuration comes from the checkout of the change and its author could raise its own.
	MaxTokens int
	// DefaultFPModel judges the findings when the configuration names no model: a stronger model
	// than the one that produced them. Empty keeps the default model of the backend.
	DefaultFPModel string
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

// runState is what a run knows once the change is collected, shared by every mode
type runState struct {
	deps        Deps
	opts        Options
	enabled     []modes.Mode
	files       []provider.ModifiedFile
	units       []collect.ReviewUnit
	changeNotes []string
	// self is the login of the token owner, empty when it is unknown
	self     string
	index    *state.Index
	feedback map[string][]publish.Feedback
	out      *reports
	errs     []error

	// Created on first use: nothing is needed when every unit was already reviewed
	budget         *llm.Budget
	projectContext *collect.ProjectContext
}

// Run reviews a PR/MR with each requested mode
func Run(ctx context.Context, deps Deps, opts Options) error {
	st, err := prepare(ctx, deps, opts)
	if err != nil || st == nil {
		return err
	}

	for _, mode := range st.enabled {
		run, err := st.reviewMode(ctx, mode)
		if err != nil {
			return err
		}
		st.out.add(run)
	}

	if st.budget != nil {
		total := st.budget.Totals()
		fmt.Printf("Total for the run: %d tokens in, %d out, %d read from cache, %d written to cache, estimated cost %s\n",
			total.Usage.Input, total.Usage.Output, total.Usage.CacheRead, total.Usage.CacheWrite,
			publish.FormatCost(total.Cost, total.UnpricedCalls > 0))
	}

	if err := st.out.write(); err != nil {
		fmt.Printf("Warning: %v\n", err)
		st.errs = append(st.errs, err)
	}
	return errors.Join(st.errs...)
}

// prepare collects the units to review and what the run needs to tell them apart. It returns a
// nil state when the run ends there: nothing to review, or a change that is ignored.
func prepare(ctx context.Context, deps Deps, opts Options) (*runState, error) {
	cfg := deps.Config

	names := opts.Modes
	if len(names) == 0 {
		names = cfg.EnabledModes()
	}
	enabled, err := modes.Enabled(names, cfg)
	if err != nil {
		return nil, err
	}
	if len(enabled) == 0 {
		fmt.Println("No review mode enabled")
		return nil, stopped(ctx, deps, opts, nil, "no review mode is enabled")
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
			return nil, fmt.Errorf("failed to get change info: %w", err)
		}
		if ignored, reason := cfg.ShouldIgnoreChange(info.Author, info.Title, info.Labels); ignored {
			fmt.Printf("Skipping review: %s\n", reason)
			return nil, stopped(ctx, deps, opts, nil, reason)
		}
	}

	reportDir := deps.ReportDir
	if reportDir == "" {
		reportDir = deps.ProjectRoot
	}
	st := &runState{
		deps:    deps,
		opts:    opts,
		enabled: enabled,
		out:     newReports(reportDir, cfg, publish.Change{Number: opts.Number, CheckoutSHA: opts.CheckoutSHA}),
	}

	fmt.Printf("Fetching modified files...\n")
	files, err := deps.Provider.GetModifiedFiles(ctx, opts.Number)
	if err != nil {
		return nil, fmt.Errorf("failed to get modified files: %w", err)
	}

	files, unsafe := dropUnsafePaths(files)
	st.files = files
	if name := changedConfig(files); name != "" {
		note := fmt.Sprintf("This change modifies %s: the settings of its new version apply to this review", name)
		fmt.Printf("Warning: %s\n", note)
		st.changeNotes = append(st.changeNotes, note)
	}
	if unsafe > 0 {
		st.changeNotes = append(st.changeNotes, fmt.Sprintf("%d %s skipped, the name holds control characters", unsafe, text.Plural(unsafe, "file")))
	}
	req := collectRequest(files, enabled)
	hidden, counted := hiddenNotes(files, req.Hunks.Extensions, cfg.ShouldIgnore)
	if info, err := changeInfo(); err != nil {
		fmt.Printf("Warning: could not check that the platform lists every changed file: %v\n", err)
	} else if info.ChangedFiles > len(files)+unsafe {
		hidden = append(hidden, fmt.Sprintf("The platform lists %d of the %d changed files, the others are never reviewed", len(files)+unsafe, info.ChangedFiles))
	}
	for _, note := range hidden {
		fmt.Printf("Warning: %s\n", note)
	}
	st.changeNotes = append(st.changeNotes, hidden...)
	if counted != "" {
		st.changeNotes = append(st.changeNotes, counted)
	}
	fmt.Printf("Found %d modified files\n", len(files))

	collector := collect.New(deps.Provider, cfg)
	defer collector.Close()
	st.units, err = collector.Collect(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to extract functions: %w", err)
	}

	fmt.Printf("Extracted %d modified units\n", len(st.units))

	if len(st.units) == 0 {
		fmt.Println("No code to review")
		return nil, errors.Join(st.out.write(), stopped(ctx, deps, opts, files, "no file of the change is left to review", hidden...))
	}

	if len(req.Related.Presets) > 0 {
		if err := attachRelated(ctx, deps, opts, st.units, req.Related, changeInfo); err != nil {
			return nil, err
		}
	}

	st.self, err = deps.Provider.CurrentUser(ctx)
	if err != nil {
		fmt.Printf("Warning: could not identify the token owner: %v\n", err)
		st.self = ""
	}

	if opts.Force {
		fmt.Println("Force flag set - reviewing all modified units")
	} else {
		fmt.Println("Checking for previously reviewed units...")
		existing, err := deps.Provider.ListComments(ctx, opts.Number)
		if err != nil {
			fmt.Printf("Warning: could not fetch existing comments: %v\n", err)
		} else {
			if st.self == "" {
				fmt.Println("Warning: trusting every existing comment")
			}
			st.index = state.LoadIndex(existing, st.self)
			st.feedback = collectFeedback(ctx, deps.Provider, opts.Number, existing, st.self)
		}
	}
	return st, nil
}

// reviewMode runs one mode over the units it accepts and publishes the outcome. Only a failure
// to reach the LLM is returned: publishing errors are kept for the end of the run.
func (st *runState) reviewMode(ctx context.Context, mode modes.Mode) (publish.Run, error) {
	cfg := st.deps.Config

	candidates := unitsFor(mode, st.units)
	toReview := candidates
	var carried []findings.Reported
	if st.index != nil {
		var skipped map[string]int
		toReview, skipped = st.index.Partition(mode, state.RulesDigest(mode.Rules()), candidates)
		if n := len(candidates) - len(toReview); n > 0 {
			fmt.Printf("Skipped %d already reviewed units\n", n)
		}
		carried = st.index.OpenFindings(mode, skipped)
	}
	alreadyReviewed := len(candidates) - len(toReview)

	if len(toReview) == 0 {
		fmt.Printf("Nothing left to review for %s\n", mode.Name())
		return publish.Run{Mode: mode.Name(), Scope: mode.Scope().String(), Version: mode.Version(), Carried: carried, AlreadyReviewed: alreadyReviewed, Notes: slices.Clone(st.changeNotes), Feedback: st.feedback[mode.Name()]}, nil
	}

	if st.budget == nil {
		client, err := st.deps.NewLLM()
		if err != nil {
			return publish.Run{}, err
		}
		client = llm.WithRedaction(client, st.deps.Secrets...)
		st.budget = llm.WithBudget(client, cfg.LLM.TokenBudget(st.deps.MaxTokens), newPricing(cfg), logLine)
		st.projectContext = loadProjectContext(st.deps.ProjectRoot, cfg)
		st.projectContext.ChangedFiles = changedPaths(st.files)
	}

	fmt.Printf("Reviewing %d units with LLM (%s)...\n", len(toReview), mode.Name())
	before := st.budget.Totals()
	results := ReviewUnits(ctx, st.budget, mode, toReview, st.projectContext)
	run := publish.Run{Mode: mode.Name(), Scope: mode.Scope().String(), Version: mode.Version(), Units: toReview}
	settleFindings(ctx, st.budget, st.deps.DefaultFPModel, mode, results, st.index, &run)
	completeRun(&run, results, st.budget.Totals().Sub(before), st.projectContext)
	run.Carried = carried
	run.AlreadyReviewed = alreadyReviewed
	run.Feedback = st.feedback[mode.Name()]
	run.Notes = append(slices.Clone(st.changeNotes), run.Notes...)

	fmt.Printf("Found %d issues (out of %d units reviewed)\n", len(run.Published), len(run.Reviewed))
	if n := len(run.Skipped); n > 0 {
		fmt.Printf("Warning: %d units were not reviewed, the token budget was reached\n", n)
	}
	fmt.Printf("Tokens used by %s: %d in, %d out, %d read from cache, %d written to cache\n", mode.Name(),
		run.Usage.Input, run.Usage.Output, run.Usage.CacheRead, run.Usage.CacheWrite)

	if err := publishRun(ctx, st.deps.Provider, st.opts, st.self, mode, run); err != nil {
		fmt.Printf("Warning: mode %s: %v\n", mode.Name(), err)
		st.errs = append(st.errs, fmt.Errorf("mode %s: %w", mode.Name(), err))
	}
	return run, nil
}

// completeRun adds to a run what the units cost and how each one ended. The notes come in this
// order: those of the project context, those of the findings, then those of the units.
func completeRun(run *publish.Run, results []UnitResult, used llm.Totals, pc *collect.ProjectContext) {
	run.Usage = used.Usage
	run.Cost = used.Cost
	run.CostIncomplete = used.UnpricedCalls > 0
	if pc != nil {
		run.Notes = append(slices.Clone(pc.Notes), run.Notes...)
	}
	for _, u := range run.Units {
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
}

func publishRun(ctx context.Context, p provider.Provider, opts Options, self string, mode modes.Mode, run publish.Run) error {
	if opts.DryRun {
		for _, f := range run.Published {
			fmt.Printf("Dry run: would comment on %q:%d\n%s\n", f.File, f.StartLine, publish.LogText(publish.BodyFor(f)))
		}
		fmt.Printf("Dry run: would publish the summary:\n%s\n", publish.LogText(publish.SummaryBody(run, nil)))
		return nil
	}

	var errs []error

	if len(run.Published) > 0 {
		fmt.Println("Posting review with inline suggestions...")
		if err := publish.Inline(ctx, p, opts.Number, run); err != nil {
			errs = append(errs, fmt.Errorf("failed to create review: %w", err))
		} else {
			fmt.Println("Review posted successfully!")
		}
	}

	// The summary carries the state of later runs: without the identity of the token owner
	// it could not tell its own comment from a forged one. Not worth failing the job over.
	if self == "" {
		fmt.Printf("Warning: summary of %s skipped, the token owner is unknown\n", mode.Name())
	} else if err := publish.Summary(ctx, p, opts.Number, self, run); err != nil {
		errs = append(errs, fmt.Errorf("failed to publish the summary: %w", err))
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
func loadProjectContext(root string, cfg *config.Config) *collect.ProjectContext {
	pc, err := collect.GenerateProjectContext(root, &cfg.Context)
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

// fpInputFactor bounds what the false positive filter receives to this many times the findings
// a mode may publish, so that it can still discard some and be replaced by others
const fpInputFactor = 3

// settleFindings applies the policy of a mode to what its units reported and records the result
// in run: thresholds, findings already reported by a previous run, false positive filter, then
// the cap on what is published
func settleFindings(ctx context.Context, client llm.Client, defaultFPModel string, mode modes.Mode, results []UnitResult, index *state.Index, run *publish.Run) {
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
	all, run.Repeated = findings.Dedup(all, seen)

	// Only the most severe findings are worth a call to the strongest model: the others could
	// not be published anyway
	if policy.MaxPublished > 0 {
		all, run.Unjudged = findings.Cap(all, fpInputFactor*policy.MaxPublished)
	}

	if fp := policy.FalsePositive; fp != nil && len(all) > 0 {
		cfg := *fp
		if cfg.Model == "" {
			cfg.Model = defaultFPModel
		}

		kept, verdicts, err := findings.FilterFalsePositives(ctx, client, cfg, units, previous, all)
		if err != nil {
			fmt.Printf("Warning: %v, the findings of %s are published unfiltered\n", err, mode.Name())
			run.Notes = append(run.Notes, fmt.Sprintf("The false positive filter failed, the findings were not filtered: %v", err))
		} else {
			for _, v := range verdicts {
				if !v.Keep {
					fmt.Printf("Discarded as a false positive: %s (%s)\n", text.OneLine(all[v.ID].Title), text.OneLine(v.Reason))
				}
			}
			run.Verdicts = verdicts
			run.Discarded = len(all) - len(kept)
			run.FPModel = cfg.Model
			all = kept
		}
	}

	run.Published = all
	if policy.MaxPublished > 0 {
		run.Published, run.Overflow = findings.Cap(all, policy.MaxPublished)
	}
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

// hiddenNotes tells which files of the change escape the review without any setting to bring
// them back, as the change could hide code there: the files below a vendor or node_modules
// directory that is not at the root of the project, the files of a reviewable kind the platform
// sent without a patch, and the vendored or generated files at the root, which are only counted.
func hiddenNotes(files []provider.ModifiedFile, hunkExtensions []string, ignored func(string) bool) (notes []string, counted string) {
	var nested, noPatch []string
	rooted := 0
	for _, f := range files {
		if f.Status == "deleted" {
			continue
		}
		switch {
		case config.IsNestedVendoredPath(f.Path):
			nested = append(nested, f.Path)
		case config.IsGeneratedPath(f.Path) || config.IsVendoredPath(f.Path):
			rooted++
		case f.NoPatch && reviewable(f.Path, hunkExtensions) && !ignored(f.Path):
			noPatch = append(noPatch, f.Path)
		}
	}

	if len(nested) > 0 {
		notes = append(notes, "Not reviewed, below a nested vendor or node_modules directory: "+listPaths(nested))
	}
	if len(noPatch) > 0 {
		notes = append(notes, "Not reviewed, the platform sent no diff for them (too large, collapsed or empty): "+listPaths(noPatch))
	}
	if rooted > 0 {
		verb := "is"
		if rooted > 1 {
			verb = "are"
		}
		counted = fmt.Sprintf("%d %s %s never reviewed (vendor, generated)", rooted, text.Plural(rooted, "file"), verb)
	}
	return notes, counted
}

// reviewable tells whether a file would have been reviewed had its patch been given
func reviewable(path string, hunkExtensions []string) bool {
	return parser.IsSupported(path) || slices.Contains(hunkExtensions, strings.TrimPrefix(filepath.Ext(path), "."))
}

func listPaths(paths []string) string {
	listed := paths[:min(len(paths), maxHiddenListed)]
	list := strings.Join(listed, ", ")
	if more := len(paths) - len(listed); more > 0 {
		list += fmt.Sprintf(" (and %d more)", more)
	}
	return list
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
