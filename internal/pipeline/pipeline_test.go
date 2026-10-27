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
	"sort"
	"strconv"
	"strings"
	"sync"
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
	Notes       []string          `json:"notes"`
}

type fakeProvider struct {
	mr      mrFixture
	head    string
	posted  []provider.ReviewComment
	writes  int
	comment []provider.ExistingComment
	notes   []string
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

// PostComment keeps the note like a real PR/MR would, so that the next run finds it
func (f *fakeProvider) PostComment(ctx context.Context, number int, body string) (string, error) {
	f.writes++
	if f.postErr != nil {
		return "", f.postErr
	}
	id := strconv.Itoa(len(f.comment) + 1)
	f.comment = append(f.comment, provider.ExistingComment{ID: id, Author: f.self, Body: body})
	f.notes = append(f.notes, id)
	return id, nil
}

func (f *fakeProvider) UpdateComment(ctx context.Context, number int, id, body string) error {
	f.writes++
	for i := range f.comment {
		if f.comment[i].ID == id {
			f.comment[i].Body = body
		}
	}
	return nil
}

// noteBodies returns the current body of every general note written by the run
func (f *fakeProvider) noteBodies() []string {
	var bodies []string
	for _, id := range f.notes {
		for _, c := range f.comment {
			if c.ID == id {
				bodies = append(bodies, c.Body)
			}
		}
	}
	return bodies
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

var (
	functionRe = regexp.MustCompile(`Function: (\S+) \(lines`)
	hunkRe     = regexp.MustCompile(`File: (\S+)\nChanged lines: \d+-\d+`)
)

// falsePositiveKey is the key of responses.json holding the reply of the false positive filter
const falsePositiveKey = "__false_positive__"

// responseKey tells which scripted reply a request gets: the function name for a function unit,
// the file path for a hunk, and a fixed key for the false positive filter
func responseKey(prompt string) (string, bool) {
	if strings.Contains(prompt, "FINDINGS TO ASSESS") {
		return falsePositiveKey, true
	}
	if m := functionRe.FindStringSubmatch(prompt); m != nil {
		return m[1], true
	}
	if m := hunkRe.FindStringSubmatch(prompt); m != nil {
		return m[1], true
	}
	return "", false
}

func scriptedLLM(t *testing.T, responses map[string]string, mu *sync.Mutex, requests *[]json.RawMessage) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		*requests = append(*requests, json.RawMessage(body))
		mu.Unlock()

		var req openai.ChatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("invalid request body: %v", err)
		}

		key, found := responseKey(req.Messages[len(req.Messages)-1].Content)
		if !found {
			t.Errorf("cannot find the unit in the prompt")
		}

		content, ok := responses[key]
		if !ok {
			t.Errorf("no scripted response for %q", key)
			content = `{"issues": []}`
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": content}}},
			"usage":   map[string]int{"prompt_tokens": 100, "completion_tokens": 10},
		})
	}))
}

type harness struct {
	fake     *fakeProvider
	mu       sync.Mutex
	requests []json.RawMessage
	deps     Deps
	newCalls int
	mr       mrFixture
	// responses is read by the scripted server: tests may change it before the run
	responses map[string]string
}

func newHarness(t *testing.T, dir string) *harness {
	t.Helper()

	h := &harness{}
	readJSON(t, filepath.Join(dir, "mr.json"), &h.mr)
	var responses map[string]string
	readJSON(t, filepath.Join(dir, "responses.json"), &responses)
	h.responses = responses

	cfg, err := config.LoadFromPath(filepath.Join(dir, "ainspector.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	h.fake = &fakeProvider{mr: h.mr, head: filepath.Join(dir, "head"), self: "ainspector-bot"}
	for _, c := range h.mr.ExistingComments {
		h.fake.comment = append(h.fake.comment, provider.ExistingComment{Author: h.fake.self, Path: c.Path, Line: c.Line, Body: c.Body})
	}

	server := scriptedLLM(t, responses, &h.mu, &h.requests)
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

// keepPostedComments makes the inline comments of the run visible to the next one
func (h *harness) keepPostedComments() {
	for _, c := range h.fake.posted {
		h.fake.comment = append(h.fake.comment, provider.ExistingComment{Author: h.fake.self, Path: c.Path, Line: c.Line, Body: c.Body, Positioned: true})
	}
}

func (h *harness) result() expectedRun {
	// Units are reviewed concurrently: requests are compared in a stable order
	requests := append([]json.RawMessage(nil), h.requests...)
	sort.Slice(requests, func(i, j int) bool { return string(requests[i]) < string(requests[j]) })

	got := expectedRun{LLMRequests: requests, Notes: h.fake.noteBodies()}
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

	h.keepPostedComments()
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

func TestRun_SecondRunSkipsCleanUnitsThanksToTheStateBlock(t *testing.T) {
	h := newHarness(t, filepath.Join("..", "..", "testdata", "mrs", "go-basic"))

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}
	if len(h.requests) != 3 || len(h.fake.posted) != 2 {
		t.Fatalf("expected 3 reviews and 2 comments on the first run, got %d and %d", len(h.requests), len(h.fake.posted))
	}

	// Only two of the three units left an inline marker: the third one (greet) is known
	// from the state block of the summary alone
	h.keepPostedComments()
	h.fake.posted = nil
	h.requests = nil

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}
	if len(h.requests) != 0 {
		t.Errorf("a clean unit must not be reviewed again, got %d requests", len(h.requests))
	}
}

func TestRun_SummaryWithoutFindings(t *testing.T) {
	h := newHarness(t, filepath.Join("..", "..", "testdata", "mrs", "ignored-change"))
	h.fake.mr.Change.Author = "alice"
	h.fake.mr.Change.Title = "Fix"

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}

	if len(h.fake.posted) != 0 {
		t.Errorf("no inline comment expected, got %d", len(h.fake.posted))
	}
	notes := h.fake.noteBodies()
	if len(notes) != 1 || !strings.Contains(notes[0], "Reviewed 1 function, no issue found.") {
		t.Errorf("expected a summary note, got %v", notes)
	}
}

func TestRun_SummaryIsUpdatedNotDuplicated(t *testing.T) {
	h := newHarness(t, filepath.Join("..", "..", "testdata", "mrs", "go-basic"))

	for i := 0; i < 2; i++ {
		if err := Run(context.Background(), h.deps, Options{Number: 1, Force: true}); err != nil {
			t.Fatal(err)
		}
	}

	if len(h.fake.notes) != 1 {
		t.Errorf("the second run must update the first summary, got %d notes", len(h.fake.notes))
	}
}

func TestRun_BudgetExhaustionIsReportedInTheSummary(t *testing.T) {
	h := newHarness(t, filepath.Join("..", "..", "testdata", "mrs", "go-basic"))
	h.deps.Config.LLM.MaxTokensPerRun = 1

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}

	if len(h.requests) != 1 {
		t.Errorf("only the first unit fits the budget, got %d requests", len(h.requests))
	}
	notes := h.fake.noteBodies()
	if len(notes) != 1 {
		t.Fatalf("expected a summary, got %v", notes)
	}
	for _, want := range []string{"Reviewed 1 function", "the token budget was reached:\n- divide (main.go)\n- printAll (main.go)", "Tokens: 100 in, 10 out"} {
		if !strings.Contains(notes[0], want) {
			t.Errorf("summary should contain %q, got:\n%s", want, notes[0])
		}
	}
}

func TestRun_SummaryIsSkippedWithoutTheTokenOwner(t *testing.T) {
	h := newHarness(t, filepath.Join("..", "..", "testdata", "mrs", "php-symfony"))
	h.fake.self = ""

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Errorf("a missing identity must not fail the job, got %v", err)
	}
	if len(h.fake.posted) != 1 || len(h.fake.notes) != 0 {
		t.Errorf("the inline comment must still be posted and no summary written, got %d and %d", len(h.fake.posted), len(h.fake.notes))
	}
}

func TestRun_DryRunPrintsTheSummaryWithoutWriting(t *testing.T) {
	h := newHarness(t, filepath.Join("..", "..", "testdata", "mrs", "ignored-change"))
	h.fake.mr.Change.Author = "alice"

	if err := Run(context.Background(), h.deps, Options{Number: 1, DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if h.fake.writes != 0 || len(h.fake.notes) != 0 {
		t.Errorf("dry run must not write, got %d writes", h.fake.writes)
	}
}

func TestRun_ForgedMarkerDoesNotSkipTheReview(t *testing.T) {
	h := newHarness(t, filepath.Join("..", "..", "testdata", "mrs", "php-symfony"))

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
