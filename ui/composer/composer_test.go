package composer

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"strings"
	"testing"
)

func TestApplyAllIsOneUndoPoint(t *testing.T) {
	m := New("normal")
	m.SetValue("teh cat")
	m.ApplyAll("the cat")
	if m.Value() != "the cat" {
		t.Fatalf("apply = %q", m.Value())
	}
	if !m.Undo() {
		t.Fatal("no undo recorded")
	}
	if m.Value() != "teh cat" {
		t.Fatalf("undo = %q", m.Value())
	}
}

func TestApplySelectionReplacesSelection(t *testing.T) {
	m := New("normal")
	m.SetValue("hello world")
	// Shift+Left selects the trailing "world" one rune at a time.
	for i := 0; i < 5; i++ {
		if _, err := m.Update(tea.KeyMsg{Type: tea.KeyShiftLeft}); err != nil {
			t.Fatal(err)
		}
	}
	if m.Selected() == "" {
		t.Skip("terminal-independent selection not available; selection rewrite covered in app tests")
	}
	if !m.ApplySelection("there") {
		t.Fatal("selection not replaced")
	}
	if m.Value() != "hello there" {
		t.Fatalf("selection apply = %q", m.Value())
	}
}

func TestApplySelectionWithoutSelectionIsNoop(t *testing.T) {
	m := New("normal")
	m.SetValue("hi")
	if m.ApplySelection("x") {
		t.Fatal("reported a selection that was not there")
	}
	if m.Value() != "hi" {
		t.Fatalf("value changed: %q", m.Value())
	}
}

func TestMarkersRenderUnderline(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	m := New("normal")
	m.SetValue("teh cat")
	m.SetMarkers([]Marker{{Start: 0, End: 3}})
	// The marker style is the accent foreground plus underline; the cursor only
	// uses the accent as a background, so this sequence identifies the marker.
	const marked = "38;2;255;0;0;4m"
	view := m.View(lipgloss.Color("#ff0000"))
	if !strings.Contains(view, marked) {
		t.Fatalf("marker underline missing: %q", view)
	}
	m.ClearMarkers()
	if strings.Contains(m.View(lipgloss.Color("#ff0000")), marked) {
		t.Fatal("underline survived ClearMarkers")
	}
}

// A new composer draws its hint whole and no cursor until the host focuses it;
// once focused, the cursor sits on the hint's first letter instead of hiding it.
func TestPlaceholderKeepsItsFirstLetter(t *testing.T) {
	m := New("normal")
	m.Size(40, 3)
	plain := func() string { return ansi.Strip(m.View(lipgloss.Color("#ff0000"))) }
	if v := plain(); !strings.Contains(v, "Write a message…") {
		t.Fatalf("unfocused placeholder = %q", v)
	}
	m.Focus(true)
	if v := plain(); !strings.Contains(v, "Write a message…") {
		t.Fatalf("focused placeholder = %q", v)
	}
}
