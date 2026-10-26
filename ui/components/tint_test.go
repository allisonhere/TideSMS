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

// sgrFor is the colour parameters of a theme's accent as a foreground, which
// appear inside whatever sequence a row combines them into.
func sgrFor(t *testing.T, theme string) string {
	t.Helper()
	accent := themes.Accent(theme)
	if accent == "" {
		t.Fatalf("theme %q has no accent", theme)
	}
	return termenv.TrueColor.Color(string(accent)).Sequence(false)
}

// chipSGR is the background parameters of a theme's monogram chip.
func chipSGR(t *testing.T, theme string) string {
	t.Helper()
	return "4" + strings.TrimPrefix(sgrFor(t, theme), "3")
}

func contactRows(items []contacts.Contact, selected int, focused bool) []string {
	out := ContactList(testRenderer(), ContactView{Items: items, Selected: selected, Width: 40, Height: 10, Focused: focused})
	return strings.Split(out, "\n")
}

// row finds the line naming who.
func row(t *testing.T, rows []string, who string) string {
	t.Helper()
	for _, r := range rows {
		if strings.Contains(ansi.Strip(r), who) {
			return r
		}
	}
	t.Fatalf("no row for %q", who)
	return ""
}

// A contact's theme is visible where contacts are chosen, not only once their
// conversation is open: on the chip, and on the name.
func TestContactListTintsThemedNames(t *testing.T) {
	truecolor(t)
	list := []contacts.Contact{
		{Name: "Amy", PhoneNumber: "+1555000001", Theme: "rose-pine"},
		{Name: "Family", PhoneNumber: "+1555000003"},
	}
	rows := contactRows(list, -1, true)
	amy, family := row(t, rows, "Amy"), row(t, rows, "Family")
	if !strings.Contains(amy, sgrFor(t, "rose-pine")) || !strings.Contains(amy, chipSGR(t, "rose-pine")) {
		t.Errorf("a themed contact was not tinted: %q", amy)
	}
	if strings.Contains(family, "rose") || strings.Contains(family, sgrFor(t, "rose-pine")) {
		t.Errorf("an unthemed contact borrowed another's accent: %q", family)
	}
	if w := ansi.StringWidth(amy); w != 40 || w != ansi.StringWidth(family) {
		t.Errorf("rows are %d and %d cells, want 40", w, ansi.StringWidth(family))
	}
}

// On the selection fill the accent has no contrast guarantee, so the name is
// drawn plainly there and the chip keeps the colour.
func TestSelectedRowIsNotTinted(t *testing.T) {
	truecolor(t)
	list := []contacts.Contact{{Name: "Amy", PhoneNumber: "+1555000001", Theme: "rose-pine"}}
	amy := row(t, contactRows(list, 0, true), "Amy")
	if strings.Contains(amy, sgrFor(t, "rose-pine")) {
		t.Errorf("the selected name was tinted over the selection: %q", amy)
	}
	if !strings.Contains(amy, chipSGR(t, "rose-pine")) {
		t.Errorf("the selected row lost its chip colour: %q", amy)
	}
	if !strings.HasPrefix(ansi.Strip(amy), "▌") {
		t.Errorf("the selected row has no selection bar: %q", ansi.Strip(amy))
	}
}

// A selected row is filled only while its list has focus; an unfocused list
// keeps the bar alone.
func TestSelectionFillFollowsFocus(t *testing.T) {
	truecolor(t)
	list := []contacts.Contact{{Name: "Amy", PhoneNumber: "+1555000001"}, {Name: "Bo", PhoneNumber: "+1555000002"}}
	fill := func(focused bool) bool {
		bg := testRenderer().Styles.ItemSelected.GetBackground().(lipgloss.Color)
		return strings.Contains(row(t, contactRows(list, 0, focused), "Amy"), termenv.TrueColor.Color(string(bg)).Sequence(true))
	}
	if !fill(true) || fill(false) {
		t.Errorf("selection fill focused=%v unfocused=%v, want true and false", fill(true), fill(false))
	}
	if !strings.HasPrefix(ansi.Strip(row(t, contactRows(list, 0, false), "Amy")), "▌") {
		t.Error("an unfocused list lost its selection bar")
	}
}

// Every segment of a row carries the row's background, so no reset leaves the
// rest of the line unpainted.
func TestTintDoesNotPunchAHoleInTheRow(t *testing.T) {
	truecolor(t)
	list := []contacts.Contact{{Name: "Amy", PhoneNumber: "+1555000001", Theme: "rose-pine"}}
	amy := row(t, contactRows(list, -1, true), "Amy")
	after := amy[strings.Index(amy, "Amy")+len("Amy"):]
	if !strings.Contains(after, "\x1b[") {
		t.Errorf("nothing painted after the name: %q", amy)
	}
}

// A search with no results says so, rather than claiming there are no
// conversations at all.
func TestContactSearchWithNoMatches(t *testing.T) {
	out := ansi.Strip(ContactList(testRenderer(), ContactView{Width: 40, Height: 10, Query: "zed"}))
	if !strings.Contains(out, "No contacts match “zed”") || strings.Contains(out, "No conversations yet") {
		t.Errorf("empty search misreported:\n%s", out)
	}
}

// A thread shows the palette its conversation will open in, selected or not.
func TestThreadsTintFromTheSuppliedMap(t *testing.T) {
	truecolor(t)
	ts := []domain.Thread{
		{ID: "t1", DisplayName: "Amy", LastMessage: "hi"},
		{ID: "t2", DisplayName: "Family", LastMessage: "hi"},
	}
	render := func(selected string, th map[string]string) []string {
		return strings.Split(Threads(testRenderer(), ThreadView{Threads: ts, Selected: selected, Width: 30, Height: 12, Themes: th, Focused: true}), "\n")
	}
	for _, selected := range []string{"t1", "t2"} {
		lines := render(selected, map[string]string{"t1": "nord"})
		if !strings.Contains(lines[0], chipSGR(t, "nord")) {
			t.Errorf("selected=%s: the themed thread has no chip in its palette: %q", selected, lines[0])
		}
		for i, line := range lines[:2] {
			if w := ansi.StringWidth(line); w != 30 {
				t.Errorf("selected=%s line %d is %d cells, want the pane's 30", selected, i, w)
			}
			if strings.HasPrefix(ansi.Strip(line), "▌") != (selected == "t1") {
				t.Errorf("selected=%s line %d selection bar wrong: %q", selected, i, ansi.Strip(line))
			}
		}
		if !strings.Contains(ansi.Strip(lines[0]), "Amy") || !strings.Contains(ansi.Strip(lines[1]), "hi") {
			t.Errorf("the themed thread lost its text:\n%s", ansi.Strip(strings.Join(lines, "\n")))
		}
	}
	if out := strings.Join(render("t2", nil), "\n"); strings.Contains(out, chipSGR(t, "nord")) {
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
	rows := contactRows(list, -1, true)
	if r := row(t, rows, "Bubbled"); !strings.Contains(r, sgrFor(t, "nord")) {
		t.Errorf("a contact with only an incoming bubble palette was not tinted: %q", r)
	}
	if r := row(t, rows, "Outgoing only"); strings.Contains(r, sgrFor(t, "gruvbox-dark")) {
		t.Errorf("an outgoing bubble palette coloured the contact's own row: %q", r)
	}
	// The whole-conversation theme wins over the bubble palette.
	if r := row(t, rows, "Both"); !strings.Contains(r, sgrFor(t, "rose-pine")) || strings.Contains(r, sgrFor(t, "nord")) {
		t.Errorf("the bubble palette beat the conversation theme: %q", r)
	}
}
