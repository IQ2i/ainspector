package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestGitLab(t *testing.T, handler http.HandlerFunc) *GitLabProvider {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return newGitLabProvider(server.URL+"/api/v4", "owner/repo", "token")
}

const gitlabMR = `{"iid":1,"title":"Add feature","sha":"headsha","author":{"username":"alice"},"labels":["bug","no-ai-review"],"diff_refs":{"base_sha":"b","start_sha":"s","head_sha":"h"}}`

func TestGitLab_GetChangeInfo(t *testing.T) {
	p := newTestGitLab(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/projects/owner/repo/merge_requests/1" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(gitlabMR))
	})

	info, err := p.GetChangeInfo(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info.Number != 1 || info.Author != "alice" || info.Title != "Add feature" {
		t.Errorf("unexpected info: %+v", info)
	}
	if len(info.Labels) != 2 || info.Labels[1] != "no-ai-review" {
		t.Errorf("unexpected labels: %v", info.Labels)
	}
	if info.BaseSHA != "b" || info.StartSHA != "s" || info.HeadSHA != "h" {
		t.Errorf("unexpected SHAs: %+v", info)
	}
}

func TestGitLab_CreateReview_ReportsEachComment(t *testing.T) {
	var bodies []map[string]any
	p := newTestGitLab(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(gitlabMR))
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		bodies = append(bodies, body)

		if strings.Contains(fmt.Sprint(body["body"]), "invalid line") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"message":"line_code is invalid"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"d1","notes":[]}`))
	})

	comments := []ReviewComment{
		{Path: "a.go", Line: 3, Body: "first", Suggestion: "fixed()"},
		{Path: "b.go", Line: 9, Body: "invalid line"},
		{Path: "c.go", Line: 1, Body: "third"},
	}
	results, err := p.CreateReview(context.Background(), 1, comments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].Err != nil || results[1].Err == nil || results[2].Err != nil {
		t.Errorf("expected only the second comment to fail, got %v / %v / %v", results[0].Err, results[1].Err, results[2].Err)
	}
	if results[1].Comment.Path != "b.go" {
		t.Errorf("result must carry its comment, got %+v", results[1].Comment)
	}
	if len(bodies) != 3 {
		t.Fatalf("a failing comment must not stop the batch, got %d requests", len(bodies))
	}
	if !strings.Contains(fmt.Sprint(bodies[0]["body"]), "```suggestion:-0+0\nfixed()\n```") {
		t.Errorf("expected suggestion block, got %v", bodies[0]["body"])
	}
	position, _ := bodies[0]["position"].(map[string]any)
	if position["new_path"] != "a.go" || position["head_sha"] != "h" || position["base_sha"] != "b" || position["start_sha"] != "s" || position["new_line"] != float64(3) {
		t.Errorf("unexpected position: %v", position)
	}
}

func TestGitLab_CreateReview_MultiLineSuggestion(t *testing.T) {
	var bodies []string
	p := newTestGitLab(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(gitlabMR))
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		bodies = append(bodies, fmt.Sprint(body["body"]))
		_, _ = w.Write([]byte(`{"id":"d1","notes":[]}`))
	})

	_, err := p.CreateReview(context.Background(), 1, []ReviewComment{
		{Path: "a.go", Line: 3, EndLine: 5, Body: "range", Suggestion: "fixed()"},
		{Path: "b.go", Line: 4, EndLine: 6, Body: "plain text"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(bodies[0], "```suggestion:-0+2\nfixed()\n```") {
		t.Errorf("the suggestion must replace the two lines below the first, got %q", bodies[0])
	}
	if strings.Contains(bodies[1], "suggestion") {
		t.Errorf("no suggestion expected, got %q", bodies[1])
	}
}

func TestGitLab_CreateReview_NoComments(t *testing.T) {
	p := newTestGitLab(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request expected")
	})

	results, err := p.CreateReview(context.Background(), 1, nil)
	if err != nil || len(results) != 0 {
		t.Errorf("expected nothing, got %v / %v", results, err)
	}
}

func TestGitLab_PostAndUpdateComment(t *testing.T) {
	var updated string
	p := newTestGitLab(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]string
		_ = json.Unmarshal(raw, &body)

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v4/projects/owner/repo/merge_requests/1/notes":
			_, _ = w.Write([]byte(`{"id":42,"body":"` + body["body"] + `"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v4/projects/owner/repo/merge_requests/1/notes/42":
			updated = body["body"]
			_, _ = w.Write([]byte(`{"id":42}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	id, err := p.PostComment(context.Background(), 1, "hello")
	if err != nil || id != "42" {
		t.Fatalf("expected id 42, got %q / %v", id, err)
	}
	if err := p.UpdateComment(context.Background(), 1, id, "updated"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated != "updated" {
		t.Errorf("expected updated body, got %q", updated)
	}
	if err := p.UpdateComment(context.Background(), 1, "not-a-number", "x"); err == nil {
		t.Error("expected an error for an invalid note ID")
	}
}

func TestGitLab_ListComments(t *testing.T) {
	p := newTestGitLab(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[
			{"id":"d1","notes":[{"id":10,"body":"inline","system":false,"author":{"username":"alice"},"position":{"new_path":"a.go","new_line":7}}]},
			{"id":"d2","notes":[{"id":11,"body":"general","system":false,"author":{"username":"bob"}}]},
			{"id":"d3","notes":[{"id":12,"body":"added 1 commit","system":true}]}
		]`))
	})

	got, err := p.ListComments(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 comments (system note excluded), got %d: %+v", len(got), got)
	}
	if !got[0].Positioned || got[0].Path != "a.go" || got[0].Line != 7 || got[0].ID != "10" {
		t.Errorf("unexpected inline comment: %+v", got[0])
	}
	if got[0].Author != "alice" || got[1].Author != "bob" {
		t.Errorf("unexpected authors: %q, %q", got[0].Author, got[1].Author)
	}
	if got[1].Positioned || got[1].Body != "general" || got[1].ID != "11" {
		t.Errorf("unexpected general comment: %+v", got[1])
	}
}

func TestGitLab_UninitializedClient(t *testing.T) {
	p := &GitLabProvider{}
	ctx := context.Background()

	if _, err := p.GetChangeInfo(ctx, 1); err == nil {
		t.Error("GetChangeInfo should fail")
	}
	if _, err := p.PostComment(ctx, 1, "x"); err == nil {
		t.Error("PostComment should fail")
	}
	if err := p.UpdateComment(ctx, 1, "1", "x"); err == nil {
		t.Error("UpdateComment should fail")
	}
	if _, err := p.CreateReview(ctx, 1, []ReviewComment{{}}); err == nil {
		t.Error("CreateReview should fail")
	}
	if _, err := p.ListComments(ctx, 1); err == nil {
		t.Error("ListComments should fail")
	}
	if _, err := p.CurrentUser(ctx); err == nil {
		t.Error("CurrentUser should fail")
	}
}

func TestGitLab_CurrentUser(t *testing.T) {
	p := newTestGitLab(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/user" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":5,"username":"project_1_bot"}`))
	})

	got, err := p.CurrentUser(context.Background())
	if err != nil || got != "project_1_bot" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestGitLab_ListReactions(t *testing.T) {
	p := newTestGitLab(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/projects/owner/repo/merge_requests/1/notes/10/award_emoji":
			_, _ = w.Write([]byte(`[{"name":"thumbsup"},{"name":"thumbsdown"},{"name":"thumbsdown"},{"name":"tada"}]`))
		case "/api/v4/projects/owner/repo/merge_requests/1/notes/11/award_emoji":
			w.WriteHeader(http.StatusForbidden)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})

	got, err := p.ListReactions(context.Background(), 1, []string{"10", "11"})
	if err == nil || !strings.Contains(err.Error(), "note 11") {
		t.Errorf("the unreadable note should be reported, got %v", err)
	}
	if len(got) != 1 || got["10"] != (ReactionCount{Up: 1, Down: 2}) {
		t.Errorf("unexpected counts: %+v", got)
	}
}

func TestGitLab_CreateReview_WithoutDiffRefsReportsEveryComment(t *testing.T) {
	posted := 0
	p := newTestGitLab(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posted++
		}
		_, _ = w.Write([]byte(`{"iid":1,"title":"t","diff_refs":{"base_sha":"","start_sha":"","head_sha":""}}`))
	})

	results, err := p.CreateReview(context.Background(), 1, []ReviewComment{{Path: "a.go", Line: 3, Body: "x"}, {Path: "b.go", Line: 4, Body: "y"}})
	if err != nil {
		t.Fatal(err)
	}
	if posted != 0 {
		t.Errorf("no discussion can be positioned without diff_refs, %d were posted", posted)
	}
	if len(results) != 2 || results[0].Err == nil || results[1].Err == nil || results[1].Comment.Path != "b.go" {
		t.Errorf("every comment must come back with an error so it is posted as a general note: %+v", results)
	}
}
