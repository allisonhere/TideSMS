// Package components renders reusable TideUI surfaces; it performs no I/O.
package components

import (
	"strings"

	"github.com/allisonhere/tidesms/internal/backend"
	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/themes"
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// tinted paints text in a theme's accent, so a list shows which palette a
// contact or thread carries rather than leaving a theme visible only once its
// conversation is open.
//
// The span closes with a reset, which is safe inside a row: TideUI renders a
// row through StyleOver, and that re-opens the row's own attributes after every
// reset, so only the name is tinted and the suffix and padding are unaffected.
//
// A selected row is left alone. Selection is drawn by inverting the row, and a
// foreground chosen for the pane background has no contrast guarantee on it;
// the accent is still visible on every other row.
func tinted(text, theme string, selected bool) string {
	if selected || theme == "" {
		return text
	}
	accent := themes.Accent(theme)
	if accent == "" {
		return text
	}
	return lipgloss.NewStyle().Foreground(accent).Render(text)
}

// chip paints text on a theme's own colours rather than tinting it against the
// pane, so a thread row reads as that person's conversation at a glance. The
// name sits on the accent, as TideUI draws its own headings, and the message
// preview on the theme's background and text, as a small bubble would. Both
// pairs come from one palette, so they are legible whatever the pane is, which
// is why a chip is kept on the selected row where a bare tint is not.
func chip(text, theme string, name bool) string {
	if theme == "" || !themes.Valid(theme) {
		return text
	}
	base := themes.Base(theme)
	style := lipgloss.NewStyle().Background(base.Bg).Foreground(base.Fg)
	if name {
		style = lipgloss.NewStyle().Background(base.BorderFocus).Foreground(base.Bg).Bold(true)
	}
	return style.Render(text)
}

func ContactList(r tideui.Renderer, items []contacts.Contact, selected int, active string, w, h int, query string, searching bool) string {
	rows := []string{}
	visible := max(1, h-3)
	start := max(0, selected-visible+1)
	if len(items) == 0 {
		rows = append(rows, r.Styles.DetailMeta.Render("No conversations yet\nn starts a message · a adds a contact"))
	}
	for i := start; i < min(len(items), start+visible); i++ {
		prefix := "  "
		if items[i].PhoneNumber == active {
			prefix = "● "
		}
		suffix := ""
		if items[i].Synced {
			suffix = "⟲"
		}
		// A contact may have been given only a bubble palette, which is just as
		// much their colour as a whole theme. ThemeOut is left out: that is the
		// colour of your own messages in their thread, not a mark of who they
		// are.
		palette := themes.First(items[i].Theme, items[i].ThemeIn)
		name := tinted(contacts.SafeLabel(items[i].Name), palette, i == selected)
		rows = append(rows, r.RenderRow(tideui.Row{Prefix: prefix, Text: name, Suffix: suffix, Selected: i == selected}, w))
	}
	for len(rows) < visible {
		rows = append(rows, "")
	}
	label := "/ Search contacts"
	if query != "" || searching {
		label = "/ " + query
		if searching {
			label += "▏"
		}
	}
	rows = append(rows, r.Styles.DetailMeta.Render(label), r.Styles.DetailMeta.Render("n new message · a add · ⟲ from phone"))
	for i, line := range rows {
		rows[i] = ansi.Truncate(line, w, "…")
	}
	return strings.Join(rows, "\n")
}
func Recipient(r tideui.Renderer, name, phone string) string {
	if phone == "" {
		return r.Styles.DetailTitle.Render("New message") + "\n\n" + r.Styles.DetailMeta.Render("Choose a contact or press n to enter a number.")
	}
	return r.Styles.DetailTitle.Render("To: "+contacts.SafeLabel(name)) + "\n" + r.Styles.DetailMeta.Render(phone)
}
func Status(device *backend.Device, theme, mode, extra string) tideui.StatusBar {
	connection := "KDE Connect ○ No phone selected"
	if device != nil {
		if device.Connected {
			connection = "KDE Connect ● " + device.Name + " · Connected"
		} else {
			connection = "KDE Connect ○ " + device.Name + " · offline"
		}
	}
	right := theme
	if mode != "" {
		right += " | " + mode
	}
	if extra != "" {
		right += " | " + extra
	}
	return tideui.StatusBar{Left: connection, Right: right}
}
func Notification(r tideui.Renderer, text string, failed bool) string {
	if failed {
		return r.Styles.StatusError.Render(text)
	}
	return r.Styles.StatusSuccess.Render(text)
}
func Modal(r tideui.Renderer, title, body string, width int) tideui.Overlay {
	return r.SoftPanelOverlay(tideui.SoftPanel{Prefix: "TideSMS", Title: title, Content: r.RenderSoftBody(width, body), Width: width})
}
func Choices(r tideui.Renderer, items []string, selected, width, height int) string {
	var rows []string
	count := max(1, height)
	start := max(0, selected-count+1)
	for i := start; i < min(len(items), start+count); i++ {
		rows = append(rows, r.RenderSoftRow(tideui.SoftRow{Text: items[i], Selected: i == selected}, width))
	}
	if len(items) == 0 {
		rows = append(rows, "No matches")
	}
	return strings.Join(rows, "\n")
}
func DeviceChoices(devices []backend.Device) []string {
	var rows []string
	for _, d := range devices {
		state := "○ offline"
		if d.Connected {
			state = "● connected"
		}
		rows = append(rows, d.Name+" · "+state+"\n"+d.ID+" · SMS "+d.SMSCapability)
	}
	return rows
}
