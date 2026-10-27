package publish

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/findings"
	"github.com/iq2i/ainspector/internal/llm"
	"github.com/iq2i/ainspector/internal/state"
)

func openFinding(file string, line int, sev findings.Severity, title, fp string) findings.Finding {
	return findings.Finding{Mode: "security", File: file, StartLine: line, EndLine: line + 2, Severity: sev, Category: "access-control", Title: title, Fingerprint: fp, UnitHash: "u-" + fp}
}

func withSuggestion(f findings.Finding, s string) findings.Finding {
	f.Suggestion = s
	return f
}

func readQuality(t *testing.T, dir, name string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	var entries []map[string]any
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("invalid report %s: %v", data, err)
	}
	return entries
}

func TestCodeQuality_EmptyReportIsAnArray(t *testing.T) {
	dir := t.TempDir()
	if err := NewCodeQuality(dir, "gl-code-quality-report.json").Write(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "gl-code-quality-report.json"))
	if strings.TrimSpace(string(data)) != "[]" {
		t.Errorf("expected an empty array, got %q", data)
	}
}

func TestCodeQuality_EntriesAndSeverities(t *testing.T) {
	dir := t.TempDir()
	r := NewCodeQuality(dir, "report.json")
	run := Run{
		Mode: "security",
		Published: []findings.Finding{
			openFinding("./src/A.php", 10, findings.SeverityCritical, "IDOR\non show", "aaaaaaaaaaaa"),
			openFinding("src/B.php", 4, findings.SeverityHigh, "t", "bbbbbbbbbbbb"),
			openFinding("src/C.php", 4, findings.SeverityMedium, "t", "cccccccccccc"),
			openFinding("src/D.php", 4, findings.SeverityLow, "t", "dddddddddddd"),
			openFinding("src/E.php", 4, findings.SeverityInfo, "t", "eeeeeeeeeeee"),
		},
	}
	if err := r.Publish(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if err := r.Write(); err != nil {
		t.Fatal(err)
	}

	got := readQuality(t, dir, "report.json")
	if len(got) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(got))
	}
	var sev []string
	for _, e := range got {
		sev = append(sev, e["severity"].(string))
	}
	if strings.Join(sev, ",") != "blocker,critical,major,minor,info" {
		t.Errorf("unexpected severities %v", sev)
	}

	first := got[0]
	loc := first["location"].(map[string]any)
	lines := loc["lines"].(map[string]any)
	if first["description"] != "IDOR on show" || first["check_name"] != "ainspector/security/access-control" || first["fingerprint"] != "aaaaaaaaaaaa" {
		t.Errorf("unexpected entry %v", first)
	}
	if loc["path"] != "src/A.php" || lines["begin"] != float64(10) || lines["end"] != float64(12) {
		t.Errorf("unexpected location %v", loc)
	}
	if cats := first["categories"].([]any); len(cats) != 1 || cats[0] != "Security" {
		t.Errorf("unexpected categories %v", first["categories"])
	}
}

func TestCodeQuality_QualityFindingsAreMinor(t *testing.T) {
	dir := t.TempDir()
	r := NewCodeQuality(dir, "report.json")
	f := finding("a.go", 3, "Nil pointer when the map is empty", "u1", "")
	if err := r.Publish(context.Background(), Run{Mode: "quality", Published: []findings.Finding{f}}); err != nil {
		t.Fatal(err)
	}
	_ = r.Write()

	got := readQuality(t, dir, "report.json")
	if len(got) != 1 || got[0]["severity"] != "minor" || got[0]["check_name"] != "ainspector/quality" || got[0]["fingerprint"] != findings.CodeFingerprint(f, 0, "") {
		t.Errorf("unexpected report %v", got)
	}
	if _, ok := got[0]["categories"]; ok {
		t.Error("a quality finding is not a security one")
	}
	if got[0]["description"] != "Nil pointer when the map is empty" {
		t.Errorf("a finding without title uses its explanation, got %v", got[0]["description"])
	}
	lines := got[0]["location"].(map[string]any)["lines"].(map[string]any)
	if _, ok := lines["end"]; ok {
		t.Errorf("a one line finding has no end, got %v", lines)
	}
}

func TestCodeQuality_GathersEveryKindOfOpenFinding(t *testing.T) {
	dir := t.TempDir()
	r := NewCodeQuality(dir, "report.json")
	run := Run{
		Mode:      "security",
		Published: []findings.Finding{openFinding("a.php", 1, findings.SeverityHigh, "published", "aaaaaaaaaaaa")},
		Overflow:  []findings.Finding{openFinding("b.php", 1, findings.SeverityLow, "overflow", "bbbbbbbbbbbb")},
		Unjudged:  []findings.Finding{openFinding("e.php", 1, findings.SeverityLow, "unjudged", "eeeeeeeeeeee")},
		Repeated: []findings.Finding{
			openFinding("c.php", 1, findings.SeverityLow, "repeated", "cccccccccccc"),
			openFinding("a.php", 1, findings.SeverityHigh, "published", "aaaaaaaaaaaa"),
		},
		Carried: []findings.Reported{{Fingerprint: "dddddddddddd", File: "d.php", Line: 5, Category: "idor", Title: "carried", Severity: findings.SeverityMedium}},
	}
	_ = r.Publish(context.Background(), run)
	_ = r.Write()

	got := readQuality(t, dir, "report.json")
	var titles []string
	for _, e := range got {
		titles = append(titles, e["description"].(string))
	}
	if strings.Join(titles, ",") != "published,overflow,repeated,carried" {
		t.Errorf("unexpected entries %v: unjudged findings stay out, fingerprints must be unique", titles)
	}
}

func TestCodeQuality_DescriptionIsBounded(t *testing.T) {
	dir := t.TempDir()
	r := NewCodeQuality(dir, "report.json")
	_ = r.Publish(context.Background(), Run{Mode: "security", Published: []findings.Finding{openFinding("a.php", 0, findings.SeverityLow, strings.Repeat("é", 2000), "aaaaaaaaaaaa")}})
	_ = r.Write()

	e := readQuality(t, dir, "report.json")[0]
	if n := len([]rune(e["description"].(string))); n != maxQualityDescription {
		t.Errorf("expected %d runes, got %d", maxQualityDescription, n)
	}
	if e["location"].(map[string]any)["lines"].(map[string]any)["begin"] != float64(1) {
		t.Error("a line below 1 is not valid for GitLab")
	}
}

func TestCodeQuality_RefusesPathsOutsideTheDirectory(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{"../report.json", "/tmp/report.json", "a/../../report.json"} {
		if err := NewCodeQuality(dir, p).Write(); err == nil {
			t.Errorf("%q should be refused", p)
		}
	}
}

func TestArtifact_HoldsNoCode(t *testing.T) {
	dir := t.TempDir()
	a := NewArtifact(dir, "ainspector-report.json", Change{Number: 7, CheckoutSHA: "abc"})
	run := Run{
		Mode: "security", Scope: "function+class+related", Version: "security/2", Model: "m",
		Units: []collect.ReviewUnit{{
			Kind: collect.KindFunction, FilePath: "src/A.php", Name: "show", StartLine: 3, EndLine: 9, ChangeType: "modified",
			Content: "SECRET-CODE", Diff: "SECRET-DIFF",
			Enclosing: &collect.ClassHeader{Name: "A", Rendered: "SECRET-HEADER"},
			Related:   []collect.RelatedFile{{Path: "src/Security/AVoter.php", Reason: "voter", Content: "SECRET-RELATED", Truncated: true}},
			Notes:     []string{"header truncated"},
		}},
		Statuses:        []UnitStatus{{Hash: "h1", Status: StatusReviewed}},
		AlreadyReviewed: 2,
		Published:       []findings.Finding{withSuggestion(openFinding("src/A.php", 3, findings.SeverityHigh, "t", "aaaaaaaaaaaa"), "SECRET-SUGGESTION")},
		Unjudged:        []findings.Finding{openFinding("src/B.php", 1, findings.SeverityLow, "u", "bbbbbbbbbbbb")},
		Verdicts:        []findings.Verdict{{ID: 0, Keep: true, Reason: "ok"}},
		Usage:           llm.Usage{Input: 10, Output: 5, CacheRead: 3, CacheWrite: 1},
		Cost:            0.5,
	}
	if err := a.Publish(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	if err := a.Write(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "ainspector-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"SECRET-CODE", "SECRET-DIFF", "SECRET-HEADER", "SECRET-RELATED", "SECRET-SUGGESTION"} {
		if strings.Contains(string(data), leak) {
			t.Errorf("the artifact must not hold %s", leak)
		}
	}

	var doc struct {
		Version int `json:"version"`
		Change  Change
		Modes   []struct {
			Mode  string
			Units []struct {
				Hash, Status, Enclosing string
				Related                 []artifactRelated
			}
			AlreadyReviewed int `json:"already_reviewed"`
			Published       []findings.Finding
			Unjudged        []findings.Finding
			Usage           artifactUsage
			Cost            float64
			Carried         []findings.Reported
			Skipped         []string
		}
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Version != 1 || doc.Change.Number != 7 || len(doc.Modes) != 1 {
		t.Fatalf("unexpected document %s", data)
	}
	m := doc.Modes[0]
	u := m.Units[0]
	if u.Hash != "h1" || u.Status != "reviewed" || u.Enclosing != "A" || len(u.Related) != 1 || !u.Related[0].Truncated || m.AlreadyReviewed != 2 {
		t.Errorf("unexpected unit %+v", m)
	}
	if len(m.Unjudged) != 1 {
		t.Errorf("the artifact keeps the findings the filter never judged, got %+v", m.Unjudged)
	}
	if len(m.Published) != 1 || m.Usage.CacheRead != 3 || m.Cost != 0.5 || m.Carried == nil || m.Skipped == nil {
		t.Errorf("unexpected mode %+v: lists must be arrays, never null", m)
	}
}

func TestArtifact_GathersEveryMode(t *testing.T) {
	dir := t.TempDir()
	a := NewArtifact(dir, "report.json", Change{Number: 1})
	_ = a.Publish(context.Background(), Run{Mode: "quality"})
	_ = a.Publish(context.Background(), Run{Mode: "security"})
	_ = a.Write()

	data, _ := os.ReadFile(filepath.Join(dir, "report.json"))
	var doc struct{ Modes []struct{ Mode string } }
	if err := json.Unmarshal(data, &doc); err != nil || len(doc.Modes) != 2 || doc.Modes[1].Mode != "security" {
		t.Errorf("unexpected document %s (%v)", data, err)
	}
}

func TestArtifact_EmptyRun(t *testing.T) {
	dir := t.TempDir()
	if err := NewArtifact(dir, "report.json", Change{Number: 1}).Write(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "report.json"))
	if !strings.Contains(string(data), `"modes": []`) {
		t.Errorf("expected an empty list of modes, got %s", data)
	}
}

func TestWriteFile(t *testing.T) {
	dir := t.TempDir()

	if err := writeFile(dir, "reports/out/r.json", []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(dir, "reports/out/r.json", []byte("two")); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "reports", "out", "r.json"))
	if string(data) != "two" {
		t.Errorf("expected the file to be replaced, got %q", data)
	}
	if info, _ := os.Stat(filepath.Join(dir, "reports", "out", "r.json")); info.Mode().Perm() != 0o644 {
		t.Errorf("unexpected mode %v", info.Mode())
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "reports", "out"))
	if len(entries) != 1 {
		t.Errorf("no temporary file must remain, got %v", entries)
	}
}

func TestWriteFile_RefusesLinks(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()

	if err := os.Symlink(filepath.Join(outside, "target"), filepath.Join(dir, "report.json")); err != nil {
		t.Skip("symbolic links are not available")
	}
	if err := writeFile(dir, "report.json", []byte("x")); err == nil {
		t.Error("a symbolic link must not be written through")
	}

	if err := os.Symlink(outside, filepath.Join(dir, "out")); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(dir, "out/report.json", []byte("x")); err == nil {
		t.Error("a directory that is a link must not be written through")
	}

	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Errorf("nothing must reach the directory outside, got %v", entries)
	}
}

func TestWriteFile_RefusesADirectoryTarget(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "report.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(dir, "report.json", []byte("x")); err == nil {
		t.Error("a directory is not a report")
	}
}

func TestSummary_StateKeepsTheOpenFindings(t *testing.T) {
	run := Run{
		Mode: "security", Scope: "function+class", Version: "security/1",
		Published: []findings.Finding{openFinding("a.php", 3, findings.SeverityHigh, "published", "aaaaaaaaaaaa")},
		Overflow:  []findings.Finding{openFinding("b.php", 3, findings.SeverityLow, "overflow", "bbbbbbbbbbbb")},
		Repeated:  []findings.Finding{openFinding("c.php", 3, findings.SeverityLow, "repeated", "cccccccccccc")},
		Carried:   []findings.Reported{{Fingerprint: "dddddddddddd", File: "d.php", Unit: "old"}},
	}
	block, ok := state.ParseStateBlock("security", SummaryBody(run, nil))
	if !ok {
		t.Fatal("no state block")
	}
	var got []string
	for _, r := range block.Open {
		got = append(got, r.Fingerprint)
	}
	if strings.Join(got, ",") != "dddddddddddd,aaaaaaaaaaaa,bbbbbbbbbbbb,cccccccccccc" {
		t.Errorf("unexpected open findings %v", got)
	}
	if block.Open[1].Unit != "u-aaaaaaaaaaaa" || block.Open[1].Severity != findings.SeverityHigh || block.Open[1].EndLine != 5 {
		t.Errorf("an open finding keeps what the report needs, got %+v", block.Open[1])
	}
	if len(block.Findings) != 1 {
		t.Errorf("only the published findings are marked as reported, got %v", block.Findings)
	}
}

func TestCodeQuality_QualityFindingFollowsItsMovedUnit(t *testing.T) {
	report := func(start, line int) string {
		dir := t.TempDir()
		r := NewCodeQuality(dir, "report.json")
		f := findings.Finding{Mode: "quality", File: "a.go", StartLine: line, Explanation: "Nil pointer", UnitHash: "u1"}
		run := Run{
			Mode:      "quality",
			Units:     []collect.ReviewUnit{{Name: "load", StartLine: start}},
			Statuses:  []UnitStatus{{Hash: "u1", Status: StatusReviewed}},
			Published: []findings.Finding{f},
		}
		_ = r.Publish(context.Background(), run)
		_ = r.Write()
		return readQuality(t, dir, "report.json")[0]["fingerprint"].(string)
	}
	if report(10, 13) != report(30, 33) {
		t.Error("the fingerprint of a quality finding must survive its function moving down the file")
	}
}

func TestSummary_OpenStateRemembersTheStartOfTheUnit(t *testing.T) {
	f := openFinding("a.php", 12, findings.SeverityHigh, "t", "aaaaaaaaaaaa")
	f.UnitHash = "u1"
	run := Run{
		Mode: "security", Scope: "function", Version: "security/1",
		Units:     []collect.ReviewUnit{{Name: "show", StartLine: 10}},
		Statuses:  []UnitStatus{{Hash: "u1", Status: StatusReviewed}},
		Published: []findings.Finding{f},
		Unjudged:  []findings.Finding{openFinding("b.php", 1, findings.SeverityLow, "never judged", "bbbbbbbbbbbb")},
	}
	body := SummaryBody(run, nil)
	blocks, ok := state.ParseStateBlock("security", body)
	if !ok || len(blocks.Open) != 1 || blocks.Open[0].UnitLine != 10 {
		t.Errorf("expected the published finding only, with its unit line, got %+v", blocks.Open)
	}
	if !strings.Contains(body, "never judged") {
		t.Error("the summary still lists the findings that were not judged")
	}
}
