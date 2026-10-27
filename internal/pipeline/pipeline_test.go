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
	"github.com/iq2i/ainspector/internal/publish"
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
	// CodeQuality and Artifact are the files written by the run, when it wrote them
	CodeQuality json.RawMessage `json:"code_quality,omitempty"`
	Artifact    json.RawMessage `json:"artifact,omitempty"`
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
	// reactions are the thumbs of the comments by ID, reactionErr is returned with them
	reactions   map[string]provider.ReactionCount
	reactionErr error
}

func (f *fakeProvider) ListReactions(ctx context.Context, number int, ids []string) (map[string]provider.ReactionCount, error) {
	got := make(map[string]provider.ReactionCount)
	for _, id := range ids {
		if c, ok := f.reactions[id]; ok {
			got[id] = c
		}
	}
	return got, f.reactionErr
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
	// reportDir receives the reports of the run, never the fixtures
	reportDir string
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

	h.reportDir = t.TempDir()
	h.deps = Deps{
		Provider:  h.fake,
		ReportDir: h.reportDir,
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
	for i, c := range h.fake.posted {
		id := fmt.Sprintf("inline-%d", i+1)
		h.fake.comment = append(h.fake.comment, provider.ExistingComment{ID: id, Author: h.fake.self, Path: c.Path, Line: c.Line, Body: c.Body, Positioned: true})
	}
}

// report returns a file written by the run, nil when there is none
func (h *harness) report(name string) json.RawMessage {
	data, err := os.ReadFile(filepath.Join(h.reportDir, name))
	if err != nil {
		return nil
	}
	return data
}

func (h *harness) result() expectedRun {
	// Units are reviewed concurrently: requests are compared in a stable order
	requests := append([]json.RawMessage(nil), h.requests...)
	sort.Slice(requests, func(i, j int) bool { return string(requests[i]) < string(requests[j]) })

	got := expectedRun{LLMRequests: requests, Notes: h.fake.noteBodies()}
	got.CodeQuality = h.report("gl-code-quality-report.json")
	got.Artifact = h.report("ainspector-report.json")
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
			if tt.wantIgnored && (h.report("gl-code-quality-report.json") != nil || h.report("ainspector-report.json") != nil) {
				t.Error("an ignored change must not write a report")
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

func TestRun_VendoredAndUnsafeFilesAreNotReviewed(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "mrs", "go-basic")
	baseline := newHarness(t, dir)
	if err := Run(context.Background(), baseline.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}

	h := newHarness(t, dir)
	main := h.fake.mr.Files[0]
	for _, path := range []string{"vendor/acme/lib.go", "web/node_modules/x/index.go", "evil\n::error::injected.go"} {
		extra := main
		extra.Path = path
		h.fake.mr.Files = append(h.fake.mr.Files, extra)
	}
	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}

	if len(h.requests) != len(baseline.requests) {
		t.Errorf("got %d requests, want %d: only the regular file is reviewed", len(h.requests), len(baseline.requests))
	}
}

func TestDropUnsafePaths(t *testing.T) {
	files := []provider.ModifiedFile{{Path: "a.go"}, {Path: "b\r\n.go"}, {Path: "c.go", OldPath: "d\x1b[2K.go"}}
	got := dropUnsafePaths(files)
	if len(got) != 1 || got[0].Path != "a.go" {
		t.Errorf("got %+v", got)
	}
}

func TestRun_EnvironmentCapBeatsTheConfiguration(t *testing.T) {
	h := newHarness(t, filepath.Join("..", "..", "testdata", "mrs", "go-basic"))
	h.deps.Config.LLM.MaxTokensPerRun = 1000000
	h.deps.MaxTokens = 1

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}
	if len(h.requests) != 1 {
		t.Errorf("the cap of the environment leaves room for one unit only, got %d requests", len(h.requests))
	}

	h = newHarness(t, filepath.Join("..", "..", "testdata", "mrs", "go-basic"))
	h.deps.Config.LLM.MaxTokensPerRun = 1
	h.deps.MaxTokens = 1000000
	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}
	if len(h.requests) != 1 {
		t.Errorf("a high cap in the environment must not raise the configuration, got %d requests", len(h.requests))
	}
}

func TestRun_ConfigurationChangeIsFlagged(t *testing.T) {
	h := newHarness(t, filepath.Join("..", "..", "testdata", "mrs", "go-basic"))
	h.fake.mr.Files = append(h.fake.mr.Files, h.fake.mr.Files[0])
	h.fake.mr.Files[len(h.fake.mr.Files)-1].Path = "ainspector.yaml"
	h.fake.mr.Files[len(h.fake.mr.Files)-1].Patch = "@@ -1 +1 @@\n-rules: []\n+rules: [x]\n"

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}
	notes := h.fake.noteBodies()
	if len(notes) != 1 || !strings.Contains(notes[0], "This change modifies ainspector.yaml") {
		t.Errorf("the summary should flag the configuration change, got %v", notes)
	}
}

// captureStdout returns what fn printed on the standard output
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		data, _ := io.ReadAll(r)
		done <- string(data)
	}()
	defer func() { os.Stdout = old }()

	fn()
	_ = w.Close()
	os.Stdout = old
	return <-done
}

func TestRun_SecretsNeverReachLogsCommentsOrReports(t *testing.T) {
	const apiKey = "sk-ant-sentinel-0123456789"
	const token = "glpat-sentinel-0123456789"

	// An endpoint that rejects the key and echoes it, as some do
	rejecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "Incorrect API key provided: "+apiKey+" (token "+token+")")
	}))
	defer rejecting.Close()

	h := newHarness(t, filepath.Join("..", "..", "testdata", "mrs", "go-basic"))
	h.deps.Secrets = []string{apiKey, token}
	h.deps.NewLLM = func() (llm.Client, error) { return openai.New(rejecting.URL, apiKey, "test-model"), nil }

	var runErr error
	out := captureStdout(t, func() { runErr = Run(context.Background(), h.deps, Options{Number: 1}) })
	if runErr != nil {
		t.Fatal(runErr)
	}
	if !strings.Contains(out, "failed") {
		t.Fatalf("the failed review should be logged, got:\n%s", out)
	}

	var published []string
	published = append(published, out)
	for _, c := range h.fake.posted {
		published = append(published, c.Body, c.Suggestion)
	}
	published = append(published, h.fake.noteBodies()...)
	for _, name := range []string{"gl-code-quality-report.json", "ainspector-report.json"} {
		published = append(published, string(h.report(name)))
	}
	for _, text := range published {
		for _, secret := range []string{apiKey, token} {
			if strings.Contains(text, secret) {
				t.Errorf("a secret leaked:\n%s", text)
			}
		}
	}
}

func TestRun_ReactionsToEarlierCommentsAreRecorded(t *testing.T) {
	h := newHarness(t, filepath.Join("..", "..", "testdata", "mrs", "go-basic"))
	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}
	if len(h.fake.posted) == 0 {
		t.Fatal("the first run should comment")
	}
	h.keepPostedComments()
	h.fake.reactions = map[string]provider.ReactionCount{"inline-1": {Up: 2}, "inline-2": {Down: 1}}
	h.fake.reactionErr = errors.New("one note could not be read")

	out := captureStdout(t, func() {
		if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "could not read all the reactions") {
		t.Errorf("the partial failure should be logged, got:\n%s", out)
	}

	var doc struct {
		Modes []struct{ Feedback []publish.Feedback }
	}
	if err := json.Unmarshal(h.report("ainspector-report.json"), &doc); err != nil {
		t.Fatal(err)
	}
	fb := doc.Modes[0].Feedback
	if len(fb) != 2 || fb[0].Up != 2 || fb[1].Down != 1 || fb[0].File == "" || fb[0].Unit == "" {
		t.Errorf("unexpected feedback %+v", fb)
	}
}

func TestRun_NoReactionsAreReadUnderForce(t *testing.T) {
	h := newHarness(t, filepath.Join("..", "..", "testdata", "mrs", "go-basic"))
	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}
	h.keepPostedComments()
	h.fake.reactions = map[string]provider.ReactionCount{"inline-1": {Up: 1}}

	if err := Run(context.Background(), h.deps, Options{Number: 1, Force: true}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(h.report("ainspector-report.json")), `"feedback"`) {
		t.Error("--force does not read the earlier comments")
	}
}

// withConfigChange adds a modified ainspector.yaml to the change
func (h *harness) withConfigChange() {
	h.fake.mr.Files = append(h.fake.mr.Files, h.fake.mr.Files[0])
	last := &h.fake.mr.Files[len(h.fake.mr.Files)-1]
	last.Path, last.OldPath = "ainspector.yaml", ""
	last.Patch = "@@ -1 +1 @@\n-rules: []\n+rules: [x]\n"
}

func TestRun_ConfigurationChangeIsNoticedWhenTheRunStopsEarly(t *testing.T) {
	stops := map[string]func(h *harness){
		"ignored author": func(h *harness) { h.fake.mr.Change.Author = "Renovate-Bot" },
		"no mode enabled": func(h *harness) {
			off := false
			h.deps.Config.Modes.Quality.Enabled = &off
		},
		"nothing left to review": func(h *harness) { h.deps.Config.Ignore.Paths = []string{"**"} },
	}

	for name, stop := range stops {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, filepath.Join("..", "..", "testdata", "mrs", "ignored-change"))
			h.withConfigChange()
			stop(h)

			for range 2 {
				if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
					t.Fatal(err)
				}
			}

			if len(h.requests) != 0 {
				t.Error("the run must have stopped before the LLM")
			}
			notes := h.fake.noteBodies()
			if len(notes) != 1 || !strings.Contains(notes[0], publish.NoticeMarker) || !strings.Contains(notes[0], "modifies `ainspector.yaml`") {
				t.Errorf("expected one notice about the configuration, got %v", notes)
			}
		})
	}
}

func TestRun_NoNoticeWhenTheConfigurationIsLeftAlone(t *testing.T) {
	h := newHarness(t, filepath.Join("..", "..", "testdata", "mrs", "ignored-change"))
	h.fake.mr.Change.Author = "Renovate-Bot"

	if err := Run(context.Background(), h.deps, Options{Number: 1}); err != nil {
		t.Fatal(err)
	}
	if h.fake.writes != 0 {
		t.Errorf("nothing should be written, got %v", h.fake.noteBodies())
	}
}

func TestRun_NoticeIsNotWrittenInDryRunNorWithoutTheTokenOwner(t *testing.T) {
	for name, opts := range map[string]struct {
		dryRun bool
		self   string
	}{
		"dry run":      {dryRun: true, self: "ainspector-bot"},
		"unknown self": {self: ""},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, filepath.Join("..", "..", "testdata", "mrs", "ignored-change"))
			h.withConfigChange()
			h.fake.mr.Change.Author = "Renovate-Bot"
			h.fake.self = opts.self

			if err := Run(context.Background(), h.deps, Options{Number: 1, DryRun: opts.dryRun}); err != nil {
				t.Fatal(err)
			}
			if h.fake.writes != 0 {
				t.Errorf("nothing should be written, got %v", h.fake.noteBodies())
			}
		})
	}
}
