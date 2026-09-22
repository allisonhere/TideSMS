// Package composer is the sole Ripple integration point.
package composer

import (
	"github.com/allisonhere/ripple"
	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type osClipboard struct{}

func (osClipboard) Read() (string, error) { return clipboard.ReadAll() }
func (osClipboard) Write(s string) error  { return clipboard.WriteAll(s) }

type Model struct {
	editor  ripple.Model
	markers []Marker
}

// Marker is a rune range the host wants highlighted in place, such as the spans
// an AI review has suggested changing.
type Marker struct{ Start, End int }

// CancelMsg reports that the editor asked to leave composing, as vim's ":q" or
// a clean second Esc in Normal mode does. The host decides what leaving means.
type CancelMsg struct{}

func New(mode string) Model {
	m := Model{editor: ripple.New()}
	m.editor.SetClipboard(osClipboard{})
	m.editor.SetPlaceholder("Write a message…")
	m.SetMode(mode)
	return m
}
func (m *Model) SetMode(mode string) {
	if mode == "vim" {
		m.editor.SetInputMode(ripple.ModeVim)
	} else {
		m.editor.SetInputMode(ripple.ModePlain)
	}
}
func (m *Model) SetValue(s string) { m.editor.SetValue(s) }
func (m Model) Value() string      { return m.editor.Value() }

// Selected returns the current selection, or "" when nothing is selected.
func (m Model) Selected() string { return m.editor.SelectedText() }

// ApplySelection replaces the current selection with text as one undo point,
// and reports whether there was a selection to replace.
func (m *Model) ApplySelection(text string) bool {
	if m.editor.SelectedText() == "" {
		return false
	}
	m.editor.InsertString(text)
	m.ClearMarkers()
	return true
}

// ApplyAll replaces the whole document as a single undo point. Ripple records
// the pre-edit text, so one undo restores exactly what was there before.
func (m *Model) ApplyAll(text string) {
	m.editor.SelectAll()
	m.editor.InsertString(text)
	m.ClearMarkers()
}

// Undo steps back one edit, so an accepted AI change is recoverable.
func (m *Model) Undo() bool { return m.editor.Undo() }

// InsertString inserts text at the caret as one undo unit, used for quoted
// replies and other programmatic composition.
func (m *Model) InsertString(s string) { m.editor.InsertString(s) }

// SetMarkers replaces the inline highlight ranges.
func (m *Model) SetMarkers(ms []Marker) { m.markers = ms }

// ClearMarkers removes every inline highlight.
func (m *Model) ClearMarkers() { m.markers = nil }

func (m Model) marked(offset int) bool {
	for _, mk := range m.markers {
		if offset >= mk.Start && offset < mk.End {
			return true
		}
	}
	return false
}
func (m *Model) Focus(on bool) {
	if on {
		m.editor.Focus()
	} else {
		m.editor.Blur()
	}
}
func (m *Model) Size(w, h int) { m.editor.SetSize(max(1, w), max(1, h)) }
func (m Model) Mode() string {
	if mode := m.editor.Mode(); mode != "" {
		return mode
	}
	return "INSERT"
}
func (m *Model) Update(msg tea.Msg) (tea.Cmd, error) {
	if p, ok := msg.(ripple.PasteMsg); ok && p.Err != nil {
		return nil, p.Err
	}
	// Vim :w is not a send shortcut. Explicit application send keys own sending,
	// but a cancel request is surfaced so the host can leave the composer.
	switch msg.(type) {
	case ripple.SubmitMsg:
		return nil, nil
	case ripple.CancelMsg:
		return func() tea.Msg { return CancelMsg{} }, nil
	}
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	return cmd, nil
}
func (m Model) View(accent lipgloss.Color) string {
	cursor := lipgloss.NewStyle().Background(accent).Foreground(lipgloss.Color("#1e1e2e"))
	selected := lipgloss.NewStyle().Background(lipgloss.Color("#45475a"))
	opts := ripple.Options{Cursor: cursor.Render(" "), CursorRune: func(s string) string { return cursor.Render(s) }, Selected: func(s string) string { return selected.Render(s) }, Placeholder: func(s string) string { return lipgloss.NewStyle().Foreground(lipgloss.Color("#9399b2")).Render(s) }}
	if len(m.markers) > 0 {
		mark := lipgloss.NewStyle().Foreground(accent).Underline(true)
		opts.StyleKey = func(offset int) string {
			if m.marked(offset) {
				return "ai"
			}
			return ""
		}
		opts.Style = func(key, text string) string {
			if key == "ai" {
				return mark.Render(text)
			}
			return text
		}
	}
	return m.editor.View(opts)
}
