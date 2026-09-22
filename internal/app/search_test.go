package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Global search finds a cached message in any thread and jumps straight to it,
// highlighting it rather than landing on the newest message.
func TestGlobalSearchJumpsToMatch(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	// Open a different thread first, so the jump has to switch threads.
	openThreadByID(t, d, familyThread)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	if cmd != nil {
		t.Fatal("ctrl+f should open the search modal synchronously")
	}
	if m.modal != "search-all" {
		t.Fatalf("modal = %q", m.modal)
	}
	m.searchInput.SetValue("dessert")
	if c := m.runGlobalSearch(); c != nil {
		msg := c()
		if _, next := m.Update(msg); next != nil {
			d.run(next)
		}
	}
	if len(m.globalResults) == 0 {
		t.Fatal("no results for dessert")
	}

	d.run(m.globalSearchKey(tea.KeyMsg{Type: tea.KeyEnter}))
	if m.history.active == nil || m.history.active.ID != amyThread {
		t.Fatalf("jump did not open the matching thread: %+v", m.history.active)
	}
	if m.history.highlightID == "" {
		t.Fatal("jumped-to message was not highlighted")
	}
	sel := m.history.view.Current()
	if sel == nil || !strings.Contains(sel.Body, "dessert") {
		t.Fatalf("selection did not land on the match: %+v", sel)
	}
	// The highlight is temporary: the next navigation clears it.
	d.press("j")
	if m.history.highlightID != "" {
		t.Fatal("highlight survived navigation")
	}
}

func TestGlobalSearchPaletteCommand(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	if cmd, ok := m.searchAction("Search all messages"); !ok || cmd != nil {
		t.Fatal("palette search should open the modal synchronously")
	}
	if m.modal != "search-all" {
		t.Fatalf("modal = %q", m.modal)
	}
}
