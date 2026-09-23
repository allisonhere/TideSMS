package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Headings are drawn but never landed on: selection addresses fields, so
// moving through the panel must never stop on a title.
func TestSettingsHeadingsAreNotSelectable(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	d.run(m.action("Open settings"))

	fields := m.settingsFields()
	lines := m.settingsLines()
	headings := 0
	for _, line := range lines {
		if line.heading != "" {
			headings++
		}
	}
	if headings < 3 {
		t.Fatalf("got %d headings, want the panel divided", headings)
	}
	if len(lines) != len(fields)+headings {
		t.Fatalf("lines %d, fields %d, headings %d: every field must be drawn exactly once", len(lines), len(fields), headings)
	}
	// Field indices run 0..n-1 in drawn order, so the cursor and the drawing
	// agree about which row is which.
	want := 0
	for _, line := range lines {
		if line.heading != "" {
			continue
		}
		if line.index != want {
			t.Fatalf("field index %d out of order, want %d", line.index, want)
		}
		want++
	}

	// Walking the whole panel lands on every field and nothing else.
	m.choice = 0
	for i := 0; i < len(fields)+5; i++ {
		f, ok := m.selectedSetting()
		if !ok {
			t.Fatal("no selectable row")
		}
		if f.label == "" {
			t.Fatal("landed on a row with no label")
		}
		d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyDown}))
	}
	if m.choice != len(fields)-1 {
		t.Errorf("moving down stopped at %d, want the last field %d", m.choice, len(fields)-1)
	}
}

// Each group's rows appear under its own heading, in order.
func TestSettingsRowsSitUnderTheirHeading(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	m.Update(tea.WindowSizeMsg{Width: 150, Height: 46})
	d.run(m.action("Open settings"))

	view := ansi.Strip(m.View())
	assistant := strings.Index(view, "ASSISTANT")
	sending := strings.Index(view, "SENDING")
	provider := strings.Index(view, "Provider")
	background := strings.Index(view, "Background sending")
	if assistant < 0 || sending < 0 || provider < 0 || background < 0 {
		t.Fatalf("panel is missing its groups:\n%s", view)
	}
	if !(assistant < provider && provider < sending && sending < background) {
		t.Errorf("rows are not under their own headings:\n%s", view)
	}
}

// A window too short for the panel scrolls, keeping the selected row drawn and
// its heading with it.
func TestSettingsScrollKeepsTheSelectionAndItsHeading(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	d.run(m.action("Open settings"))
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})

	selectSetting(t, m, "Background sending")
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "more") {
		t.Fatalf("a short window did not scroll:\n%s", view)
	}
	if !strings.Contains(view, "Background sending") {
		t.Fatalf("the selection scrolled out of view:\n%s", view)
	}
	if !strings.Contains(view, "SENDING") {
		t.Errorf("the selected row lost its heading:\n%s", view)
	}
}

// Settings are reachable by key, not only through the command palette.
func TestSettingsOpenByKey(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	d.settle("history", func() bool { return len(m.history.view.Messages) > 0 })

	comma := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{','}}
	for _, p := range []int{paneThreads, paneConversation, paneContacts} {
		m.modal = ""
		m.setPane(p)
		m.Update(comma)
		if m.modal != "settings" {
			t.Errorf("comma did not open settings from pane %d: modal=%q", p, m.modal)
		}
	}

	// The composer takes a comma as text, so the chord is what works there.
	m.modal = ""
	m.setPane(paneComposer)
	m.Update(comma)
	if m.modal == "settings" {
		t.Error("a comma typed into the composer opened settings")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	if m.modal != "settings" {
		t.Errorf("Ctrl+O did not open settings from the composer: modal=%q", m.modal)
	}
}
