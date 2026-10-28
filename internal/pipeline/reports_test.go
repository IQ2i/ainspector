package pipeline

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type qualityEntry struct {
	Fingerprint string `json:"fingerprint"`
	Severity    string `json:"severity"`
	Location    struct {
		Lines struct {
			Begin int `json:"begin"`
		} `json:"lines"`
	} `json:"location"`
}

func (h *harness) qualityReport(t *testing.T) []qualityEntry {
	t.Helper()
	data := h.report("gl-code-quality-report.json")
	if data == nil {
		t.Fatal("no Code Quality report was written")
	}
	var entries []qualityEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("invalid report %s: %v", data, err)
	}
	return entries
}

// artifactModes returns the modes of the artifact, by name
func (h *harness) artifactModes(t *testing.T) map[string]map[string]any {
	t.Helper()
	data := h.report("ainspector-report.json")
	if data == nil {
		t.Fatal("no artifact was written")
	}
	var doc struct {
		Modes []map[string]any `json:"modes"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	modes := make(map[string]map[string]any)
	for _, m := range doc.Modes {
		modes[m["mode"].(string)] = m
	}
	return modes
}

// nextRun makes what the previous run posted visible to the next one
func (h *harness) nextRun() {
	h.keepPostedComments()
	h.requests, h.fake.posted = nil, nil
}

func voterCopy(t *testing.T) (*harness, string) {
	t.Helper()
	dir := t.TempDir()
	copyTree(t, filepath.Join("..", "..", "testdata", "mrs", voterFixture), dir)
	return newHarness(t, dir), dir
}

// fixVoter changes a related file of the unit, so that it is reviewed again
func fixVoter(t *testing.T, dir string) {
	t.Helper()
	voter := filepath.Join(dir, "head", "src", "Security", "Voter", "InvoiceVoter.php")
	data, err := os.ReadFile(voter)
	if err != nil {
		t.Fatal(err)
	}
	fixed := strings.Replace(string(data), "$token->getUser() instanceof UserInterface", "$subject->getOwner() === $token->getUser()", 1)
	if err := os.WriteFile(voter, []byte(fixed), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRun_ReportsDescribeTheRun(t *testing.T) {
	h, _ := voterCopy(t)

	if err := Run(context.Background(), h.deps, Options{Number: 7, CheckoutSHA: "abc123"}); err != nil {
		t.Fatal(err)
	}

	report := h.qualityReport(t)
	if len(report) != 1 || report[0].Severity != "critical" || len(report[0].Fingerprint) != 12 {
		t.Errorf("unexpected Code Quality report %+v", report)
	}

	data := h.report("ainspector-report.json")
	for _, leak := range []string{"instanceof UserInterface", "$this->denyAccessUnlessGranted", "password_hashers"} {
		if strings.Contains(string(data), leak) {
			t.Errorf("the artifact must not hold the code it reviewed: %q", leak)
		}
	}
	var doc struct {
		Change struct {
			Number      int    `json:"number"`
			CheckoutSHA string `json:"checkout_sha"`
		} `json:"change"`
	}
	if err := json.Unmarshal(data, &doc); err != nil || doc.Change.Number != 7 || doc.Change.CheckoutSHA != "abc123" {
		t.Errorf("unexpected change %+v (%v)", doc.Change, err)
	}
	sec := h.artifactModes(t)["security"]
	if sec == nil || len(sec["published"].([]any)) != 1 || len(sec["verdicts"].([]any)) != 1 {
		t.Errorf("the artifact holds the findings and the verdicts of the filter, got %v", sec)
	}
}

func TestRun_ReportKeepsTheFindingsOfSkippedUnits(t *testing.T) {
	h, _ := voterCopy(t)

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}
	first := h.qualityReport(t)
	h.nextRun()

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}
	if len(h.requests) != 0 {
		t.Fatalf("nothing changed, got %d requests", len(h.requests))
	}

	second := h.qualityReport(t)
	if len(second) != 1 || second[0] != first[0] {
		t.Errorf("the widget shows the last pipeline only: the finding of the first run must stay, got %+v", second)
	}
	sec := h.artifactModes(t)["security"]
	if sec["already_reviewed"] != float64(1) || len(sec["carried"].([]any)) != 1 || len(sec["units"].([]any)) != 0 {
		t.Errorf("unexpected artifact %v", sec)
	}
}

func TestRun_FindingStillTrueStaysInTheReportWithoutANewComment(t *testing.T) {
	h, dir := voterCopy(t)

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}
	first := h.qualityReport(t)
	h.nextRun()
	fixVoter(t, dir)

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}
	if len(h.requests) == 0 {
		t.Fatal("the unit must be reviewed again")
	}
	if len(h.fake.posted) != 0 {
		t.Errorf("a finding already reported is not commented twice, got %d comments", len(h.fake.posted))
	}
	if got := h.qualityReport(t); len(got) != 1 || got[0].Fingerprint != first[0].Fingerprint {
		t.Errorf("the finding is still true: it must stay in the report, got %+v", got)
	}
	if rep := h.artifactModes(t)["security"]["repeated"].([]any); len(rep) != 1 {
		t.Errorf("the artifact lists the repeated finding, got %v", rep)
	}

	// Once more without change: the unit is skipped, its finding is carried with its new hash
	h.nextRun()
	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}
	if len(h.requests) != 0 {
		t.Fatalf("nothing changed, got %d requests", len(h.requests))
	}
	if got := h.qualityReport(t); len(got) != 1 || got[0].Fingerprint != first[0].Fingerprint {
		t.Errorf("the finding must survive a run that skips its unit, got %+v", got)
	}
}

func TestRun_FixedFindingLeavesTheReport(t *testing.T) {
	h, dir := voterCopy(t)

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}
	h.nextRun()
	fixVoter(t, dir)
	h.responses["show"] = `{"findings": []}`

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}
	if got := h.qualityReport(t); len(got) != 0 {
		t.Errorf("a unit reviewed again replaces what was said of it, got %+v", got)
	}
	if string(h.report("gl-code-quality-report.json")) == "null\n" {
		t.Error("an empty report is an array")
	}

	h.nextRun()
	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}
	if got := h.qualityReport(t); len(got) != 0 {
		t.Errorf("the fixed finding must not come back, got %+v", got)
	}
}

func TestRun_ForceCarriesNothing(t *testing.T) {
	h, _ := voterCopy(t)

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}
	h.nextRun()
	h.responses["show"] = `{"findings": []}`

	if err := Run(context.Background(), h.deps, Options{Number: 1, Force: true}); err != nil {
		t.Fatal(err)
	}
	if got := h.qualityReport(t); len(got) != 0 {
		t.Errorf("a forced run reviews everything again, nothing is carried, got %+v", got)
	}
}

func TestRun_ReportsOfAQualityRun(t *testing.T) {
	h := newHarness(t, filepath.Join("..", "..", "testdata", "mrs", "go-basic"))

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}
	first := h.qualityReport(t)
	if len(first) != 2 || first[0].Severity != "minor" || first[0].Fingerprint == first[1].Fingerprint {
		t.Fatalf("unexpected report %+v", first)
	}
	h.nextRun()

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}
	if len(h.requests) != 0 {
		t.Fatalf("nothing changed, got %d requests", len(h.requests))
	}
	if second := h.qualityReport(t); len(second) != 2 || second[0] != first[0] || second[1] != first[1] {
		t.Errorf("the quality findings are carried as well, got %+v", second)
	}
}

func TestRun_NoCodeToReviewStillWritesEmptyReports(t *testing.T) {
	h := newHarness(t, filepath.Join("..", "..", "testdata", "mrs", "go-basic"))
	h.fake.mr.Files = nil

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(h.report("gl-code-quality-report.json"))); got != "[]" {
		t.Errorf("expected an empty report, got %q", got)
	}
	if len(h.artifactModes(t)) != 0 {
		t.Error("no mode ran")
	}
}

func TestRun_DryRunWritesTheReportsLocally(t *testing.T) {
	h, _ := voterCopy(t)

	if err := Run(context.Background(), h.deps, Options{Number: 1, DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if h.fake.writes != 0 {
		t.Errorf("a dry run writes nothing to the MR, got %d writes", h.fake.writes)
	}
	if got := h.qualityReport(t); len(got) != 1 {
		t.Errorf("the report is a local file, written all the same: got %+v", got)
	}
}

func TestRun_ReportsCanBeDisabledOrMoved(t *testing.T) {
	h, _ := voterCopy(t)
	off, moved := "", "out/cq.json"
	h.deps.Config.Publish.Artifact = &off
	h.deps.Config.Publish.CodeQuality = &moved

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}
	if h.report("ainspector-report.json") != nil || h.report("gl-code-quality-report.json") != nil {
		t.Error("a disabled report is not written, nor the default of a moved one")
	}
	if h.report(moved) == nil {
		t.Errorf("expected the report at %s", moved)
	}
}

func TestRun_ReportPathOutsideTheDirectoryFailsTheRunButNotTheReview(t *testing.T) {
	h, _ := voterCopy(t)
	escape := "../cq.json"
	h.deps.Config.Publish.CodeQuality = &escape

	err := Run(context.Background(), h.deps, Options{Number: 1})
	if err == nil || !strings.Contains(err.Error(), "Code Quality report") {
		t.Errorf("expected the report error, got %v", err)
	}
	if len(h.fake.posted) != 1 || h.report("ainspector-report.json") == nil {
		t.Error("the review and the other report are not lost")
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(h.reportDir), "cq.json")); statErr == nil {
		t.Error("nothing may be written outside the directory")
	}
}
