package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iq2i/ainspector/internal/llm"
	"github.com/iq2i/ainspector/internal/llm/anthropic"
)

// TestLive_SecurityFixtures replays the security reference merge requests against the real
// API, false positive filter included. It only runs when ANTHROPIC_API_KEY is set:
//
//	ANTHROPIC_API_KEY=... go test ./internal/pipeline -run Live -v
func TestLive_SecurityFixtures(t *testing.T) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY is not set")
	}

	// categories of which at least one must be reported, none meaning that nothing may be
	cases := map[string][]string{
		"security-idor":              {"idor", "access-control"},
		"security-dql":               {"injection"},
		"security-twig-raw":          {"xss"},
		"security-vue-vhtml":         {"xss"},
		"security-removed-isgranted": {"access-control"},
		"security-voter-idor":        {"idor", "access-control"},
		"security-clean":             nil,
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, filepath.Join("..", "..", "testdata", "mrs", name))
			h.deps.NewLLM = func() (llm.Client, error) { return anthropic.New(key, "claude-sonnet-5-5"), nil }
			h.deps.DefaultFPModel = "claude-opus-5-5"

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			if err := Run(ctx, h.deps, Options{Number: 1}); err != nil {
				t.Fatal(err)
			}

			if want == nil {
				if len(h.fake.posted) != 0 {
					t.Errorf("a clean merge request must get no inline comment, got %+v", h.fake.posted)
				}
				if len(h.fake.noteBodies()) != 1 {
					t.Error("the summary must be there")
				}
				return
			}

			for _, c := range h.fake.posted {
				for _, category := range want {
					if strings.Contains(c.Body, "("+category+",") {
						return
					}
				}
			}
			t.Errorf("expected a finding of category %v, got %+v", want, h.fake.posted)
		})
	}
}
