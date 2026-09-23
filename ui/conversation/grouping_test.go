package conversation

import (
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tidesms/internal/themes"
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/x/ansi"
)

var runStart = time.Date(2026, 9, 22, 10, 0, 0, 0, time.Local)

// at is a message minutes after runStart.
func at(dir domain.Direction, body string, minutes int) domain.Message {
	msg := message(dir, body)
	msg.Timestamp = runStart.Add(time.Duration(minutes) * time.Minute)
	if dir == domain.Outgoing {
		msg.Sender = "You"
	}
	return msg
}

// laidOut lays out messages tall enough to show every line, and returns the
// model with its plain-text lines.
func laidOut(t *testing.T, ms []domain.Message, o Options) (*Model, []string) {
	t.Helper()
	r := tideui.NewRenderer(themes.Resolve("tide", "", ""), tideui.StyleOptions{})
	m := New()
	m.SetMessages(ms)
	o.Timestamps = "smart"
	m.Layout(r, 80, 200, o)
	return &m, plain(m.lines)
}

func plain(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimRight(ansi.Strip(l), " ")
	}
	return out
}

func count(lines []string, s string) int {
	n := 0
	for _, l := range lines {
		if strings.Contains(l, s) {
			n++
		}
	}
	return n
}

// Messages in quick succession from one sender share a single header and sit
// together without gaps; the run ends with one blank line.
func TestARunSharesOneHeader(t *testing.T) {
	_, lines := laidOut(t, []domain.Message{
		at(domain.Incoming, "one", 0), at(domain.Incoming, "two", 1), at(domain.Incoming, "three", 4),
	}, Options{})
	if n := count(lines, "10:0"); n != 1 {
		t.Fatalf("a run drew %d headers:\n%s", n, strings.Join(lines, "\n"))
	}
	blanks := 0
	for _, l := range lines {
		if l == "" {
			blanks++
		}
	}
	if blanks != 1 {
		t.Errorf("a run drew %d blank lines, want only the one after it:\n%s", blanks, strings.Join(lines, "\n"))
	}
}

// A long pause, a change of direction, a new day and the unread boundary each
// start a new run.
func TestRunsBreak(t *testing.T) {
	nextDay := at(domain.Incoming, "tomorrow", 24*60)
	unread := at(domain.Incoming, "unread", 1)
	unread.Unread = true
	for name, ms := range map[string][]domain.Message{
		"pause":     {at(domain.Incoming, "a", 0), at(domain.Incoming, "b", 6)},
		"direction": {at(domain.Incoming, "a", 0), at(domain.Outgoing, "b", 1)},
		"day":       {at(domain.Incoming, "a", 0), nextDay},
		"unread":    {at(domain.Incoming, "a", 0), unread},
	} {
		m, _ := laidOut(t, ms, Options{})
		if s := m.shapes(); !s[1].header {
			t.Errorf("%s: the second message continued the first", name)
		}
	}
}

// Only the newest outgoing message states a settled status; one that is still
// sending or has failed keeps its own wherever it is.
func TestStatusOnlyWhereItSaysSomething(t *testing.T) {
	failed := at(domain.Outgoing, "b", 1)
	failed.Status = domain.Failed
	_, lines := laidOut(t, []domain.Message{
		at(domain.Outgoing, "a", 0), failed, at(domain.Outgoing, "c", 2),
		at(domain.Incoming, "reply", 3),
	}, Options{})
	if n := count(lines, "sent"); n != 1 {
		t.Errorf("sent shown %d times, want once:\n%s", n, strings.Join(lines, "\n"))
	}
	if n := count(lines, "failed"); n != 1 {
		t.Errorf("a failed message lost its status:\n%s", strings.Join(lines, "\n"))
	}
}

// One-to-one, incoming headers carry only the time; a group names the sender.
func TestHeadersNameTheSenderOnlyInGroups(t *testing.T) {
	msg := at(domain.Incoming, "hi", 0)
	names := map[string]string{"+15551234567": "Amy"}
	if _, lines := laidOut(t, []domain.Message{msg}, Options{Names: names}); count(lines, "Amy") != 0 {
		t.Errorf("a one-to-one header repeated the name:\n%s", strings.Join(lines, "\n"))
	}
	if _, lines := laidOut(t, []domain.Message{msg}, Options{Names: names, Group: true}); count(lines, "Amy · 10:00") != 1 {
		t.Errorf("a group header lost the sender:\n%s", strings.Join(lines, "\n"))
	}
}

// A message jumped to keeps its header even in the middle of a run.
func TestHighlightKeepsTheHeader(t *testing.T) {
	ms := []domain.Message{at(domain.Incoming, "a", 0), at(domain.Incoming, "b", 1)}
	m, _ := laidOut(t, ms, Options{HighlightID: ms[1].ID})
	if !m.shapes()[1].header {
		t.Error("the highlighted message lost its header")
	}
}

// A cached message is redrawn when a newer one changes its shape: the previous
// newest outgoing message loses its status once another follows it.
func TestCacheFollowsTheShape(t *testing.T) {
	r := tideui.NewRenderer(themes.Resolve("tide", "", ""), tideui.StyleOptions{})
	m := New()
	first := at(domain.Outgoing, "first", 0)
	m.SetMessages([]domain.Message{first})
	m.Layout(r, 80, 200, Options{Timestamps: "smart"})
	if count(plain(m.lines), "sent") != 1 {
		t.Fatal("the only outgoing message shows no status")
	}
	m.SetMessages([]domain.Message{first, at(domain.Outgoing, "second", 30)})
	m.Layout(r, 80, 200, Options{Timestamps: "smart"})
	if n := count(plain(m.lines), "sent"); n != 1 {
		t.Errorf("a stale cached status survived: sent shown %d times:\n%s", n, strings.Join(plain(m.lines), "\n"))
	}
}
