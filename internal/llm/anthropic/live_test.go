package anthropic

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/llm"
	"github.com/iq2i/ainspector/internal/modes/quality"
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

// TestLive_QualityRequest sends the request of the quality mode as is: structured output,
// adaptive thinking and server-side fallbacks together have only been validated offline so far
func TestLive_QualityRequest(t *testing.T) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY is not set")
	}

	var conventions strings.Builder
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&conventions, "Convention %d: handlers validate their input and return wrapped errors.\n", i)
	}
	pc := &collect.ProjectContext{Description: conventions.String()}
	unit := collect.ReviewUnit{
		Kind: collect.KindFunction, FilePath: "main.go", Language: "go", Name: "divide", StartLine: 1, EndLine: 3,
		ChangeType: "modified",
		Content:    "func divide(a, b int) int {\n\treturn a / b\n}",
		Diff:       "+\treturn a / b",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	mode := quality.New("claude-sonnet-5-5", nil, false)
	client := New(key, "claude-sonnet-5-5")
	var last llm.Response
	for i := 0; i < 2; i++ {
		resp, err := client.Complete(ctx, mode.BuildRequest(unit, pc))
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if resp.JSON == nil {
			t.Fatalf("call %d: expected structured JSON, got text %q", i, resp.Text)
		}
		if _, err := mode.Parse(resp, unit); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		last = resp
	}

	if last.Usage.CacheRead == 0 {
		t.Errorf("expected cache reads on the second call, got usage %+v", last.Usage)
	}
}
