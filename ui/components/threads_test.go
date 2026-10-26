package components

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/charmbracelet/x/ansi"
)

func threadRows(ts []domain.Thread, drafts map[string]string) string {
	return ansi.Strip(Threads(testRenderer(), ThreadView{Threads: ts, Width: 40, Height: 20, Drafts: drafts}))
}

// An unread thread keeps its time as well as its count.
func TestUnreadRowKeepsItsTime(t *testing.T) {
	at := time.Now()
	out := threadRows([]domain.Thread{{ID: "t", DisplayName: "Amy", LastMessage: "hi", LastTimestamp: at, UnreadCount: 2}}, nil)
	lines := strings.Split(out, "\n")
	if want := at.Local().Format("15:04"); !strings.Contains(lines[0], want) {
		t.Errorf("unread row lacks its time %q:\n%s", want, out)
	}
	if !strings.HasSuffix(strings.TrimRight(lines[1], " "), " 2") {
		t.Errorf("unread row lacks its count:\n%s", out)
	}
	if !strings.Contains(out, "1 unread") {
		t.Errorf("footer does not count the unread thread:\n%s", out)
	}
}

// Timestamps read the way a messaging list reads them.
func TestWhen(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.Local)
	for _, c := range []struct {
		at   time.Time
		want string
	}{
		{now.Add(-time.Hour), "11:00"},
		{now.AddDate(0, 0, -1), "Yesterday"},
		{now.AddDate(0, 0, -3), "Mon"},
		{now.AddDate(0, -2, 0), "Aug 15"},
		{now.AddDate(-1, 0, 0), "Oct 2025"},
	} {
		if got := when(c.at, now); got != c.want {
			t.Errorf("when(%v) = %q, want %q", c.at, got, c.want)
		}
	}
}

// Monograms take the first and last initials, and say "#" for a number.
func TestMonogram(t *testing.T) {
	for name, want := range map[string]string{"Amy Rivera": "AR", "Mom": "M", "Mary Jo Smith": "MS", "+1 555 0100": "#", "": "#"} {
		if got := monogram(name); got != want {
			t.Errorf("monogram(%q) = %q, want %q", name, got, want)
		}
	}
}

// Pinned threads sit under their own heading, above the rest.
func TestPinnedThreadsAreHeaded(t *testing.T) {
	ts := []domain.Thread{
		{ID: "a", DisplayName: "Amy", LastTimestamp: time.Now(), Pinned: true},
		{ID: "b", DisplayName: "Bo", LastTimestamp: time.Now()},
	}
	out := threadRows(ts, nil)
	pinned, recent, bo := strings.Index(out, "Pinned"), strings.Index(out, "Recent"), strings.Index(out, "Bo")
	if pinned < 0 || recent < pinned || bo < recent {
		t.Errorf("pinned section missing or out of order:\n%s", out)
	}
	if strings.Contains(threadRows(ts[1:], nil), "Pinned") {
		t.Error("a list with nothing pinned grew a heading")
	}
}

// The selected thread stays on screen however far down the list it is.
func TestSelectionScrollsIntoView(t *testing.T) {
	var ts []domain.Thread
	for i := range 20 {
		ts = append(ts, domain.Thread{ID: fmt.Sprint(i), DisplayName: fmt.Sprintf("Person %d", i), LastTimestamp: time.Now()})
	}
	out := ansi.Strip(Threads(testRenderer(), ThreadView{Threads: ts, Selected: "17", Width: 40, Height: 12}))
	if !strings.Contains(out, "Person 17") {
		t.Errorf("selected thread scrolled off:\n%s", out)
	}
	if n := len(strings.Split(out, "\n")); n != 12 {
		t.Errorf("list is %d lines, want the pane's 12", n)
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

// A compact list drops the blank line between entries and nothing else.
func TestCompactListDropsTheGap(t *testing.T) {
	ts := []domain.Thread{
		{ID: "a", DisplayName: "Amy", LastMessage: "one", LastTimestamp: time.Now()},
		{ID: "b", DisplayName: "Bo", LastMessage: "two", LastTimestamp: time.Now()},
	}
	lines := func(compact bool) []string {
		return strings.Split(ansi.Strip(Threads(testRenderer(), ThreadView{Threads: ts, Width: 40, Height: 20, Compact: compact})), "\n")
	}
	standard, compact := lines(false), lines(true)
	if !strings.Contains(standard[3], "Bo") {
		t.Errorf("standard spacing: second entry not after a blank line:\n%s", strings.Join(standard, "\n"))
	}
	if !strings.Contains(compact[2], "Bo") || !strings.Contains(compact[3], "two") {
		t.Errorf("compact spacing: second entry not directly below the first:\n%s", strings.Join(compact, "\n"))
	}
	if len(compact) != 20 {
		t.Errorf("compact list is %d lines, want the pane's 20", len(compact))
	}
}

// An open conversation is headed like its row: chip, name, and the number.
func TestConversationHeader(t *testing.T) {
	lines := ConversationHeader(testRenderer(), "Amy Rivera", "+1555000001", "nord", 50)
	if len(lines) != 2 {
		t.Fatalf("header is %d lines, want 2", len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != 50 {
			t.Errorf("line %d is %d cells, want 50", i, w)
		}
	}
	if !strings.Contains(ansi.Strip(lines[0]), "AR") || !strings.Contains(ansi.Strip(lines[0]), "Amy Rivera") || !strings.Contains(ansi.Strip(lines[1]), "+1555000001") {
		t.Errorf("header lost its content:\n%s", ansi.Strip(strings.Join(lines, "\n")))
	}
}
