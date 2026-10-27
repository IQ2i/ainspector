package anthropic

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/iq2i/ainspector/internal/llm"
)

// TestLive_PromptCaching calls the real API, so it only runs when ANTHROPIC_API_KEY is set
func TestLive_PromptCaching(t *testing.T) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY is not set")
	}

	var prefix strings.Builder
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&prefix, "Rule %d: functions must be small, named after what they do, and covered by tests.\n", i)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	client := New(key, "claude-sonnet-5-5")
	var second llm.Response
	for i := 0; i < 2; i++ {
		resp, err := client.Complete(ctx, llm.Request{
			System:    []llm.Block{{Text: prefix.String(), Cache: true}},
			Messages:  []llm.Message{{Role: "user", Content: fmt.Sprintf("Reply with the single word ok (call %d).", i)}},
			MaxTokens: 2000,
		})
		if err != nil {
			t.Fatal(err)
		}
		second = resp
	}

	if second.Usage.CacheRead == 0 {
		t.Errorf("expected cache reads on the second call, got usage %+v", second.Usage)
	}
}
