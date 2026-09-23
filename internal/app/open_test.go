package app

import (
	"strings"
	"testing"
)

// `tidesms --open ID`, which TideDeck uses, opens that conversation as soon as
// the cache is read, ready to reply.
func TestOpenThreadOnStart(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	m.OpenThreadOnStart(unknownThread)
	syncPhone(t, d)
	d.settle("open", func() bool { return m.history.active != nil })
	if m.history.active.ID != unknownThread || m.history.pane != paneComposer {
		t.Fatalf("opened %q in pane %v", m.history.active.ID, m.history.pane)
	}
}

// A conversation the cache does not hold says so instead of opening nothing.
func TestOpenUnknownThreadSaysSo(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	m.OpenThreadOnStart("thread:phone:999")
	syncPhone(t, d)
	if m.history.active != nil || !strings.Contains(m.notice, "isn't in the cache") {
		t.Fatalf("active %v, notice %q", m.history.active, m.notice)
	}
}
