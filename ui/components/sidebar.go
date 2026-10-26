package components

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/themes"
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/lucasb-eyer/go-colorful"
)

// The sidebar lists, threads and contacts, share one row grammar so moving
// between them changes the content, not the way it reads:
//
//	▌ AR  Amy Rivera                 19:46
//	      see you at 6, bring the…      2
//
// a selection bar in the first column, a monogram chip that carries the
// person's palette, the name, and right-aligned metadata. The chip replaces
// painting the whole entry in a contact's theme: a list of full-width bands
// was a wall of colour in which the selection was hard to find, while a chip
// still says whose palette a row will open in.

// chipWidth is the monogram chip and the gap after it.
const chipWidth = 5

// minChipWidth is the narrowest list that still has room for a chip beside a
// readable name; below it the chip is dropped.
const minChipWidth = 22

// rowPaint holds the styles for one row, all on the same background so no
// segment punches a hole in a selected row's fill.
type rowPaint struct {
	base, dim, strong, accent, badge lipgloss.Style
	bar                              string
}

// paintFor returns a row's styles. A selected row is filled only while the
// list has focus; otherwise it keeps just the bar, so the eye goes to the
// pane being typed in while the list still says where it stands.
func paintFor(r tideui.Renderer, selected, focused bool) rowPaint {
	t := r.Styles.Theme
	bg := t.Bg
	if selected && focused {
		bg = r.Styles.ItemSelected.GetBackground().(lipgloss.Color)
	}
	base := lipgloss.NewStyle().Background(bg).Foreground(t.Fg)
	p := rowPaint{
		base:   base,
		dim:    base.Foreground(muted(t.Fg, bg)),
		strong: base.Bold(true),
		accent: base.Foreground(t.Unread).Bold(true),
		badge:  lipgloss.NewStyle().Background(t.Unread).Foreground(t.Bg).Bold(true),
		bar:    base.Render(" "),
	}
	switch {
	case selected && focused:
		p.strong = p.strong.Foreground(r.Styles.ItemSelected.GetForeground())
		p.bar = base.Foreground(t.BorderFocus).Render("▌")
	case selected:
		p.bar = base.Foreground(t.Border).Render("▌")
	}
	// The unread colour is chosen for the pane, not the selection fill; where
	// it fades into the fill, the selection's own text colour stands in.
	if contrast(t.Unread, bg) < 3 {
		p.accent = p.strong
	}
	return p
}

// name is the style for a row's name. Bold marks what needs reading: unread
// messages, or the row the focused list is on. Away from the selection a
// themed name is drawn in its palette's accent; on the selection fill an
// accent chosen for the pane background has no contrast guarantee, and the
// chip still carries the colour there.
func (p rowPaint) name(selected, focused, unread bool, theme string) lipgloss.Style {
	st := p.base
	if unread {
		st = st.Bold(true)
	}
	if selected && focused {
		return p.strong
	}
	if accent := themes.Accent(theme); accent != "" && !selected && contrast(accent, p.base.GetBackground()) >= 3 {
		st = st.Foreground(accent)
	}
	return st
}

// muted is secondary text: the foreground eased toward the background, so it
// always reads as quieter than a name in the same row, whichever way a theme's
// own dimmed colour leans, while keeping a 3:1 contrast with the background.
func muted(fg, bg lipgloss.TerminalColor) lipgloss.TerminalColor {
	f, ferr := colorful.Hex(colorHex(fg))
	b, berr := colorful.Hex(colorHex(bg))
	if ferr != nil || berr != nil {
		return fg
	}
	for _, amount := range []float64{0.4, 0.3, 0.2, 0.1} {
		c := lipgloss.Color(f.BlendLab(b, amount).Clamped().Hex())
		if contrast(c, bg) >= 3 {
			return c
		}
	}
	return fg
}

func colorHex(c lipgloss.TerminalColor) string {
	hex, _ := c.(lipgloss.Color)
	return string(hex)
}

// contrast is the WCAG contrast ratio of two colours. A palette's accent was
// chosen for that palette's own background, not the pane's: a pastel from a
// dark theme vanishes on a light pane, so the name is tinted only where it
// stays legible, and the chip carries the colour everywhere else.
func contrast(a, b lipgloss.TerminalColor) float64 {
	lum := func(c lipgloss.TerminalColor) float64 {
		col, err := colorful.Hex(colorHex(c))
		if err != nil {
			return 0
		}
		lin := func(v float64) float64 {
			if v <= 0.04045 {
				return v / 12.92
			}
			return math.Pow((v+0.055)/1.055, 2.4)
		}
		return 0.2126*lin(col.R) + 0.7152*lin(col.G) + 0.0722*lin(col.B)
	}
	la, lb := lum(a), lum(b)
	return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
}

// line lays out one row: the left part, a gap, and the right part flush with
// the edge, exactly w cells. The left part is cut short to make room, never
// the right, which holds the time or count a reader scans down for.
func (p rowPaint) line(w int, left, right string) string {
	room := w - ansi.StringWidth(right)
	if ansi.StringWidth(left) > room-1 && right != "" {
		left = ansi.Truncate(left, max(0, room-1), "")
	}
	left = ansi.Truncate(left, w, "")
	gap := max(0, w-ansi.StringWidth(left)-ansi.StringWidth(right))
	return left + p.base.Render(strings.Repeat(" ", gap)) + right
}

// monogram is the one or two letters a chip shows: the initials of the first
// and last words of a name, or "#" for a bare number.
func monogram(name string) string {
	var initials []rune
	for _, word := range strings.Fields(name) {
		if c, _ := utf8.DecodeRuneInString(word); unicode.IsLetter(c) {
			initials = append(initials, unicode.ToUpper(c))
		}
	}
	switch len(initials) {
	case 0:
		return "#"
	case 1:
		return string(initials)
	}
	return string([]rune{initials[0], initials[len(initials)-1]})
}

// chip draws a monogram on the person's own palette: the accent behind the
// palette's background colour, as TideUI draws its headings. Someone with no
// palette gets the pane's neutral border colour, so colour in the list always
// means a theme has been chosen.
func chip(r tideui.Renderer, name, theme string) string {
	text := fmt.Sprintf(" %-2s", monogram(name)) + " "
	if theme != "" && themes.Valid(theme) {
		palette := themes.Base(theme)
		return lipgloss.NewStyle().Background(palette.BorderFocus).Foreground(palette.Bg).Bold(true).Render(text)
	}
	return r.Styles.PaneHeaderInactive.Render(text)
}

// lead is the start of a row: bar, then chip and gap when the list is wide
// enough to carry one. indent is the same width in blank cells, for the lines
// below the first.
func (p rowPaint) lead(r tideui.Renderer, w int, name, theme string) (lead, indent string) {
	if w < minChipWidth {
		return p.bar + p.base.Render(" "), p.bar + p.base.Render(" ")
	}
	return p.bar + p.base.Render(" ") + chip(r, name, theme) + p.base.Render(" "),
		p.bar + p.base.Render(strings.Repeat(" ", chipWidth+1))
}

// when is a timestamp as a messaging list shows it: the time today, the
// weekday within the last week, the date this year, and month and year
// before that.
func when(t, now time.Time) string {
	t, now = t.Local(), now.Local()
	day := func(x time.Time) time.Time { return time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, time.Local) }
	days := int(day(now).Sub(day(t)).Hours() / 24)
	switch {
	case t.IsZero():
		return ""
	case days <= 0:
		return t.Format("15:04")
	case days == 1:
		return "Yesterday"
	case days < 7:
		return t.Format("Mon")
	case t.Year() == now.Year():
		return t.Format("Jan 2")
	}
	return t.Format("Jan 2006")
}

// window picks which of a list's lines to show in h rows so that the lines
// from first to last, the selected entry, are on screen with a little of what
// follows it.
func window(lines []string, first, last, h int) []string {
	if len(lines) <= h {
		return lines
	}
	start := 0
	if last+2 >= h {
		start = min(last+2-h+1, len(lines)-h)
	}
	start = min(start, first)
	return lines[max(0, start):min(len(lines), max(0, start)+h)]
}

// Heading is a labelled rule dividing a pane into sections. An active
// heading draws its label in the accent, marking the section that has focus,
// where the accent is legible on the pane; otherwise the label is plain bold.
func Heading(r tideui.Renderer, title string, w int, active bool) string {
	t := r.Styles.Theme
	meta := r.Styles.DetailMeta.UnsetItalic()
	label := meta.Bold(true)
	if active && contrast(t.BorderFocus, t.Bg) >= 3 {
		label = label.Foreground(t.BorderFocus)
	}
	rule := meta.Foreground(t.Border)
	return rule.Render("─") + label.Render(" "+title+" ") + rule.Render(strings.Repeat("─", max(0, w-ansi.StringWidth(title)-3)))
}

// Footer is a pane's last line: a summary on the left and, flush right, what
// goes with it. The right side is dropped first when the pane is narrow.
func Footer(r tideui.Renderer, w int, summary, right string) string {
	meta := r.Styles.DetailMeta.UnsetItalic()
	if ansi.StringWidth(summary)+ansi.StringWidth(right)+2 > w {
		right = ""
	}
	gap := max(0, w-ansi.StringWidth(summary)-ansi.StringWidth(right))
	return ansi.Truncate(meta.Render(summary)+meta.Render(strings.Repeat(" ", gap))+meta.Render(right), w, "…")
}

// fill pads a list to h lines above its footer.
func fill(rows []string, h int) []string {
	for len(rows) < h {
		rows = append(rows, "")
	}
	return rows[:h]
}

// ContactView is what the contact list shows.
type ContactView struct {
	Items     []contacts.Contact
	Selected  int    // index into Items; -1 for none
	Active    string // phone number of the current recipient
	Width     int
	Height    int
	Query     string
	Searching bool
	Focused   bool
}

// ContactList renders the sidebar's contacts: a search field, one row per
// contact, and a footer of the keys that add to the list.
func ContactList(r tideui.Renderer, v ContactView) string {
	w, h := v.Width, v.Height
	t := r.Styles.Theme
	field := r.Styles.DetailMeta.UnsetItalic()
	search := field.Foreground(t.Border).Render("/ ") + field.Render("Search contacts")
	if v.Query != "" || v.Searching {
		search = field.Foreground(t.BorderFocus).Render("/ ") + r.Styles.DetailBody.Render(contacts.SafeLabel(v.Query))
		if v.Searching {
			search += lipgloss.NewStyle().Background(t.Bg).Foreground(t.BorderFocus).Render("▏")
		}
	}
	top := []string{ansi.Truncate(search, w, "…"), field.Foreground(t.Border).Render(strings.Repeat("─", max(1, w)))}

	var rows []string
	first, last := 0, 0
	for i, c := range v.Items {
		selected := i == v.Selected
		p := paintFor(r, selected, v.Focused)
		// A contact may have been given only a bubble palette, which is just as
		// much their colour as a whole theme. ThemeOut is left out: that is the
		// colour of your own messages in their thread, not a mark of who they
		// are.
		palette := themes.First(c.Theme, c.ThemeIn)
		label := contacts.SafeLabel(c.Name)
		lead, _ := p.lead(r, w, label, palette)
		name := lead + p.name(selected, v.Focused, false, palette).Render(label)
		var meta []string
		if c.PhoneNumber == v.Active {
			meta = append(meta, p.accent.Render("●"))
		}
		if c.Synced {
			meta = append(meta, p.dim.Render("⟲"))
		}
		if w >= 44 && c.PhoneNumber != label {
			meta = append([]string{p.dim.Render(c.PhoneNumber)}, meta...)
		}
		if selected {
			first, last = len(rows), len(rows)
		}
		rows = append(rows, p.line(w, name, strings.Join(meta, p.base.Render(" "))+p.base.Render(" ")))
	}
	if len(v.Items) == 0 {
		empty := "No conversations yet\nn starts a message · a adds a contact"
		if v.Query != "" {
			empty = fmt.Sprintf("No contacts match “%s”\nEsc clears the search", contacts.SafeLabel(v.Query))
		}
		for _, l := range strings.Split(empty, "\n") {
			rows = append(rows, " "+r.Styles.DetailMeta.Render(l))
		}
	}
	body := max(1, h-len(top)-1)
	out := append(top, fill(window(rows, first, last, body), body)...)
	out = append(out, Footer(r, w, fmt.Sprintf("%d contacts", len(v.Items)), "n new · a add"))
	for i, line := range out {
		out[i] = ansi.Truncate(line, w, "…")
	}
	return strings.Join(out[:min(len(out), max(1, h))], "\n")
}

// ConversationHeader is the top of an open conversation, in the same grammar
// as a list row: the person's chip and name, and under it the number or the
// group's members. It is drawn on r, the conversation's own renderer, so it
// sits on the conversation's background. Each line is exactly w cells.
func ConversationHeader(r tideui.Renderer, name, detail, theme string, w int) []string {
	p := paintFor(r, false, false)
	label := contacts.SafeLabel(name)
	lead := p.base.Render(" ") + chip(r, label, theme) + p.base.Render(" ")
	indent := p.base.Render(strings.Repeat(" ", chipWidth+1))
	if w < minChipWidth {
		lead, indent = p.base.Render(" "), p.base.Render(" ")
	}
	room := max(1, w-ansi.StringWidth(lead)-1)
	head := p.line(w, lead+p.strong.Render(ansi.Truncate(label, room, "…")), "")
	sub := p.line(w, indent+p.dim.Render(ansi.Truncate(contacts.SafeLabel(detail), max(1, w-ansi.StringWidth(indent)-1), "…")), "")
	return []string{head, sub}
}
