package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A thread's unsent text shows in the list once you leave it, but not while
// it is being typed, where the composer already shows it.
func TestThreadListShowsDraftsOnceYouLeave(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	m.setPane(paneComposer)
	typeText(m, "running late")
	if _, ok := m.threadDrafts()[amyThread]; ok {
		t.Error("the thread being typed in is listed with a draft")
	}
	m.setPane(paneThreads)
	if got := m.threadDrafts()[amyThread]; got != "running late" {
		t.Fatalf("draft after leaving = %q", got)
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "Draft: running late") {
		t.Errorf("the list does not show the draft:\n%s", view)
	}
}
