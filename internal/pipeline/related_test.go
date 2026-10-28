package pipeline

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iq2i/ainspector/internal/collect"
)

const voterFixture = "security-voter-idor"

func voterHarness(t *testing.T) *harness {
	t.Helper()
	return newHarness(t, filepath.Join("..", "..", "testdata", "mrs", voterFixture))
}

// requestTexts returns the last message of every request sent to the LLM
func requestTexts(h *harness) []string {
	var texts []string
	for _, r := range h.result().LLMRequests {
		texts = append(texts, string(r))
	}
	return texts
}

func TestRun_RelatedFilesReachTheReviewAndTheFalsePositiveFilter(t *testing.T) {
	h := voterHarness(t)

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}

	texts := requestTexts(h)
	if len(texts) != 2 {
		t.Fatalf("expected the review and the filter, got %d requests", len(texts))
	}
	for i, text := range texts {
		for _, want := range []string{"src/Security/Voter/InvoiceVoter.php", "instanceof UserInterface", "firewalls and access_control of the security configuration", "Invoice {{ invoice.number }}"} {
			if !strings.Contains(text, want) {
				t.Errorf("request %d lacks %q", i, want)
			}
		}
		if strings.Contains(text, "password_hashers") {
			t.Errorf("request %d holds more of the security configuration than its access rules", i)
		}
	}
	if len(h.fake.posted) != 1 || h.fake.posted[0].Line != 28 || !strings.Contains(h.fake.posted[0].Body, "(idor,") {
		t.Errorf("the IDOR hidden in the voter must be reported, got %+v", h.fake.posted)
	}
}

func TestRun_ClassScopeSendsNoRelatedFiles(t *testing.T) {
	h := voterHarness(t)
	h.deps.Config.Modes.Security.Scope = "function+class"

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}

	if len(h.requests) == 0 {
		t.Fatal("expected a review")
	}
	for _, text := range requestTexts(h) {
		if strings.Contains(text, "Related file ") {
			t.Errorf("no related file expected at this scope:\n%s", text)
		}
	}
}

func TestRun_NoPresetNoRelatedFiles(t *testing.T) {
	h := voterHarness(t)
	h.deps.Config.Modes.Security.Related.Presets = []string{"vue"}

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}
	for _, text := range requestTexts(h) {
		if strings.Contains(text, "Related file ") {
			t.Error("the symfony preset is not enabled")
		}
	}
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		target := filepath.Join(to, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRun_ChangedRelatedFileReviewsTheUnitAgain(t *testing.T) {
	dir := t.TempDir()
	copyTree(t, filepath.Join("..", "..", "testdata", "mrs", voterFixture), dir)
	h := newHarness(t, dir)

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}
	h.keepPostedComments()
	h.requests, h.fake.posted = nil, nil

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}
	if len(h.requests) != 0 {
		t.Fatalf("nothing changed, got %d requests", len(h.requests))
	}

	voter := filepath.Join(dir, "head", "src", "Security", "Voter", "InvoiceVoter.php")
	data, err := os.ReadFile(voter)
	if err != nil {
		t.Fatal(err)
	}
	fixed := strings.Replace(string(data), "$token->getUser() instanceof UserInterface", "$subject->getOwner() === $token->getUser()", 1)
	if err := os.WriteFile(voter, []byte(fixed), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}
	if len(h.requests) == 0 {
		t.Fatal("a change of the voter must review its controller again")
	}
	if !strings.Contains(requestTexts(h)[0], "getOwner()") {
		t.Error("the review must see the new voter")
	}
}

func TestMergeRelated(t *testing.T) {
	a := collect.RelatedPolicy{Presets: []string{"symfony"}, MaxFiles: 6, MaxBytes: 100}
	b := collect.RelatedPolicy{Presets: []string{"symfony", "vue"}, MaxFiles: 3, MaxBytes: 500}

	got := mergeRelated(a, b)

	if len(got.Presets) != 2 || got.Presets[1] != "vue" || got.MaxFiles != 6 || got.MaxBytes != 500 {
		t.Errorf("unexpected policy %+v", got)
	}
}
