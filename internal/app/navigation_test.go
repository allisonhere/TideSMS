package app

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func navigationKey(m *Model, key string) tea.Cmd {
	k := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	switch key {
	case "tab":
		k = tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		k = tea.KeyMsg{Type: tea.KeyShiftTab}
	case "esc":
		k = tea.KeyMsg{Type: tea.KeyEsc}
	case "alt+esc":
		k = tea.KeyMsg{Type: tea.KeyEsc, Alt: true}
	case "enter":
		k = tea.KeyMsg{Type: tea.KeyEnter}
	case "alt+1", "alt+2", "alt+3":
		k = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key[4:]), Alt: true}
	}
	_, cmd := m.Update(k)
	return cmd
}

func TestKeyboardPaneRoutesAndFocusPreservation(t *testing.T) {
	for _, width := range []int{55, 120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m, _, _, d, _ := conversationFixture(t)
			syncPhone(t, d)
			openThreadByID(t, d, amyThread)
			m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
			m.cfg.Composer.Mode = "normal"
			m.editor.SetMode("normal")
			m.editor.SetValue("draft")
			m.setPane(paneComposer)
			m.Update(tea.KeyMsg{Type: tea.KeyLeft})
			m.history.view.Move(-1)
			selected, offset := m.history.view.Selected, m.history.view.Offset
			seq := m.history.cacheSeq
			steps := []struct {
				key  string
				pane int
			}{
				{"tab", paneThreads}, {"tab", paneComposer}, {"shift+tab", paneThreads}, {"shift+tab", paneComposer},
				{"alt+2", paneConversation}, {"shift+tab", paneThreads}, {"alt+2", paneConversation}, {"tab", paneComposer},
				{"alt+1", paneThreads}, {"c", paneContacts}, {"tab", paneComposer}, {"tab", paneContacts},
				{"alt+1", paneThreads}, {"alt+3", paneComposer},
			}
			for _, step := range steps {
				if cmd := navigationKey(m, step.key); cmd != nil {
					t.Fatalf("focus key %s triggered async work", step.key)
				}
				if m.history.pane != step.pane || m.focus != (step.pane == paneComposer) {
					t.Fatalf("%s focused %d, want %d", step.key, m.history.pane, step.pane)
				}
				if m.editor.Value() != "draft" {
					t.Fatal("focus change modified draft")
				}
				view := ansi.Strip(m.View())
				if len(strings.Split(view, "\n")) > 30 {
					t.Fatal("focus label overflowed window")
				}
				if step.pane == paneComposer && !strings.Contains(view, "▸ Compose") {
					t.Fatal("composer focus not labelled")
				}
				if step.pane == paneConversation && !strings.Contains(view, "▸ History") {
					t.Fatal("history focus not labelled")
				}
			}
			if m.history.cacheSeq != seq || m.history.view.Selected != selected || m.history.view.Offset != offset {
				t.Fatal("focus navigation reloaded or scrolled history")
			}
			typeText(m, "X")
			if m.editor.Value() != "drafXt" {
				t.Fatalf("caret moved during navigation: %q", m.editor.Value())
			}
			if !m.editor.Undo() || m.editor.Value() != "draft" {
				t.Fatal("undo did not survive navigation")
			}
		})
	}
}

func TestPaneNavigationBackAndInputPrecedence(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	m.setPane(paneComposer)
	d.run(navigationKey(m, "esc"))
	if m.history.pane != paneConversation {
		t.Fatal("Esc did not leave composer")
	}
	d.run(navigationKey(m, "esc"))
	if m.history.pane != paneThreads {
		t.Fatal("Esc did not return to threads")
	}
	d.run(navigationKey(m, "enter"))
	if m.history.pane != paneComposer {
		t.Fatal("opening thread did not focus composer")
	}
	d.run(navigationKey(m, "alt+2"))
	d.run(navigationKey(m, "/"))
	navigationKey(m, "alt+3")
	if m.history.pane != paneConversation {
		t.Fatal("shortcut escaped search input")
	}
	d.run(navigationKey(m, "esc"))
	if m.history.search || m.history.pane != paneConversation {
		t.Fatal("search Esc left history")
	}
	m.modal = "help"
	navigationKey(m, "alt+1")
	if m.history.pane != paneConversation {
		t.Fatal("pane shortcut escaped modal")
	}
	navigationKey(m, "esc")
	if m.modal != "" || m.history.pane != paneConversation {
		t.Fatal("modal Esc changed pane")
	}
	m.cfg.Composer.Mode = "vim"
	m.editor.SetMode("vim")
	m.focusArea(paneComposer)
	navigationKey(m, "i")
	navigationKey(m, "esc")
	if m.history.pane != paneComposer {
		t.Fatal("first Vim Esc left composer")
	}
	d.run(navigationKey(m, "esc"))
	if m.history.pane != paneConversation {
		t.Fatal("second Vim Esc did not leave composer")
	}
	m.focusArea(paneComposer)
	navigationKey(m, "i")
	d.run(navigationKey(m, "alt+esc"))
	if m.history.pane != paneConversation {
		t.Fatal("Alt+Esc did not leave Vim composer")
	}
}

func TestPaneNavigationEmptyAndReadOnly(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	for _, key := range []string{"tab", "alt+2", "alt+3"} {
		navigationKey(m, key)
		if m.history.pane != paneThreads {
			t.Fatal("empty state entered unusable pane")
		}
	}
	openThreadByID(t, d, familyThread)
	m.setPane(paneThreads)
	d.run(navigationKey(m, "enter"))
	if m.history.pane != paneConversation {
		t.Fatal("read-only group opened composer")
	}
	for _, key := range []string{"alt+3", "r"} {
		navigationKey(m, key)
		if m.history.pane != paneConversation {
			t.Fatal("read-only group entered composer")
		}
	}
}

func TestReadOnlyTabTogglesSidebarAndHistory(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, familyThread)
	m.setPane(paneThreads)
	navigationKey(m, "tab")
	if m.history.pane != paneConversation {
		t.Fatal("Tab did not enter group history")
	}
	navigationKey(m, "tab")
	if m.history.pane != paneThreads {
		t.Fatal("Tab trapped focus in read-only history")
	}
}

func TestFocusPaletteActions(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	for _, action := range []struct {
		name string
		pane int
	}{{"Focus threads", paneThreads}, {"Focus history", paneConversation}, {"Focus composer", paneComposer}} {
		m.openPalette()
		d.run(m.action(action.name))
		if m.modal != "" || m.history.pane != action.pane {
			t.Fatalf("palette action %s did not focus expected area", action.name)
		}
	}
}
