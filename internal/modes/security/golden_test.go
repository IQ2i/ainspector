package security

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/llm"
)

var update = flag.Bool("update", false, "regenerate golden files")

func assertGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s (run with -update): %v", path, err)
	}
	if string(want) != string(got) {
		t.Errorf("golden mismatch for %s\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}

func renderBlocks(blocks []llm.Block) string {
	parts := make([]string, len(blocks))
	for i, b := range blocks {
		parts[i] = b.Text
		if b.Cache {
			parts[i] += "\n<<< cache breakpoint >>>"
		}
	}
	return strings.Join(parts, "\n\n")
}

func sortedLanguages() []string {
	langs := []string{"unknown"}
	for l := range languageSpecificRules {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	return langs
}

func TestGolden_SystemPrompt(t *testing.T) {
	contexts := map[string]*collect.ProjectContext{
		"nil":     nil,
		"raw":     {Description: "=== README.md ===\n# Demo\n"},
		"changed": {Description: "=== README.md ===\n# Demo\n", ChangedFiles: []string{"src/Controller/AccountController.php", "tests/Controller/AccountControllerTest.php", "evil\nname.php"}},
	}
	for _, lang := range sortedLanguages() {
		for ctxName, pc := range contexts {
			name := fmt.Sprintf("%s-%s.txt", lang, ctxName)
			t.Run(name, func(t *testing.T) {
				assertGolden(t, filepath.Join("testdata", "golden", "system", name), []byte(renderBlocks(buildSystemBlocks(lang, pc))))
			})
		}
	}
}

func TestGolden_UserPrompt(t *testing.T) {
	function := collect.ReviewUnit{
		Kind: collect.KindFunction, Name: "show", StartLine: 14, EndLine: 18, FilePath: "src/Controller/AccountController.php", Language: "php",
		Content: "#[Route('/{id}')]\npublic function show(Account $account): Response\n{\n    return $this->render('account/show.html.twig', ['account' => $account]);\n}",
		Diff:    "-    #[IsGranted('ACCOUNT_VIEW', 'account')]\n+    #[Route('/{id}')]", ChangeType: "modified",
		Enclosing: &collect.ClassHeader{Name: "AccountController", StartLine: 8, EndLine: 25, Rendered: "#[IsGranted('ROLE_USER')]\nclass AccountController\n{\n    #[Route('/{id}')]\n    public function show(Account $account): Response { ... }\n}"},
	}
	deleted := function
	deleted.DeletedOnly = true
	withRelated := function
	withRelated.Related = []collect.RelatedFile{
		{Path: "src/Security/Voter/AccountVoter.php", Reason: "voter named after Account,\nwhich show receives", Content: "<?php\nclass AccountVoter\n{\n    protected function voteOnAttribute(): bool { return true; }\n}"},
		{Path: "config/packages/security.yaml", Reason: "firewalls and access_control of the security configuration", Content: "security:\n  access_control: []\n... (truncated)", Truncated: true},
	}
	hunk := collect.ReviewUnit{
		Kind: collect.KindHunk, Name: "lines 3-9", StartLine: 3, EndLine: 9, FilePath: "templates/account/show.html.twig", Language: "twig",
		Content: "<h1>{{ account.name }}</h1>\n<div>{{ account.bio|raw }}</div>", Diff: "+<div>{{ account.bio|raw }}</div>", ChangeType: "modified",
	}

	for name, unit := range map[string]collect.ReviewUnit{
		"function":     function,
		"deleted-only": deleted,
		"related":      withRelated,
		"hunk":         hunk,
	} {
		t.Run(name, func(t *testing.T) {
			assertGolden(t, filepath.Join("testdata", "golden", "user", name+".txt"), []byte(buildUserPrompt(unit)))
		})
	}
}

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// TestPromptChecksums fails when a prompt changes without a bump of the mode version, as the
// unit hash would not invalidate the reviews made with the previous prompt
func TestPromptChecksums(t *testing.T) {
	if got := New(Config{}).Version(); got != checksumsVersion {
		t.Errorf("checksums were recorded for %s but the mode is %s: update the checksums together with the version", checksumsVersion, got)
	}
	if got := sum(baseSystemPrompt); got != wantBaseChecksum {
		t.Errorf("baseSystemPrompt changed (sha256 %s): bump the mode version and regenerate goldens", got)
	}
	if got := sum(string(reviewSchema)); got != wantSchemaChecksum {
		t.Errorf("reviewSchema changed (sha256 %s): bump the mode version and regenerate goldens", got)
	}
	for lang, rules := range languageSpecificRules {
		want, ok := wantRuleChecksums[lang]
		if !ok {
			t.Errorf("no checksum recorded for %s (sha256 %s)", lang, sum(rules))
			continue
		}
		if got := sum(rules); got != want {
			t.Errorf("rules for %s changed (sha256 %s): bump the mode version and regenerate goldens", lang, got)
		}
	}
	if len(wantRuleChecksums) != len(languageSpecificRules) {
		t.Errorf("checksum table covers %d languages, prompts define %d", len(wantRuleChecksums), len(languageSpecificRules))
	}
}

const checksumsVersion = "security/2"

const (
	wantBaseChecksum   = "c8ad80dcb2ac076417c78b053bb80322b847ab3b15f1c8a9253db54a4528ae56"
	wantSchemaChecksum = "7bf0fcd510cb57ff5838562de0f39a572109646828fb06f1d3cb48dd80ba6dc3"
)

var wantRuleChecksums = map[string]string{
	"bash":       "5f96132d6f33aad0bc7e45e552b02056e0a76dbc97399dd73c9474b5a86b3237",
	"c":          "14327dace9989349d03f0179b35bfc215b6aa5bef4a388755cad684dab8f52e4",
	"cpp":        "c5aef087aa3cc02382817ead693adb6cf5475b7dca240e3580d4ab5fcaa20490",
	"csharp":     "2e5af14351d89c1dd4ddeb510dff591093d39f62545f9828a861190afadef303",
	"env":        "4d5877be31f5144e96e399d11cf011c882b4d73ab35e77ddab2ac18d22bdce2f",
	"go":         "82fd36312394dc9f4fc54a25e858ab632f4558809fdda6297960eb30cad2b841",
	"java":       "57bab6870430ddbb4ab19df752377f9d836634a45145638f953c545639716548",
	"javascript": "8e3aba2c2a1572aeba3f692d1e066de4973261cd6f4aab390035e791271d5214",
	"php":        "d72b6ce68bf66fa400b325c3359d219b7087b7b1da4f61c57af5ba80993c5c5a",
	"python":     "3c705ce33fb7d15d64a3491b70f4174aca54fbeb9446ca3b8af33966a7fa29f8",
	"ruby":       "9e0fece137d428c708f846809b31c57f25ccb64fb61ad7d059f20d18b217c633",
	"rust":       "a54ba23d4269e85bb648ea145da4b4ac1d76b6029830dab6eae1a669da006e89",
	"twig":       "20634f16af6125c6a9b664e0112dc3969d83f317fccf5481cf4d7dc99d73841f",
	"typescript": "95abfea2c0a22d78a2eba98b752dfd9f6cdd2fccbe25f1121cc5633ff9fcce9f",
	"vue":        "8ee2b114be5327c249af93e3cc22a499573abf98678137a7a43f4c2fe6865bad",
	"yaml":       "82f5d60f6e70eb84963ece28fb988843b7bca7e2cf574466e904eea3293d812d",
}
