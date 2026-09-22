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

type Model struct{ editor ripple.Model }

func New(mode string) Model {
	m := Model{ripple.New()}
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
	// Vim :w is not a send shortcut. Explicit application send keys own sending.
	switch msg.(type) {
	case ripple.SubmitMsg, ripple.CancelMsg:
		return nil, nil
	}
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	return cmd, nil
}
func (m Model) View(accent lipgloss.Color) string {
	cursor := lipgloss.NewStyle().Background(accent).Foreground(lipgloss.Color("#1e1e2e"))
	selected := lipgloss.NewStyle().Background(lipgloss.Color("#45475a"))
	return m.editor.View(ripple.Options{Cursor: cursor.Render(" "), CursorRune: func(s string) string { return cursor.Render(s) }, Selected: func(s string) string { return selected.Render(s) }, Placeholder: func(s string) string { return lipgloss.NewStyle().Foreground(lipgloss.Color("#9399b2")).Render(s) }})
}
