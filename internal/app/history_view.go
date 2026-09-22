package app

import (
	"fmt"
	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/ui/components"
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

func (m *Model) historyView() string {
	r := m.renderer()
	// The conversation carries the contact's or thread's theme; the shell around
	// it stays on the global one.
	cr := m.conversationRenderer()
	h := &m.history
	threadThemes := m.threadThemes()
	left, right, body, eh := m.dimensions()
	cw := max(1, right-2)
	name := m.recipient.Name
	context := "Choose a thread or create a new message"
	if t := h.active; t != nil {
		name = t.DisplayName
		context = "Replying to " + name
		if t.IsGroup {
			// Groups name their members here; the read-only notice sits on the
			// composer's own line so this stays one line at every width. The
			// member list is dropped when it merely repeats the thread name.
			who := make([]string, 0, len(t.Participants))
			for _, p := range t.Participants {
				who = append(who, contacts.SafeLabel(p.Name))
			}
			members := strings.Join(who, ", ")
			context = fmt.Sprintf("Replying to %s · %d people", name, len(t.Participants))
			if members != "" && members != name {
				context = fmt.Sprintf("Replying to %s · %s · %d people", name, members, len(t.Participants))
			}
		}
	} else if m.recipient.PhoneNumber != "" {
		context = "To: " + m.recipient.PhoneNumber
	}
	lines := []string{cr.Styles.DetailTitle.Render(contacts.SafeLabel(name)), cr.Styles.DetailMeta.Render(context)}
	history := h.view.View(cr, h.pane == paneConversation)
	lines = append(lines, strings.Split(history, "\n")...)
	separator := cr.Styles.DetailMeta.Render(strings.Repeat("─", cw))
	lines = append(lines, separator)
	if notice := m.composerNotice(); notice != "" {
		lines = append(lines, cr.Styles.StatusError.Render(notice))
	}
	ed := strings.Split(m.editor.View(cr.Styles.Theme.BorderFocus), "\n")
	for len(ed) < eh {
		ed = append(ed, "")
	}
	lines = append(lines, ed[:eh]...)
	hint := "Enter / F12 send · Shift+Enter newline · Alt+Esc history"
	if h.pane == paneConversation {
		hint = "j/k select · r reply · v preview · / search · G newest"
	}
	if h.search || h.searchQuery != "" {
		hint = "/ " + h.searchQuery + fmt.Sprintf(" · %d matches · n/N next", len(h.searchResults))
		if h.search {
			hint += "▏"
		}
	}
	if h.older {
		hint = "Loading older messages…"
	} else if est := m.composerEstimate(); est != "" {
		hint += " · " + est
	}
	lines = append(lines, separator, cr.Styles.DetailMeta.Render(hint), components.Notification(cr, m.notice, m.failed))
	// Paint the conversation on its own background, reopening it after the inner
	// styles' resets so the whole pane body carries the contact's palette rather
	// than only its text colours.
	paint := cr.Styles.DetailBody.Width(cw)
	for i, line := range lines {
		lines[i] = tideui.StyleOver(paint, ansi.Truncate(line, cw, ""))
	}
	rightPane := tideui.Pane{Title: "Conversation", Hint: h.view.Position(), Content: strings.Join(lines, "\n"), Focused: h.pane == paneConversation || h.pane == paneComposer, Accent: cr.Styles.Theme.BorderFocus}
	status := components.Status(m.currentDevice(), r.Styles.Theme.Name, m.editor.Mode(), m.outboxSuffix())
	status.Left += " | " + h.status
	if d := m.currentDevice(); d != nil && !d.Connected {
		status.Left = "KDE Connect ○ " + d.Name + " | Offline"
	}
	layout := tideui.Layout{Width: m.width, Height: m.height, Status: &status}
	if m.width >= 70 {
		layout.Mode = tideui.SidebarOnly
		layout.SidebarRatio = .28
		// Contacts share the sidebar with threads rather than holding a column of
		// their own: threads already carry resolved names, so the list is only
		// wanted when it is being used, and the conversation gets the width.
		sidebar := tideui.Pane{Title: "Threads", Hint: "c contacts", Content: components.Threads(r, h.threads, h.threadSelected, max(1, left-2), body, threadThemes), Focused: h.pane == paneThreads}
		if h.pane == paneContacts {
			sidebar = tideui.Pane{Title: "Contacts", Hint: "Esc threads", Content: components.ContactList(r, m.contactRows(), m.selected, m.recipient.PhoneNumber, max(1, left-2), body, m.query, m.searching), Focused: true}
		}
		layout.Panes = [3]tideui.Pane{sidebar, rightPane}
	} else {
		// A single active pane, still rendered by TideUI. Tab retains all pane state.
		layout.Mode = tideui.ThreeColumn
		layout.ColumnRatios = [3]float64{1, 0, 0}
		active := rightPane
		switch h.pane {
		case paneContacts:
			active = tideui.Pane{Title: "Contacts · Tab next", Content: components.ContactList(r, m.contactRows(), m.selected, m.recipient.PhoneNumber, max(1, m.width-2), body, m.query, m.searching), Focused: true}
		case paneThreads:
			active = tideui.Pane{Title: "Threads · Tab next", Content: components.Threads(r, h.threads, h.threadSelected, max(1, m.width-2), body, threadThemes), Focused: true}
		}
		// TideUI's Tabbed mode reserves a compact header and shows the active pane.
		layout.Mode = tideui.Tabbed
		layout.Panes = [3]tideui.Pane{active}
		layout.Panes[0].Focused = true
	}
	if m.modal != "" {
		modal := m.renderModal(r)
		layout.Modal = &modal
	}
	// The graphics escapes lead the frame rather than travelling inside it. They
	// occupy no columns and move no cursor, so they are invisible to the layout,
	// but they have to reach the terminal intact: inside a pane they would meet
	// the padding and truncation that every other line is subject to.
	return h.view.Transmissions() + r.Render(layout)
}
