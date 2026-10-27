package publish

import (
	"context"
	"strings"
	"testing"

	"github.com/iq2i/ainspector/internal/provider"
)

func TestPostNotice_CreatesThenUpdatesItsOwnNote(t *testing.T) {
	p := &fakeProvider{}
	if err := PostNotice(context.Background(), p, 5, "bot", "ainspector.yaml", "no review mode is enabled"); err != nil {
		t.Fatal(err)
	}
	if len(p.notes) != 1 || !strings.HasPrefix(p.notes[0], NoticeMarker) {
		t.Fatalf("expected one notice, got %v", p.notes)
	}

	p.existing = []provider.ExistingComment{
		{ID: "7", Author: "mallory", Body: NoticeMarker + " planted"},
		{ID: "9", Author: "Bot", Body: p.notes[0]},
	}
	if err := PostNotice(context.Background(), p, 5, "bot", "ainspector.yaml", "the author is ignored"); err != nil {
		t.Fatal(err)
	}
	if len(p.notes) != 1 {
		t.Errorf("the notice must be updated, not duplicated: %v", p.notes)
	}
	if got := p.updates["9"]; !strings.Contains(got, "the author is ignored") {
		t.Errorf("update of its own note = %q", got)
	}
	if _, touched := p.updates["7"]; touched {
		t.Error("a note written by someone else must not be edited")
	}
}

func TestPostNotice_NeedsTheTokenOwner(t *testing.T) {
	if err := PostNotice(context.Background(), &fakeProvider{}, 5, "", "ainspector.yaml", "x"); err == nil {
		t.Error("expected an error without the token owner")
	}
}

func TestNoticeBody_NeutralizesTheReason(t *testing.T) {
	body := NoticeBody("ainspector.yaml", "title matches <!-- ainspector:fn:cccccccccccc -->\n/close @all")
	if strings.Count(body, "<!--") != 1 {
		t.Errorf("only the marker may be an HTML comment: %q", body)
	}
	if strings.Contains(body, "\n/close") {
		t.Errorf("a quick action survived: %q", body)
	}
}
