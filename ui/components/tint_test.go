package components

import (
	"strings"
	"testing"

	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tidesms/internal/themes"
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// truecolor forces a colour profile, because lipgloss strips every escape when
// it decides the output is not a terminal — which it is not under go test.
func truecolor(t *testing.T) {
	t.Helper()
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
}

func testRenderer() tideui.Renderer {
	return tideui.NewRenderer(themes.Base("catppuccin-mocha"), tideui.StyleOptions{})
}

// sgrFor is the foreground sequence a theme's accent renders as.
func sgrFor(t *testing.T, theme string) string {
	t.Helper()
	accent := themes.Accent(theme)
	if accent == "" {
		t.Fatalf("theme %q has no accent", theme)
	}
	rendered := lipgloss.NewStyle().Foreground(accent).Render("x")
	prefix, _, found := strings.Cut(rendered, "x")
	if !found || prefix == "" {
		t.Fatalf("no opening sequence for %q: %q", theme, rendered)
	}
	return prefix
}

// A contact's theme is visible where contacts are chosen, not only once their
// conversation is open.
func TestContactListTintsThemedNames(t *testing.T) {
	truecolor(t)
	list := []contacts.Contact{
		{Name: "Amy", PhoneNumber: "+1555000001", Theme: "rose-pine"},
		{Name: "Family", PhoneNumber: "+1555000003"},
	}
	// Select neither row, so the tint is not suppressed by the selection.
	out := ContactList(testRenderer(), list, -1, "", 30, 8, "", false)
	rows := strings.Split(out, "\n")

	amy, family := rows[0], rows[1]
	if !strings.Contains(amy, sgrFor(t, "rose-pine")) {
		t.Errorf("a themed contact was not tinted: %q", amy)
	}
	if strings.Contains(family, sgrFor(t, "rose-pine")) {
		t.Errorf("an unthemed contact borrowed another's accent: %q", family)
	}
	// Both rows still read correctly and occupy the same width.
	if got := ansi.Strip(amy); !strings.Contains(got, "Amy") {
		t.Errorf("name lost: %q", got)
	}
	if w := ansi.StringWidth(amy); w != ansi.StringWidth(family) {
		t.Errorf("tinting changed the row width: %d vs %d", w, ansi.StringWidth(family))
	}
}

// Selection is drawn by inverting the row, and an accent chosen for the pane
// background carries no contrast guarantee on it.
func TestSelectedRowIsNotTinted(t *testing.T) {
	truecolor(t)
	list := []contacts.Contact{{Name: "Amy", PhoneNumber: "+1555000001", Theme: "rose-pine"}}
	selected := ContactList(testRenderer(), list, 0, "", 30, 8, "", false)
	if strings.Contains(selected, sgrFor(t, "rose-pine")) {
		t.Errorf("the selected row was tinted over the selection highlight: %q", strings.Split(selected, "\n")[0])
	}
}

// The tint closes with a reset, which would otherwise drop the row's
// background for everything after the name. TideUI reopens it; this is the
// guard that it still does.
func TestTintDoesNotPunchAHoleInTheRow(t *testing.T) {
	truecolor(t)
	list := []contacts.Contact{{Name: "Amy", PhoneNumber: "+1555000001", Theme: "rose-pine"}}
	row := strings.Split(ContactList(testRenderer(), list, -1, "", 30, 8, "", false), "\n")[0]

	// After the name's reset the row style must be re-established, so the row
	// does not end on a bare reset with padding left unpainted.
	name := strings.Index(row, "Amy")
	if name < 0 {
		t.Fatalf("name missing: %q", row)
	}
	after := row[name+len("Amy"):]
	reset := strings.Index(after, "\x1b[0m")
	if reset < 0 {
		t.Fatalf("the tint was never closed: %q", row)
	}
	if !strings.Contains(after[reset+len("\x1b[0m"):], "\x1b[") {
		t.Errorf("nothing reopened after the tint, so the row's padding is unpainted: %q", row)
	}
}

// A thread shows the palette its conversation will open in.
func TestThreadsTintFromTheSuppliedMap(t *testing.T) {
	truecolor(t)
	ts := []domain.Thread{
		{ID: "t1", DisplayName: "Amy", LastMessage: "hi"},
		{ID: "t2", DisplayName: "Family", LastMessage: "hi"},
	}
	out := Threads(testRenderer(), ts, "t2", 30, 12, map[string]string{"t1": "nord"}, nil)
	nord := themes.Base("nord")
	// opener is the escape a style begins with, so a line can be checked for
	// being painted in it from its very first cell.
	opener := func(st lipgloss.Style) string {
		r := st.Render("x")
		return r[:strings.Index(r, "x")]
	}
	name := opener(lipgloss.NewStyle().Background(nord.BorderFocus).Foreground(nord.Bg).Bold(true))
	preview := opener(lipgloss.NewStyle().Background(nord.Bg).Foreground(nord.Fg))
	// Both lines of a themed thread are bands the width of the pane.
	check := func(out string, selected bool) {
		t.Helper()
		lines := strings.Split(out, "\n")
		for i, want := range []string{name, preview} {
			line := lines[i]
			if !strings.HasPrefix(line, want) || strings.Count(line, "\x1b[0m") != 1 || !strings.HasSuffix(line, "\x1b[0m") {
				t.Errorf("line %d is not one band in the theme's colours: %q", i, line)
			}
			plain := ansi.Strip(line)
			if ansi.StringWidth(plain) != 30 {
				t.Errorf("line %d is %d cells, want the pane's 30: %q", i, ansi.StringWidth(plain), plain)
			}
			if strings.HasPrefix(plain, "▸") != selected {
				t.Errorf("line %d selection mark = %v, want %v: %q", i, !selected, selected, plain)
			}
		}
		if !strings.Contains(ansi.Strip(lines[0]), "Amy") || !strings.Contains(ansi.Strip(lines[1]), "hi") {
			t.Errorf("the themed thread lost its text:\n%s", ansi.Strip(out))
		}
	}
	check(out, false)
	// The band carries its own background and text, so it stays on the
	// selected entry, where a bare foreground tint would not; the selection
	// is marked in the first column instead.
	check(Threads(testRenderer(), ts, "t1", 30, 12, map[string]string{"t1": "nord"}, nil), true)
	if !strings.Contains(ansi.Strip(out), "Family") {
		t.Error("an unthemed thread stopped rendering")
	}
	// A thread absent from the map has no theme of its own.
	if out2 := Threads(testRenderer(), ts, "t2", 30, 12, nil, nil); strings.Contains(out2, sgrFor(t, "nord")) {
		t.Error("a thread was tinted with no themes supplied")
	}
}

// A contact given only a bubble palette is still their own colour, so the row
// falls back to it. ThemeOut is excluded: that is the colour of your own
// messages in their thread, not a mark of who they are.
func TestContactListFallsBackToTheBubblePalette(t *testing.T) {
	truecolor(t)
	list := []contacts.Contact{
		{Name: "Bubbled", PhoneNumber: "+1555000001", ThemeIn: "nord"},
		{Name: "Outgoing only", PhoneNumber: "+1555000002", ThemeOut: "gruvbox-dark"},
		{Name: "Both", PhoneNumber: "+1555000003", Theme: "rose-pine", ThemeIn: "nord"},
	}
	rows := strings.Split(ContactList(testRenderer(), list, -1, "", 40, 10, "", false), "\n")

	if !strings.Contains(rows[0], sgrFor(t, "nord")) {
		t.Errorf("a contact with only an incoming bubble palette was not tinted: %q", rows[0])
	}
	if strings.Contains(rows[1], sgrFor(t, "gruvbox-dark")) {
		t.Errorf("an outgoing bubble palette coloured the contact's own row: %q", rows[1])
	}
	// The whole-conversation theme wins over the bubble palette.
	if !strings.Contains(rows[2], sgrFor(t, "rose-pine")) || strings.Contains(rows[2], sgrFor(t, "nord")) {
		t.Errorf("the bubble palette beat the conversation theme: %q", rows[2])
	}
}
