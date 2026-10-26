// Package components renders reusable TideUI surfaces; it performs no I/O.
package components

import (
	"strings"

	"github.com/allisonhere/tidesms/internal/backend"
	"github.com/allisonhere/tideui"
)

// Recipient is the compose screen's prompt before anyone has been chosen; once
// they have, the screen shows their ConversationHeader instead.
func Recipient(r tideui.Renderer) string {
	return r.Styles.DetailTitle.Render("New message") + "\n\n" + r.Styles.DetailMeta.Render("Choose a contact or press n to enter a number.")
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
