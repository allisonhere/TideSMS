package components

import (
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/charmbracelet/x/ansi"
)

func threadRows(ts []domain.Thread, drafts map[string]string) string {
	return ansi.Strip(Threads(testRenderer(), ts, "", 40, 20, nil, drafts))
}

// An unread thread keeps its time beside the count.
func TestUnreadRowKeepsItsTime(t *testing.T) {
	at := time.Now().Add(-time.Minute)
	out := threadRows([]domain.Thread{{ID: "t", DisplayName: "Amy", LastMessage: "hi", LastTimestamp: at, UnreadCount: 2}}, nil)
	if want := "2 · " + at.Local().Format("15:04"); !strings.Contains(out, want) {
		t.Errorf("unread row lacks %q:\n%s", want, out)
	}
}

// A thread with unsent text shows it in place of the last message, labelled,
// and on one line however the draft was broken.
func TestDraftReplacesThePreview(t *testing.T) {
	ts := []domain.Thread{
		{ID: "a", DisplayName: "Amy", LastMessage: "see you", LastTimestamp: time.Now()},
		{ID: "b", DisplayName: "Bo", LastMessage: "ok", LastTimestamp: time.Now()},
	}
	out := threadRows(ts, map[string]string{"a": "running\nlate"})
	if !strings.Contains(out, "Draft: running late") || strings.Contains(out, "see you") {
		t.Errorf("draft not shown in place of the preview:\n%s", out)
	}
	if !strings.Contains(out, "ok") || strings.Count(out, "Draft:") != 1 {
		t.Errorf("a thread without a draft changed:\n%s", out)
	}
}

// A message with no text still says something in the list.
func TestEmptyPreviewNamesTheAttachment(t *testing.T) {
	out := threadRows([]domain.Thread{{ID: "t", DisplayName: "Amy", LastTimestamp: time.Now()}}, nil)
	if !strings.Contains(out, "Attachment") {
		t.Errorf("empty preview left blank:\n%s", out)
	}
}
