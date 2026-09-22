package app

import (
	"fmt"
	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tidesms/internal/sms"
	"github.com/allisonhere/tidesms/internal/themes"
	"github.com/allisonhere/tidesms/ui/components"
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

func (m *Model) dimensions() (left, right, body, editor int) {
	if m.history.enabled {
		body = max(1, m.height-4)
		editor = max(1, min(5, body/4))
		switch {
		case m.width >= 70:
			left = int(float64(m.width) * .28)
			right = m.width - left
		default:
			left = 0
			right = m.width
		}
		return
	}
	left = int(float64(m.width) * 0.28)
	right = m.width - left
	body = max(1, m.height-4)
	editor = max(1, body-9)
	return
}

// composerEstimate reports the draft's length and SMS segment count, or "" for
// an empty draft. It is shown next to the composer so a long message is obvious
// before sending.
func (m *Model) composerEstimate() string {
	text := m.editor.Value()
	if strings.TrimSpace(text) == "" {
		return ""
	}
	e := sms.Count(text)
	return fmt.Sprintf("%d chars · %d SMS", len([]rune(text)), e.Segments)
}

func (m *Model) sizeEditor() {
	_, right, _, height := m.dimensions()
	m.editor.Size(max(1, right-2), height)
	m.layoutConversation()
}
func (m *Model) View() string {
	if !m.ready {
		return ""
	}
	if m.history.enabled {
		return m.historyView()
	}
	theme := themes.Resolve(m.cfg.General.Theme, m.recipient.Theme, m.recipient.PhoneNumber)
	r := tideui.NewRenderer(theme, tideui.StyleOptions{PaneCorners: tideui.RoundCorners, ModalShadow: true})
	left, right, body, eh := m.dimensions()
	if m.width < 54 || m.height < 16 {
		return r.Render(tideui.Layout{Width: m.width, Height: m.height, Mode: tideui.SidebarOnly, Panes: [3]tideui.Pane{{Title: "TideSMS", Content: "Resize to at least 54 × 16\nDrafts remain safe.\nCtrl+P → Quit"}}})
	}
	list := components.ContactList(r, m.filtered(), max(0, m.selected), m.recipient.PhoneNumber, max(1, left-2), body, m.query, m.searching)
	title := "New Message"
	if m.sending {
		title = "Sending…"
	}
	header := components.Recipient(r, m.recipient.Name, m.recipient.PhoneNumber)
	// Fixed recipient region and editor viewport keep the composer stable on resize.
	lines := strings.Split(header, "\n")
	for len(lines) < 4 {
		lines = append(lines, "")
	}
	separator := r.Styles.DetailMeta.Render(strings.Repeat("─", max(1, right-2)))
	mode := m.editor.Mode() + " · Ripple"
	if !m.focus {
		mode += " · Tab to compose"
	}
	lines = append(lines, separator, r.Styles.DetailMeta.Render(mode))
	ed := strings.Split(m.editor.View(theme.BorderFocus), "\n")
	for len(ed) < eh {
		ed = append(ed, "")
	}
	lines = append(lines, ed[:eh]...)
	footer := "Enter / F12 send · Shift+Enter newline · Alt+Esc contacts"
	if est := m.composerEstimate(); est != "" {
		footer += " · " + est
	}
	lines = append(lines, separator, r.Styles.DetailMeta.Render(footer))
	notification := m.notice
	if notification == "" {
		notification = "Local drafts · Ctrl+P commands · ? help"
	}
	lines = append(lines, ansi.Truncate(components.Notification(r, notification, m.failed), right-2, "…"))
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, right-2, "")
	}
	status := components.Status(m.currentDevice(), theme.Name, strings.ToUpper(m.cfg.Composer.Mode), m.outboxSuffix())
	layout := tideui.Layout{Width: m.width, Height: m.height, Mode: tideui.SidebarOnly, SidebarRatio: 0.28, Status: &status, Panes: [3]tideui.Pane{{Title: "Contacts", Hint: "/ search", Content: list, Focused: !m.focus, Accent: theme.BorderFocus}, {Title: title, Content: strings.Join(lines, "\n"), Focused: m.focus, Accent: theme.BorderFocus}}}
	if m.modal != "" {
		overlay := m.renderModal(r)
		layout.Modal = &overlay
	}
	return r.Render(layout)
}
func (m *Model) renderModal(r tideui.Renderer) tideui.Overlay {
	w := max(20, min(66, m.width-8))
	title := m.modal
	body := ""
	hint := "↑↓ choose · Enter confirm · Esc cancel"
	switch m.modal {
	case "palette":
		title = "Commands"
		body = m.filter.View() + "\n\n" + components.Choices(r, m.choices, m.choice, w-4, m.height-12)
	case "devices":
		title = "KDE Connect devices"
		labels := []string{}
		for _, d := range m.devices {
			state := "○ offline"
			if d.Connected {
				state = "● connected"
			}
			labels = append(labels, d.Name+" · "+state)
		}
		body = components.Choices(r, labels, m.choice, w-4, max(1, m.height-15))
		if len(m.devices) > 0 {
			idx := max(0, min(m.choice, len(m.devices)-1))
			d := m.devices[idx]
			body += "\n\nID: " + d.ID + "\nSMS: " + d.SMSCapability
		} else {
			body = "No paired devices. Pair your phone in KDE Connect.\nPress r from Contacts to refresh."
		}
	case "compose":
		title = "New message"
		body = m.filter.View() + "\n\n" + components.Choices(r, m.choices, m.choice, w-4, max(1, m.height-14))
		hint = "Type a name or number · ↑↓ choose · Enter open · Esc cancel"
	case "number", "add", "edit":
		title = map[string]string{"number": "New recipient", "add": "Add contact", "edit": "Edit contact"}[m.modal]
		for i, f := range m.fields {
			label := "Phone number"
			if len(m.fields) > 1 && i == 0 {
				label = "Name"
			}
			body += label + "\n" + f.View() + "\n\n"
		}
		hint = "Tab next field · Enter save · Esc cancel"
	case "thread-themes":
		title = "Thread accent"
		body = components.Choices(r, m.choices, m.choice, w-4, m.height-12)
	case "bubble-themes":
		if m.bubbleDir == "out" {
			title = "Outgoing bubble theme"
		} else {
			title = "Incoming bubble theme"
		}
		body = components.Choices(r, m.choices, m.choice, w-4, m.height-12)
	case "contact":
		title = "Contact"
		body = m.contactDetailsHeader() + "\n\n" + components.Choices(r, m.choices, m.choice, w-4, max(1, m.height-18))
		hint = "Enter run · Esc close"
	case "delete-message":
		title = "Delete local copy"
		body = "Delete this cached copy?\nThe phone's own message is not touched, and a synced copy may\nreappear after the next sync.\n\n" + components.Choices(r, m.choices, m.choice, w-4, max(1, m.height-16))
		hint = "Enter confirm · Esc cancel"
	case "search-all":
		title = "Search messages"
		body = m.searchInput.View() + "\n\n" + components.Choices(r, m.globalSearchLabels(), m.choice, w-4, max(1, m.height-16))
		hint = "from:/before:/after: filters · Enter open · Esc close"
	case "ai-policy":
		title = "AI policy · " + m.aiPolicyName()
		body = components.Choices(r, m.choices, m.choice, w-4, max(1, m.height-14))
		hint = "inherit falls back to the contact, then the global setting"
	case "offline-send":
		title = "Phone is offline"
		device := "Your phone"
		if d := m.currentDevice(); d != nil && d.Name != "" {
			device = d.Name
		}
		body = "Queue this message for delivery when\n" + device + " reconnects?\n\n" + components.Choices(r, m.choices, m.choice, w-4, max(1, m.height-16))
		hint = "Enter choose · Esc cancel"
	case "schedule":
		title = "Schedule message"
		body = components.Choices(r, m.choices, m.choice, w-4, max(1, m.height-14))
		hint = "Enter choose · Esc cancel"
	case "schedule-time":
		title = "Schedule message"
		body = "Date and time (YYYY-MM-DD HH:MM)\n\n" + m.schedInput.View()
		hint = "Enter schedule · Esc cancel"
	case "queue":
		title = "Outgoing queue"
		if len(m.outboxEntries) == 0 {
			body = r.Styles.DetailMeta.Render("Nothing queued.") + "\n\n" + m.queueSummary()
		} else {
			body = components.Choices(r, m.choices, m.choice, w-4, max(1, m.height-16)) + "\n\n" + m.queueSummary()
		}
		hint = "Enter inspect · s send now · e edit · d remove · p pause · Esc close"
	case "queue-item":
		title = "Queued message"
		body = m.renderQueueItem()
		hint = "Esc back"
	case "ai-review":
		title = "AI writing review"
		body = m.renderReview() + "\n" + m.renderReviewPreview()
		hint = "a Accept · r Reject · e Edit · n/p Next/Prev · A Accept all · Esc Close"
	case "ai-edit":
		title = "Edit suggestion"
		body = "Suggestion\n\n" + m.aiInput.View()
		hint = "Enter save · Esc cancel"
	case "ai-instruction":
		title = "Custom rewrite"
		body = "How should it be rewritten?\n\n" + m.aiInput.View()
		hint = "Enter run · Esc cancel"
	case "message":
		title = "Message details"
		if msg := m.history.view.Current(); msg != nil {
			// The number is reported alongside the resolved name so an unfamiliar
			// sender is never hidden behind a contact label.
			from, number := msg.Sender, msg.Sender
			if msg.Direction == domain.Outgoing {
				from, number = "You", ""
				if t := m.history.active; t != nil && len(t.Participants) == 1 {
					number = t.Participants[0].RawNumber
				}
			}
			for _, c := range m.contacts {
				if c.PhoneNumber == msg.Sender {
					from = c.Name
				}
			}
			body = "From: " + contacts.SafeLabel(from) + "\n"
			if number != "" {
				body += "Number: " + number + "\n"
			}
			body += fmt.Sprintf("Time: %s\nDirection: %s\nStatus: %s\nBackend ID: %s\n\n", msg.Timestamp.Local().Format("Jan 2, 2006 3:04:05 PM"), msg.Direction, msg.Status, msg.BackendID) + components.Choices(r, m.choices, m.choice, w-4, 4)
		}
	case "themes":
		title = "Contact accent · " + m.editing.Name
		body = components.Choices(r, m.choices, m.choice, w-4, m.height-12)
	case "settings":
		title = "Settings"
		fields := m.settingsFields()
		idx := max(0, min(m.choice, len(fields)-1))
		var rows []string
		for i, f := range fields {
			rows = append(rows, r.RenderSoftRow(tideui.SoftRow{Text: f.label, Suffix: m.settingsValue(f.id, i == idx), Selected: i == idx}, w-4))
		}
		body = strings.Join(rows, "\n") + "\n\n" + r.Styles.DetailMeta.Render("↑↓ move · ←→ change · Enter toggle · Esc close") + "\n" + m.configPath
	case "delete":
		title = "Delete contact"
		body = "Delete “" + m.editing.Name + "”?\nThe contact will be removed. Its draft is retained.\n"
		hint = "Enter delete · Esc cancel"
	case "help":
		title = "Keyboard shortcuts"
		body = "CONTACTS\nj/k or ↑↓  Move      Enter  Select\n/ Search   n New message  a Add  e Edit\nt Theme    d Delete  r Refresh\n⟲ marks contacts from your phone; e or t keeps a local copy\nTab Cycle panes     Esc back to threads\nCtrl+F Search all messages\nq Quit (saves drafts)\n\nCOMPOSER\nEnter / Ctrl+Enter / F12  Send   Shift+Enter  New line\nEsc  Leave composer   Ctrl+G  AI review    In Vim, Esc belongs to Ripple; Alt+Esc or a clean second Esc leaves\nCtrl+C copies text while composing\n\nCLI submission is not a delivery receipt."
		if m.history.enabled {
			body = "THREADS & HISTORY\nTab  Cycle threads / history / composer\nc  Contact list (hidden until asked)   Esc  Back to threads\nj/k  Select thread or message   Enter  Open / inspect\nr  Reply (failed message: prepare retry)\ny  Copy message   /  Search cached thread\nn/N  Next / previous match   Esc  Exit search\ng  Oldest loaded   G  Newest / mark read\nPgUp/PgDn  Scroll message lines\nCtrl+P  Thread theme, unread, refresh, contact   Ctrl+F  Search all messages\n⟲ marks contacts from your phone; e or t keeps a local copy\nThe list shows people you have threads with; n searches everyone\n\nCOMPOSER\nEnter / Ctrl+Enter / F12  Submit   Shift+Enter  New line\nEsc  Leave composer   Ctrl+G  AI review (Vim: Alt+Esc or double Esc)\n\nq  Quit from navigation panes"
		}
		hint = "↑↓ scroll · Esc close"
	}
	if m.failed {
		body += "\n\n" + ansi.Truncate(m.notice, w-4, "…")
	}
	if m.busy {
		hint = "Saving…"
	}
	// Bound tall dialogs while keeping the active selection and footer visible.
	maxLines := max(1, m.height-8)
	ls := strings.Split(body, "\n")
	if len(ls) > maxLines {
		start := 0
		if m.modal == "help" {
			start = min(m.choice, len(ls)-maxLines)
		}
		ls = ls[start : start+maxLines]
	}
	body = strings.Join(ls, "\n") + "\n\n" + hint
	return components.Modal(r, title, body, w)
}
