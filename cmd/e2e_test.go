package cmd

import (
	"context"
	"encoding/json"
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
	Force bool `json:"force"`
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

func (f *fakeProvider) PostComment(ctx context.Context, number int, body string) error {
	f.writes++
	return nil
}

func (f *fakeProvider) CreateReview(ctx context.Context, number int, comments []provider.ReviewComment) error {
	f.writes++
	f.posted = append(f.posted, comments...)
	return nil
}

func (f *fakeProvider) GetReviewComments(ctx context.Context, number int) ([]provider.ExistingComment, error) {
	return f.comment, nil
}

var functionRe = regexp.MustCompile(`Function: (\S+) \(lines`)

func scriptedLLM(t *testing.T, responses map[string]string, requests *[]json.RawMessage) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*requests = append(*requests, json.RawMessage(body))

		var req llm.ChatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("invalid request body: %v", err)
		}

		key := "__context__"
		if !strings.Contains(req.Messages[0].Content, "technical analyst") {
			m := functionRe.FindStringSubmatch(req.Messages[len(req.Messages)-1].Content)
			if m == nil {
				t.Errorf("cannot find function name in prompt")
			} else {
				key = m[1]
			}
		}

		content, ok := responses[key]
		if !ok {
			t.Errorf("no scripted response for %q", key)
			content = "LGTM"
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": content}}},
		})
	}))
}

func TestReviewE2E(t *testing.T) {
	dirs, err := filepath.Glob(filepath.Join("..", "testdata", "mrs", "*"))
	if err != nil || len(dirs) == 0 {
		t.Fatalf("no reference merge requests found: %v", err)
	}

	for _, dir := range dirs {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			var mr mrFixture
			readJSON(t, filepath.Join(dir, "mr.json"), &mr)
			var responses map[string]string
			readJSON(t, filepath.Join(dir, "responses.json"), &responses)

			cfg, err := config.LoadFromPath(filepath.Join(dir, "ainspector.yaml"))
			if err != nil {
				t.Fatal(err)
			}

			fake := &fakeProvider{mr: mr, head: filepath.Join(dir, "head")}
			for _, c := range mr.ExistingComments {
				fake.comment = append(fake.comment, provider.ExistingComment{Path: c.Path, Line: c.Line, Body: c.Body})
			}

			var requests []json.RawMessage
			server := scriptedLLM(t, responses, &requests)
			defer server.Close()

			newClient := func() (*llm.Client, error) {
				return llm.NewClient(server.URL, "key", "test-model"), nil
			}

			if err := review(context.Background(), fake, newClient, cfg, 1, fake.head, mr.Force); err != nil {
				t.Fatal(err)
			}

			got := expectedRun{LLMRequests: requests}
			for _, c := range fake.posted {
				got.Comments = append(got.Comments, expectedComment{Path: c.Path, Line: c.Line, Body: c.Body, Suggestion: c.Suggestion})
			}
			gotJSON, err := json.MarshalIndent(got, "", "  ")
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
