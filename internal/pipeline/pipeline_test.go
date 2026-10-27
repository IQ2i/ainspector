package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/iq2i/ainspector/internal/config"
	"github.com/iq2i/ainspector/internal/llm"
	"github.com/iq2i/ainspector/internal/llm/openai"
	"github.com/iq2i/ainspector/internal/provider"
)

var update = flag.Bool("update", false, "regenerate expected.json of reference merge requests")

type mrFixture struct {
	Files []struct {
		Path    string `json:"path"`
		OldPath string `json:"old_path"`
		Status  string `json:"status"`
		Patch   string `json:"patch"`
	} `json:"files"`
	ExistingComments []struct {
		Path string `json:"path"`
		Line int    `json:"line"`
		Body string `json:"body"`
	} `json:"existing_comments"`
	Force  bool `json:"force"`
	Change struct {
		Author string   `json:"author"`
		Title  string   `json:"title"`
		Labels []string `json:"labels"`
	} `json:"change"`
}

type expectedComment struct {
	Path       string `json:"path"`
	Line       int    `json:"line"`
	Body       string `json:"body"`
	Suggestion string `json:"suggestion"`
}

type expectedRun struct {
	LLMRequests []json.RawMessage `json:"llm_requests"`
	Comments    []expectedComment `json:"comments"`
}

type fakeProvider struct {
	mr      mrFixture
	head    string
	posted  []provider.ReviewComment
	writes  int
	comment []provider.ExistingComment
	self    string
	postErr error
}

func (f *fakeProvider) GetModifiedFiles(ctx context.Context, number int) ([]provider.ModifiedFile, error) {
	var out []provider.ModifiedFile
	for _, file := range f.mr.Files {
		out = append(out, provider.ModifiedFile{Path: file.Path, OldPath: file.OldPath, Status: file.Status, Patch: file.Patch})
	}
	return out, nil
}

func (f *fakeProvider) GetFileContent(ctx context.Context, path string) (string, error) {
	data, err := os.ReadFile(filepath.Join(f.head, path))
	return string(data), err
}

func (f *fakeProvider) GetChangeInfo(ctx context.Context, number int) (provider.ChangeInfo, error) {
	c := f.mr.Change
	return provider.ChangeInfo{Number: number, Author: c.Author, Title: c.Title, Labels: c.Labels}, nil
}

func (f *fakeProvider) CurrentUser(ctx context.Context) (string, error) {
	return f.self, nil
}

func (f *fakeProvider) PostComment(ctx context.Context, number int, body string) (string, error) {
	f.writes++
	return "1", f.postErr
}

func (f *fakeProvider) UpdateComment(ctx context.Context, number int, id, body string) error {
	f.writes++
	return nil
}

func (f *fakeProvider) CreateReview(ctx context.Context, number int, comments []provider.ReviewComment) ([]provider.CommentResult, error) {
	f.writes++
	f.posted = append(f.posted, comments...)
	results := make([]provider.CommentResult, len(comments))
	for i, c := range comments {
		results[i] = provider.CommentResult{Comment: c, Err: f.postErr}
	}
	return results, nil
}

func (f *fakeProvider) ListComments(ctx context.Context, number int) ([]provider.ExistingComment, error) {
	return f.comment, nil
}

var functionRe = regexp.MustCompile(`Function: (\S+) \(lines`)

func scriptedLLM(t *testing.T, responses map[string]string, requests *[]json.RawMessage) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*requests = append(*requests, json.RawMessage(body))

		var req openai.ChatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("invalid request body: %v", err)
		}

		m := functionRe.FindStringSubmatch(req.Messages[len(req.Messages)-1].Content)
		key := ""
		if m == nil {
			t.Errorf("cannot find function name in prompt")
		} else {
			key = m[1]
		}

		content, ok := responses[key]
		if !ok {
			t.Errorf("no scripted response for %q", key)
			content = `{"issues": []}`
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": content}}},
		})
	}))
}

type harness struct {
	fake     *fakeProvider
	requests []json.RawMessage
	deps     Deps
	newCalls int
	mr       mrFixture
}

func newHarness(t *testing.T, dir string) *harness {
	t.Helper()

	h := &harness{}
	readJSON(t, filepath.Join(dir, "mr.json"), &h.mr)
	var responses map[string]string
	readJSON(t, filepath.Join(dir, "responses.json"), &responses)

	cfg, err := config.LoadFromPath(filepath.Join(dir, "ainspector.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	h.fake = &fakeProvider{mr: h.mr, head: filepath.Join(dir, "head")}
	for _, c := range h.mr.ExistingComments {
		h.fake.comment = append(h.fake.comment, provider.ExistingComment{Path: c.Path, Line: c.Line, Body: c.Body})
	}

	server := scriptedLLM(t, responses, &h.requests)
	t.Cleanup(server.Close)

	h.deps = Deps{
		Provider: h.fake,
		NewLLM: func() (llm.Client, error) {
			h.newCalls++
			return openai.New(server.URL, "key", "test-model"), nil
		},
		Config:      cfg,
		ProjectRoot: h.fake.head,
	}
	return h
}

func (h *harness) result() expectedRun {
	got := expectedRun{LLMRequests: h.requests}
	for _, c := range h.fake.posted {
		got.Comments = append(got.Comments, expectedComment{Path: c.Path, Line: c.Line, Body: c.Body, Suggestion: c.Suggestion})
	}
	return got
}

func TestRun_ReferenceMRs(t *testing.T) {
	dirs, err := filepath.Glob(filepath.Join("..", "..", "testdata", "mrs", "*"))
	if err != nil || len(dirs) == 0 {
		t.Fatalf("no reference merge requests found: %v", err)
	}

	for _, dir := range dirs {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			h := newHarness(t, dir)

			if err := Run(context.Background(), h.deps, Options{Number: 1, Force: h.mr.Force}); err != nil {
				t.Fatal(err)
			}

			gotJSON, err := json.MarshalIndent(h.result(), "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			gotJSON = append(gotJSON, '\n')

			path := filepath.Join(dir, "expected.json")
			if *update {
				if err := os.WriteFile(path, gotJSON, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("missing %s (run with -update): %v", path, err)
			}
			if string(want) != string(gotJSON) {
				t.Errorf("output differs from %s\n--- want ---\n%s\n--- got ---\n%s", path, want, gotJSON)
			}
		})
	}
}

func TestRun_DryRunWritesNothing(t *testing.T) {
	h := newHarness(t, filepath.Join("..", "..", "testdata", "mrs", "go-basic"))

	if err := Run(context.Background(), h.deps, Options{Number: 1, DryRun: true}); err != nil {
		t.Fatal(err)
	}

	if h.fake.writes != 0 || len(h.fake.posted) != 0 {
		t.Errorf("dry run must not write, got %d writes", h.fake.writes)
	}
	if len(h.requests) == 0 {
		t.Error("dry run still reviews with the LLM")
	}
}

func TestRun_SecondRunReviewsNothing(t *testing.T) {
	h := newHarness(t, filepath.Join("..", "..", "testdata", "mrs", "php-symfony"))

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}
	if len(h.fake.posted) != 1 {
		t.Fatalf("expected 1 comment on the first run, got %d", len(h.fake.posted))
	}

	for _, c := range h.fake.posted {
		h.fake.comment = append(h.fake.comment, provider.ExistingComment{Path: c.Path, Line: c.Line, Body: c.Body})
	}
	h.fake.posted = nil
	h.requests = nil
	h.newCalls = 0

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}

	if len(h.requests) != 0 || len(h.fake.posted) != 0 {
		t.Errorf("second run should do nothing, got %d requests and %d comments", len(h.requests), len(h.fake.posted))
	}
	if h.newCalls != 0 {
		t.Error("the LLM client must not be created when everything was already reviewed")
	}
}

func TestRun_ForgedMarkerDoesNotSkipTheReview(t *testing.T) {
	h := newHarness(t, filepath.Join("..", "..", "testdata", "mrs", "php-symfony"))
	h.fake.self = "ainspector-bot"

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}
	if len(h.fake.posted) != 1 {
		t.Fatalf("expected 1 comment on the first run, got %d", len(h.fake.posted))
	}
	posted := h.fake.posted[0]

	rerun := func(author string) int {
		h.fake.comment = []provider.ExistingComment{{Author: author, Path: posted.Path, Line: posted.Line, Body: posted.Body}}
		h.fake.posted = nil
		h.requests = nil
		if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
			t.Fatal(err)
		}
		return len(h.requests)
	}

	if got := rerun("mallory"); got == 0 {
		t.Error("a marker copied by another user must not skip the review")
	}
	if got := rerun("ainspector-bot"); got != 0 {
		t.Errorf("a marker written by the tool must skip the review, got %d requests", got)
	}
}

func TestRun_PublishFailureIsReturned(t *testing.T) {
	h := newHarness(t, filepath.Join("..", "..", "testdata", "mrs", "php-symfony"))
	h.fake.postErr = errors.New("forbidden")

	err := Run(context.Background(), h.deps, Options{Number: 1})
	if err == nil || !strings.Contains(err.Error(), "mode quality") {
		t.Errorf("expected the publication error of the mode, got %v", err)
	}
}

func TestRun_IgnoredChange(t *testing.T) {
	tests := map[string]struct {
		author, title string
		labels        []string
		wantIgnored   bool
	}{
		"bot author, any case": {author: "Renovate-Bot", title: "Update deps", wantIgnored: true},
		"title pattern":        {author: "alice", title: "[skip-review] wip", wantIgnored: true},
		"label":                {author: "alice", title: "Fix", labels: []string{"bug", "No-AI-Review"}, wantIgnored: true},
		"regular change":       {author: "alice", title: "Fix", labels: []string{"bug"}, wantIgnored: false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, filepath.Join("..", "..", "testdata", "mrs", "ignored-change"))
			h.fake.mr.Change.Author, h.fake.mr.Change.Title, h.fake.mr.Change.Labels = tt.author, tt.title, tt.labels

			if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
				t.Fatal(err)
			}

			reviewed := len(h.requests) > 0
			if reviewed == tt.wantIgnored {
				t.Errorf("ignored = %v but the LLM was called = %v", tt.wantIgnored, reviewed)
			}
			if tt.wantIgnored && (h.newCalls != 0 || h.fake.writes != 0) {
				t.Error("an ignored change must not create the LLM client nor write anything")
			}
		})
	}
}

func TestRun_ChangeInfoFailureStopsTheRun(t *testing.T) {
	h := newHarness(t, filepath.Join("..", "..", "testdata", "mrs", "ignored-change"))
	h.deps.Provider = failingChangeInfo{h.fake}

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err == nil {
		t.Error("expected an error when the filters cannot be evaluated")
	}
}

func TestRun_NoModeEnabled(t *testing.T) {
	h := newHarness(t, filepath.Join("..", "..", "testdata", "mrs", "go-basic"))
	off := false
	h.deps.Config.Modes.Quality.Enabled = &off

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}
	if len(h.requests) != 0 || h.fake.writes != 0 {
		t.Error("nothing should run when no mode is enabled")
	}
}

type failingChangeInfo struct{ *fakeProvider }

func (failingChangeInfo) GetChangeInfo(context.Context, int) (provider.ChangeInfo, error) {
	return provider.ChangeInfo{}, errors.New("api down")
}

func TestRun_UnknownMode(t *testing.T) {
	h := newHarness(t, filepath.Join("..", "..", "testdata", "mrs", "go-basic"))

	if err := Run(context.Background(), h.deps, Options{Number: 1, Modes: []string{"nope"}}); err == nil {
		t.Error("expected an error for an unknown mode")
	}
	if len(h.requests) != 0 {
		t.Error("nothing should run before the modes are validated")
	}
}

func TestRun_MissingLLMKeyIsReported(t *testing.T) {
	h := newHarness(t, filepath.Join("..", "..", "testdata", "mrs", "go-basic"))
	h.deps.NewLLM = func() (llm.Client, error) { return nil, errors.New("LLM_API_KEY environment variable is required") }

	err := Run(context.Background(), h.deps, Options{Number: 1})
	if err == nil || !strings.Contains(err.Error(), "LLM_API_KEY") {
		t.Errorf("expected the LLM client error, got %v", err)
	}
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatal(fmt.Errorf("%s: %w", path, err))
	}
}
