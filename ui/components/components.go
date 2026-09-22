// Package components renders reusable TideUI surfaces; it performs no I/O.
package components

import (
	"github.com/allisonhere/tidesms/internal/backend"
	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

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
		rows = append(rows, r.RenderRow(tideui.Row{Prefix: prefix, Text: contacts.SafeLabel(items[i].Name), Suffix: suffix, Selected: i == selected}, w))
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
func Status(device *backend.Device, theme, mode string) tideui.StatusBar {
	connection := "KDE Connect ○ No phone selected"
	if device != nil {
		if device.Connected {
			connection = "KDE Connect ● " + device.Name + " · Connected"
		} else {
			connection = "KDE Connect ○ " + device.Name + " · offline"
		}
	}
	return tideui.StatusBar{Left: connection, Right: theme + " | " + mode}
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
