package quality

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

// renderBlocks shows the system blocks as sent, with the cache breakpoint where it applies
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
		"nil": nil,
		"raw": {Description: "=== README.md ===\n# Demo\n"},
	}
	ruleSets := map[int][]string{
		0: nil,
		2: {"Never use panic in library code", "Every exported function needs a test"},
	}

	for _, lang := range sortedLanguages() {
		for ctxName, pc := range contexts {
			for n, rules := range ruleSets {
				name := fmt.Sprintf("%s-%s-r%d.txt", lang, ctxName, n)
				t.Run(name, func(t *testing.T) {
					got := renderBlocks(buildSystemBlocks(lang, pc, rules))
					assertGolden(t, filepath.Join("testdata", "golden", "system", name), []byte(got))
				})
			}
		}
	}
}

func TestGolden_UserPrompt(t *testing.T) {
	for _, lang := range sortedLanguages() {
		for _, withDiff := range []bool{true, false} {
			fn := &collect.ReviewUnit{
				Name:       "handle",
				StartLine:  10,
				EndLine:    14,
				Content:    "handle() {\n  doWork()\n}",
				FilePath:   "src/handler",
				Language:   lang,
				ChangeType: "modified",
			}
			suffix := "nodiff"
			if withDiff {
				fn.Diff = "@@ -11,1 +11,1 @@\n-  oldWork()\n+  doWork()"
				suffix = "diff"
			}
			name := fmt.Sprintf("%s-%s.txt", lang, suffix)
			t.Run(name, func(t *testing.T) {
				assertGolden(t, filepath.Join("testdata", "golden", "user", name), []byte(buildUserPrompt(fn)))
			})
		}
	}
}

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func TestPromptChecksums(t *testing.T) {
	if got := New("", nil).Version(); got != checksumsVersion {
		t.Errorf("checksums were recorded for %s but the mode is %s: update the checksums together with the version", checksumsVersion, got)
	}

	if got := sum(baseSystemPrompt); got != wantBaseChecksum {
		t.Errorf("baseSystemPrompt changed (sha256 %s): bump the mode version and regenerate goldens", got)
	}
	for lang, want := range wantRuleChecksums {
		if got := sum(languageSpecificRules[lang]); got != want {
			t.Errorf("rules for %s changed (sha256 %s): bump the mode version and regenerate goldens", lang, got)
		}
	}
	if len(wantRuleChecksums) != len(languageSpecificRules) {
		t.Errorf("checksum table covers %d languages, prompts define %d", len(wantRuleChecksums), len(languageSpecificRules))
	}
}

// checksumsVersion is the mode version the checksums below belong to
const checksumsVersion = "quality/3"

const wantBaseChecksum = "890b1e3d9fd81070e48a55722ea2d3ce35bd10653ce7587938b49967c0c5e98c"

var wantRuleChecksums = map[string]string{
	"bash":       "a9e9d437de8091fdca22c69b77f39babd3b8ff7aa82db4af5fc92783d25e5d5a",
	"c":          "a743e36e8ceac08b0bfcbe03ab3f046584ec51712f233dac46195b0bd19752a8",
	"cpp":        "7bcc033978f4357f4ae1ace8079d15e241df67073cf94ffb80a9f130f12e2690",
	"csharp":     "44503ccab5d1b02a1a419ff2195ff2ca6913b3033a5490e7f0b09fa67001ae5b",
	"go":         "bb0c51d621ebde477fe347fd27db6402c7817d065818dd0cbe05c530e61c71f4",
	"java":       "5ba79bf4c9bcf226961d8805ef3fa9f0d74aacf3a66ed9cb926254137078bb0d",
	"javascript": "6c7facd7698b1ed484734989ecd68828793d78a13d1bf3f94acb0fa6640bf3ec",
	"php":        "b2bd55da6442e1c155bdad9a84edc2add09169e5b8ea74153306bc8750e80860",
	"python":     "86c53fc9d37e57d95b26711024ae4dcd45705e2b27f6ce9f3a69d7c5b6ca08ab",
	"ruby":       "fbe8067921cfcc3bf481c2a658b885ca4d08678dc8ab08b1dee39324e05341a3",
	"rust":       "517170211fb4b53fa2f8997d7372c48e589f2511846c5e85db518f14f4358a42",
	"typescript": "93205d811b6931b9dc781b8fddd317c40553ad356ba4157dd8224b56cc206934",
}
