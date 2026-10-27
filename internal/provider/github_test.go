package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/go-github/v57/github"
)

func newTestGitHub(t *testing.T, handler http.HandlerFunc) *GitHubProvider {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	p := NewGitHubProvider("owner", "repo", "token")
	base, err := url.Parse(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	p.client.BaseURL = base
	p.headSHA = "headsha"
	return p
}

func TestGitHub_GetChangeInfo(t *testing.T) {
	p := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/pulls/1" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"number":1,"title":"Add feature","user":{"login":"alice"},"labels":[{"name":"bug"},{"name":"no-ai-review"}],"base":{"sha":"b"},"head":{"sha":"h"}}`))
	})

	info, err := p.GetChangeInfo(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info.Author != "alice" || info.Title != "Add feature" || info.BaseSHA != "b" || info.HeadSHA != "h" {
		t.Errorf("unexpected info: %+v", info)
	}
	if len(info.Labels) != 2 || info.Labels[1] != "no-ai-review" {
		t.Errorf("unexpected labels: %v", info.Labels)
	}
}

func TestGitHub_CreateReview(t *testing.T) {
	var review github.PullRequestReviewRequest
	p := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/pulls/1/reviews" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &review)
		_, _ = w.Write([]byte(`{"id":1}`))
	})

	results, err := p.CreateReview(context.Background(), 1, []ReviewComment{
		{Path: "a.go", Line: 3, Body: "first", Suggestion: "fixed()"},
		{Path: "b.go", Line: 4, Body: "second"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 2 || results[0].Err != nil || results[1].Err != nil {
		t.Fatalf("expected 2 successful results, got %+v", results)
	}
	if review.GetCommitID() != "headsha" || review.GetEvent() != "COMMENT" || len(review.Comments) != 2 {
		t.Errorf("unexpected review: %+v", review)
	}
	if !strings.Contains(review.Comments[0].GetBody(), "```suggestion\nfixed()\n```") {
		t.Errorf("expected suggestion block, got %q", review.Comments[0].GetBody())
	}
}

func TestGitHub_CreateReview_MultiLineSuggestion(t *testing.T) {
	var review github.PullRequestReviewRequest
	p := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &review)
		_, _ = w.Write([]byte(`{"id":1}`))
	})

	_, err := p.CreateReview(context.Background(), 1, []ReviewComment{
		{Path: "a.go", Line: 3, EndLine: 5, Body: "range", Suggestion: "fixed()"},
		{Path: "b.go", Line: 4, EndLine: 6, Body: "no suggestion"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if c := review.Comments[0]; c.GetStartLine() != 3 || c.GetLine() != 5 {
		t.Errorf("a range must go from start_line to line, got %d-%d", c.GetStartLine(), c.GetLine())
	}
	if c := review.Comments[1]; c.StartLine != nil || c.GetLine() != 4 {
		t.Errorf("a comment without suggestion stays on its line, got %+v", c)
	}
}

func TestGitHub_CreateReview_FailureIsReportedOnEveryComment(t *testing.T) {
	p := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"Line must be part of the diff"}`))
	})

	results, err := p.CreateReview(context.Background(), 1, []ReviewComment{{Path: "a.go", Line: 3, Body: "x"}, {Path: "b.go", Line: 4, Body: "y"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 2 || results[0].Err == nil || results[1].Err == nil {
		t.Errorf("expected the error on both comments, got %+v", results)
	}
	if results[1].Comment.Path != "b.go" {
		t.Errorf("result must carry its comment, got %+v", results[1].Comment)
	}
}

func TestGitHub_CreateReview_NoComments(t *testing.T) {
	p := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request expected")
	})

	if results, err := p.CreateReview(context.Background(), 1, nil); err != nil || len(results) != 0 {
		t.Errorf("expected nothing, got %v / %v", results, err)
	}
}

func TestGitHub_PostAndUpdateComment(t *testing.T) {
	var updated string
	p := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]string
		_ = json.Unmarshal(raw, &body)

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/issues/1/comments":
			_, _ = w.Write([]byte(`{"id":7}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/owner/repo/issues/comments/7":
			updated = body["body"]
			_, _ = w.Write([]byte(`{"id":7}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	id, err := p.PostComment(context.Background(), 1, "hello")
	if err != nil || id != "7" {
		t.Fatalf("expected id 7, got %q / %v", id, err)
	}
	if err := p.UpdateComment(context.Background(), 1, id, "updated"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated != "updated" {
		t.Errorf("expected updated body, got %q", updated)
	}
	if err := p.UpdateComment(context.Background(), 1, "not-a-number", "x"); err == nil {
		t.Error("expected an error for an invalid comment ID")
	}
}

func TestGitHub_ListComments(t *testing.T) {
	p := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/pulls/1/comments":
			_, _ = w.Write([]byte(`[{"id":1,"path":"a.go","line":7,"body":"inline","user":{"login":"alice"}}]`))
		case "/repos/owner/repo/issues/1/comments":
			_, _ = w.Write([]byte(`[{"id":2,"body":"general","user":{"login":"bob"}}]`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})

	got, err := p.ListComments(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 comments, got %+v", got)
	}
	if !got[0].Positioned || got[0].Path != "a.go" || got[0].Line != 7 || got[0].ID != "1" {
		t.Errorf("unexpected inline comment: %+v", got[0])
	}
	if got[0].Author != "alice" || got[1].Author != "bob" {
		t.Errorf("unexpected authors: %q, %q", got[0].Author, got[1].Author)
	}
	if got[1].Positioned || got[1].Body != "general" || got[1].ID != "2" {
		t.Errorf("unexpected general comment: %+v", got[1])
	}
}

func TestGitHub_CreateReview_RetriesEachCommentOnRejection(t *testing.T) {
	var reviews int
	p := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		var review github.PullRequestReviewRequest
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &review)
		reviews++

		for _, c := range review.Comments {
			if c.GetPath() == "bad.go" {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(`{"message":"Line must be part of the diff"}`))
				return
			}
		}
		_, _ = w.Write([]byte(`{"id":1}`))
	})

	comments := []ReviewComment{{Path: "a.go", Line: 1, Body: "x"}, {Path: "bad.go", Line: 2, Body: "y"}, {Path: "c.go", Line: 3, Body: "z"}}
	results, err := p.CreateReview(context.Background(), 1, comments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 3 || results[0].Err != nil || results[1].Err == nil || results[2].Err != nil {
		t.Errorf("expected only the second comment to fail, got %+v", results)
	}
	if reviews != 4 {
		t.Errorf("expected 1 batch + 3 single reviews, got %d requests", reviews)
	}
}

func TestGitHub_CurrentUser(t *testing.T) {
	p := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"login":"ainspector-bot"}`))
	})

	got, err := p.CurrentUser(context.Background())
	if err != nil || got != "ainspector-bot" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestGitHub_CurrentUser_ActionsTokenFallback(t *testing.T) {
	p := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Resource not accessible by integration"}`))
	})

	got, err := p.CurrentUser(context.Background())
	if err != nil || got != "github-actions[bot]" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestGitHub_CurrentUser_OtherErrorIsReported(t *testing.T) {
	p := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	if _, err := p.CurrentUser(context.Background()); err == nil {
		t.Error("expected an error")
	}
}

func TestGitHub_ListReactions(t *testing.T) {
	p := newTestGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/pulls/comments/10/reactions":
			_, _ = w.Write([]byte(`[{"content":"+1"},{"content":"+1"},{"content":"-1"},{"content":"heart"}]`))
		case "/repos/owner/repo/pulls/comments/11/reactions":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})

	got, err := p.ListReactions(context.Background(), 1, []string{"10", "11", "oops"})
	if err == nil || !strings.Contains(err.Error(), "comment 11") || !strings.Contains(err.Error(), "oops") {
		t.Errorf("the unreadable comments should be reported, got %v", err)
	}
	if len(got) != 1 || got["10"] != (ReactionCount{Up: 2, Down: 1}) {
		t.Errorf("unexpected counts: %+v", got)
	}
}
