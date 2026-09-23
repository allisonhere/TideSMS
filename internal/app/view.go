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
	"path/filepath"
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
	list := components.ContactList(r, m.contactRows(), max(0, m.selected), m.recipient.PhoneNumber, max(1, left-2), body, m.query, m.searching)
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
		notification = "Local drafts · Ctrl+P commands · Ctrl+O settings · ? help"
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

// settingsHeading draws a section title as a labelled rule. The settings panel
// and the shortcut list share it so the two read as the same kind of document:
// both are long lists that only become scannable once they are divided.
func settingsHeading(r tideui.Renderer, title string, width int) string {
	label := strings.ToUpper(title)
	rule := max(0, width-ansi.StringWidth(label)-1)
	return r.Styles.DetailMeta.Render(label + " " + strings.Repeat("─", rule))
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
	case "ai-models":
		title = "Model · " + m.cfg.AI.Provider
		body = components.Choices(r, m.choices, m.choice, w-4, m.height-12)
		hint = "↑↓ choose · Enter use · Esc cancel"
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
	case "media":
		title = m.mediaTitle()
		body = m.mediaViewerLines() + "\n" + r.Styles.DetailMeta.Render("←/→ next · d download · v preview · o open · s save · c copy path · Esc close")
		hint = "Esc close"
	case "open-attachment":
		title = "Open externally"
		body = "Open this file with the desktop's default app?\n\n" + filepath.Base(m.pendingOpenPath) + "\n\n" + components.Choices(r, m.choices, m.choice, w-4, max(1, m.height-16))
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
		lines := m.settingsLines()
		idx := max(0, min(m.choice, len(m.settingsFields())-1))
		// The panel grew past what a short window can hold, so it shows a slice
		// around the selection rather than overflowing the modal. The window is
		// only smaller than the list when it has to be, and it is measured in
		// drawn lines, headings included, since those take room too.
		visible := max(3, m.height-12)
		cursor := 0
		for i, line := range lines {
			if line.heading == "" && line.index == idx {
				cursor = i
			}
		}
		first := 0
		if len(lines) > visible {
			first = min(max(0, cursor-visible/2), len(lines)-visible)
			// Never open a window on a heading's row without the rows it
			// titles being the reason; starting one line earlier keeps the
			// heading with its group.
			if first > 0 && lines[first].heading == "" && lines[first-1].heading != "" {
				first--
			}
		}
		last := min(len(lines), first+visible)
		var rows []string
		if first > 0 {
			rows = append(rows, r.Styles.DetailMeta.Render("↑ more"))
		}
		for i := first; i < last; i++ {
			line := lines[i]
			if line.heading != "" {
				rows = append(rows, settingsHeading(r, line.heading, w-4))
				continue
			}
			suffix := m.settingsValue(line.field.id, line.index == idx)
			if m.settingEdit && line.index == idx {
				suffix = m.settingInput.View()
			}
			rows = append(rows, r.RenderSoftRow(tideui.SoftRow{Text: line.field.label, Suffix: suffix, Selected: line.index == idx}, w-4))
		}
		if last < len(lines) {
			rows = append(rows, r.Styles.DetailMeta.Render("↓ more"))
		}
		body = strings.Join(rows, "\n")
		// Say what is wrong with the AI configuration here, where it can be
		// fixed, rather than leaving it to surface later as a refusal that
		// names the privacy policy instead of the setting at fault.
		if notice := m.aiSettingsNotice(); notice != "" {
			body += "\n\n" + r.Styles.StatusError.Render(ansi.Truncate(notice, w-4, "…"))
		}
		body += "\n\n" + r.Styles.DetailMeta.Render(m.settingsHint()) + "\n" + m.configPath
	case "delete":
		title = "Delete contact"
		body = "Delete “" + m.editing.Name + "”?\nThe contact will be removed. Its draft is retained.\n"
		hint = "Enter delete · Esc cancel"
	case "help":
		title = "Keyboard shortcuts"
		body = strings.Join(helpSections(r, m.history.enabled, w-4), "\n")
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

// helpGroup is one titled run of shortcut lines, shaped like the settings
// panel's groups so the two read the same way.
type helpGroup struct {
	title string
	lines []string
}

// helpSections renders the shortcut list under the same headings the settings
// panel uses. The list was one undivided block, which is exactly the shape a
// reader has to scan rather than look up.
func helpSections(r tideui.Renderer, history bool, width int) []string {
	groups := composeHelp()
	if history {
		groups = historyHelp()
	}
	var out []string
	for i, g := range groups {
		if i > 0 {
			out = append(out, "")
		}
		out = append(out, settingsHeading(r, g.title, width))
		out = append(out, g.lines...)
	}
	return out
}

// historyHelp is the shortcut list with a conversation open.
func historyHelp() []helpGroup {
	return []helpGroup{
		{"Moving around", []string{
			"Tab            Cycle threads / history / composer",
			"j/k or ↑↓      Select thread or message",
			"Enter          Open thread · inspect message",
			"g / G          Oldest loaded · newest and mark read",
			"PgUp/PgDn      Scroll message lines",
			"c / Esc        Contact list · back to threads",
		}},
		{"The conversation", []string{
			"r              Reply (failed message: prepare retry)",
			"y              Copy message text",
			"v              Preview attachment, fetching it if needed",
			"/              Search this thread   n/N  next / previous",
			"Esc            Leave search",
		}},
		{"Composing", []string{
			"Enter          Submit   Ctrl+Enter and F12 also send",
			"Shift+Enter    New line",
			"Esc            Leave composer (Vim: Alt+Esc or double Esc)",
			"Ctrl+G         AI review of the draft",
		}},
		{"Everywhere", []string{
			", or Ctrl+O    Settings",
			"Ctrl+P         Commands: theme, unread, refresh, contact",
			"Ctrl+F         Search all messages",
			"?              This list        q  Quit from a navigation pane",
		}},
		{"Good to know", []string{
			"⟲ marks contacts from your phone; e or t keeps a local copy.",
			"The list shows people you have threads with; n searches all.",
			"Submitting to the CLI is not a delivery receipt.",
		}},
	}
}

// composeHelp is the shortcut list before a conversation is open.
func composeHelp() []helpGroup {
	return []helpGroup{
		{"Contacts", []string{
			"j/k or ↑↓      Move            Enter  Select",
			"/              Search          n      New message",
			"a / e          Add · edit      d      Delete",
			"t              Theme           r      Refresh from phone",
			"Tab / Esc      Cycle panes · back to threads",
		}},
		{"Composing", []string{
			"Enter          Send   Ctrl+Enter and F12 also send",
			"Shift+Enter    New line",
			"Esc            Leave composer (Vim: Alt+Esc or double Esc)",
			"Ctrl+G         AI review of the draft",
			"Ctrl+C         Copy text while composing",
		}},
		{"Everywhere", []string{
			", or Ctrl+O    Settings",
			"Ctrl+P         Commands",
			"Ctrl+F         Search all messages",
			"?              This list        q  Quit (saves drafts)",
		}},
		{"Good to know", []string{
			"⟲ marks contacts from your phone; e or t keeps a local copy.",
			"Submitting to the CLI is not a delivery receipt.",
		}},
	}
}
