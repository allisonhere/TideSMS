package app

import (
	"crypto/rand"
	"fmt"
	"github.com/allisonhere/tidesms/internal/backend"
	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/storage"
	"github.com/allisonhere/tidesms/internal/themes"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
)

var commands = []string{"New message", "Add contact", "Edit contact", "Delete contact", "Change contact theme", "Switch device", "Toggle composer mode", "Open settings", "Send message", "Quit", "Search current thread", "Refresh conversations", "Change thread theme", "Mark thread unread", "Copy phone number", "Open contact", "Jump to newest", "Sync phone contacts", "Toggle message bubbles", "Toggle bubble corners", "Toggle bubble fill", "Change incoming bubble theme", "Change outgoing bubble theme", "Change thread incoming bubble theme", "Change thread outgoing bubble theme", "Change AI policy", "Change thread AI policy", "AI: Review writing", "AI: Fix spelling", "AI: Fix grammar", "AI: Clean up", "AI: Make shorter", "AI: Make friendlier", "AI: Make professional", "AI: Make clearer", "AI: Custom rewrite…", "Schedule message", "Open outgoing queue", "Send queued messages", "Search all messages", "Mute thread", "Unmute thread", "Toggle notification body preview", "Contact details"}

// historyCommands are the palette entries that only make sense with the
// conversation view, so a plain compose session does not offer them.
var historyCommands = map[string]bool{
	"Search current thread": true, "Refresh conversations": true, "Change thread theme": true,
	"Mark thread unread": true, "Copy phone number": true, "Open contact": true, "Jump to newest": true,
	"Sync phone contacts": true, "Toggle message bubbles": true, "Toggle bubble corners": true,
	"Toggle bubble fill": true, "Change thread incoming bubble theme": true, "Change thread outgoing bubble theme": true,
	"Change thread AI policy": true,
	"Mute thread":             true,
	"Unmute thread":           true,
}

func (m *Model) navigation(k tea.KeyMsg) tea.Cmd {
	if k.String() == "q" || k.String() == "ctrl+c" {
		return m.quit()
	}
	if !m.loaded {
		return nil
	}
	cs := m.filtered()
	step := max(1, m.height-8)
	switch k.String() {
	case "j", "down":
		m.selected = min(len(cs)-1, m.selected+1)
	case "k", "up":
		m.selected = max(0, m.selected-1)
	case "pgdown", "ctrl+d":
		m.selected = min(len(cs)-1, m.selected+step)
	case "pgup", "ctrl+u":
		m.selected = max(0, m.selected-step)
	case "g", "home":
		m.selected = 0
	case "G", "end":
		m.selected = max(0, len(cs)-1)
	case "enter":
		if c, ok := m.selectedContact(); ok {
			return m.pickRecipient(c)
		}
	case "/":
		m.searching = true
		m.filter.SetValue(m.query)
		return m.filter.Focus()
	case "n":
		return m.openCompose()
	case "a":
		m.openForm("add", contacts.Contact{})
	case "e":
		return m.action("Edit contact")
	case "d", "delete":
		return m.action("Delete contact")
	case "t":
		return m.action("Change contact theme")
	case "r":
		if !m.discovering {
			return m.discover()
		}
	case "c", "esc":
		// The contact list borrows the sidebar; leave it the way you entered.
		if m.history.enabled {
			m.setPane(paneThreads)
		}
	case ",":
		m.openSettings()
	case "?":
		m.modal = "help"
		m.choice = 0
	}
	return nil
}
func (m *Model) searchKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		m.searching = false
		m.query = ""
		m.selected = 0
		return nil
	case "enter":
		m.searching = false
		return nil
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(k)
	m.query = m.filter.Value()
	m.selected = 0
	return cmd
}
func (m *Model) openPalette() {
	m.modal = "palette"
	m.choice = 0
	m.filter.SetValue("")
	m.filter.Focus()
	m.choices = append([]string{}, commands...)
}
func (m *Model) openDevices() { m.modal = "devices"; m.choice = 0 }

// openCompose starts a message by searching the whole address book, including
// people with no conversation yet, and accepts a typed number just as well.
func (m *Model) openCompose() tea.Cmd {
	m.modal = "compose"
	m.choice = 0
	m.filter.SetValue("")
	m.refreshCompose()
	return m.filter.Focus()
}

// refreshCompose rebuilds the picker for the current query. The list is capped
// because an imported address book can be large and only the top matches are
// reachable by eye.
func (m *Model) refreshCompose() {
	query := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	m.picks = nil
	m.choices = nil
	for _, c := range m.allContacts() {
		if query != "" && !strings.Contains(strings.ToLower(c.Name+" "+c.PhoneNumber), query) {
			continue
		}
		label := contacts.SafeLabel(c.Name)
		if label != c.PhoneNumber {
			label += " · " + c.PhoneNumber
		}
		if c.Synced {
			label += " ⟲"
		}
		m.picks = append(m.picks, c)
		m.choices = append(m.choices, label)
		if len(m.picks) == 100 {
			break
		}
	}
	// A number typed in full is always offered, so an unknown recipient needs no
	// separate form.
	if number, err := contacts.Normalize(m.filter.Value()); err == nil {
		known := false
		for _, c := range m.picks {
			if c.PhoneNumber == number {
				known = true
			}
		}
		if !known {
			m.picks = append(m.picks, contacts.Contact{Name: number, PhoneNumber: number})
			m.choices = append(m.choices, "Send to "+number)
		}
	}
	m.choice = max(0, min(m.choice, len(m.choices)-1))
}
func (m *Model) openForm(kind string, c contacts.Contact) {
	m.modal = kind
	m.editing = c
	m.field = 0
	m.fields = nil
	vals := []string{c.Name, c.PhoneNumber}
	if kind == "number" {
		vals = []string{""}
	}
	for _, v := range vals {
		f := textinput.New()
		f.CharLimit = 120
		f.SetValue(v)
		f.Width = 38
		m.fields = append(m.fields, f)
	}
	m.fields[0].Focus()
}
func (m *Model) action(name string) tea.Cmd {
	if ok, cmd := m.historyAction(name); ok {
		return cmd
	}
	if cmd, ok := m.aiAction(name); ok {
		return cmd
	}
	if cmd, ok := m.outboxAction(name); ok {
		return cmd
	}
	if cmd, ok := m.searchAction(name); ok {
		return cmd
	}
	if name == "Quit" {
		m.modal = ""
		return m.quit()
	}
	if !m.loaded {
		m.notify("Storage is not ready", true)
		return nil
	}
	switch name {
	case "New message":
		return m.openCompose()
	case "Add contact":
		m.openForm("add", contacts.Contact{})
	case "Sync phone contacts":
		m.modal = ""
		return m.syncContacts(true)
	case "Edit contact", "Delete contact", "Change contact theme", "Change incoming bubble theme", "Change outgoing bubble theme", "Change AI policy", "Contact details":
		c, ok := m.selectedContact()
		if m.focus && (m.recipient.ID != "" || m.recipient.Synced) {
			c = m.recipient
			ok = true
		}
		if !ok {
			m.notify("Select a saved contact first", true)
			m.modal = ""
			return nil
		}
		m.editing = c
		switch name {
		case "Edit contact":
			m.openForm("edit", c)
		case "Delete contact":
			if c.Synced {
				m.notify("This contact comes from your phone; edit it to keep a local copy, or remove it on the phone", true)
				m.modal = ""
				return nil
			}
			m.modal = "delete"
		case "Change contact theme":
			m.modal = "themes"
			m.choice = 0
			m.choices = append([]string{"automatic"}, themes.Names...)
		case "Change incoming bubble theme":
			m.openBubblePicker("contact", false)
		case "Change outgoing bubble theme":
			m.openBubblePicker("contact", true)
		case "Change AI policy":
			m.openAIPolicyPicker(storage.ScopeContact)
		case "Contact details":
			m.openContactDetails()
		}
	case "Switch device":
		m.openDevices()
	case "Toggle composer mode":
		c := m.cfg
		if c.Composer.Mode == "vim" {
			c.Composer.Mode = "normal"
		} else {
			c.Composer.Mode = "vim"
		}
		m.modal = ""
		return m.saveConfig(c)
	case "Toggle notification body preview":
		c := m.cfg
		c.Notifications.ShowBody = !c.Notifications.ShowBody
		return m.saveConfig(c)
	case "Open settings":
		m.openSettings()
	case "Toggle message bubbles":
		c := m.cfg
		c.Conversation.Bubbles = !c.Conversation.Bubbles
		m.modal = ""
		return m.saveConfig(c)
	case "Toggle bubble fill":
		c := m.cfg
		c.Conversation.FillBubbles = !c.Conversation.FillBubbles
		m.modal = ""
		return m.saveConfig(c)
	case "Toggle bubble corners":
		c := m.cfg
		c.Conversation.Corners = "square"
		if m.cfg.Conversation.Corners == "square" {
			c.Conversation.Corners = "round"
		}
		m.modal = ""
		return m.saveConfig(c)
	case "Send message":
		m.modal = ""
		return m.send()
	}
	return nil
}
func (m *Model) modalKey(k tea.KeyMsg) tea.Cmd {
	if m.busy {
		return nil
	}
	// The AI surfaces own Esc so closing a review also clears its markers.
	switch m.modal {
	case "ai-review":
		return m.reviewKey(k)
	case "ai-edit":
		return m.editSuggestionKey(k)
	case "ai-instruction":
		return m.instructionKey(k)
	case "schedule-time":
		return m.scheduleTimeKey(k)
	case "queue":
		return m.queueKey(k)
	case "queue-item":
		if k.String() == "esc" || k.String() == "enter" || k.String() == "q" {
			m.modal = "queue"
		}
		return nil
	case "search-all":
		return m.globalSearchKey(k)
	case "contact":
		return m.contactDetailsKey(k)
	case "media":
		return m.mediaKey(k)
	case "settings":
		// A row being typed into takes every key, Esc included: there Esc
		// abandons the edit rather than closing the panel, so it has to be
		// claimed before the shared Esc below closes every modal.
		if m.settingEdit {
			return m.settingsEditKey(k)
		}
	}
	if k.String() == "esc" {
		m.modal = ""
		return nil
	}
	switch m.modal {
	case "help":
		switch k.String() {
		case "down", "j":
			m.choice++
		case "up", "k":
			m.choice = max(0, m.choice-1)
		case "pgdown":
			m.choice += max(1, m.height-10)
		case "pgup":
			m.choice = max(0, m.choice-m.height+10)
		}
		if k.String() == "?" || k.String() == "enter" {
			m.modal = ""
		}
		return nil
	case "number", "add", "edit":
		return m.formKey(k)
	case "delete":
		if k.String() == "enter" {
			c := m.editing
			m.busy = true
			return func() tea.Msg { return mutationMsg{deleted: c.ID, err: m.store.DeleteContact(c.ID)} }
		}
		return nil
	case "settings":
		// The panel is one list: move, change, commit. It scrolls only when it
		// outgrows the window, which the renderer decides.
		fields := m.settingsFields()
		switch k.String() {
		case "up", "k", "ctrl+k":
			m.choice = max(0, m.choice-1)
		case "down", "j", "ctrl+j":
			m.choice = min(len(fields)-1, m.choice+1)
		case "home", "g":
			m.choice = 0
		case "end", "G":
			m.choice = max(0, len(fields)-1)
		case "left", "h":
			m.settingsAdjust(-1)
		case "right", "l":
			m.settingsAdjust(1)
		case "enter", " ":
			return m.settingsActivate()
		}
		return nil
	}
	count := len(m.choices)
	if m.modal == "devices" {
		count = len(m.devices)
	}
	switch k.String() {
	case "up", "ctrl+k":
		m.choice = max(0, m.choice-1)
	case "down", "ctrl+j":
		m.choice = min(count-1, m.choice+1)
	case "j":
		if m.modal != "palette" {
			m.choice = min(count-1, m.choice+1)
			return nil
		}
	case "k":
		if m.modal != "palette" {
			m.choice = max(0, m.choice-1)
			return nil
		}
	case "enter":
		if count == 0 {
			return nil
		}
		m.choice = max(0, min(m.choice, count-1))
		switch m.modal {
		case "thread-themes":
			if m.history.active == nil {
				return nil
			}
			thread := m.history.active.ID
			theme := m.choices[m.choice]
			if theme == "inherit" {
				theme = ""
			}
			m.history.active.ThemeID = theme
			m.layoutConversation()
			m.modal = ""
			s := m.history.store
			return func() tea.Msg { return historySavedMsg{s.ThreadTheme(thread, theme)} }
		case "message":
			msg := m.history.view.Current()
			if msg == nil {
				m.modal = ""
				return nil
			}
			action := m.choices[m.choice]
			m.modal = ""
			switch action {
			case "Copy":
				return m.copyText(msg.Body)
			case "Reply":
				m.setPane(paneComposer)
			case "Quote":
				m.setPane(paneComposer)
				m.quoteMessage(msg.Body)
				return nil
			case "Search text":
				m.openGlobalSearch()
				m.searchInput.SetValue(msg.Body)
				return m.runGlobalSearch()
			case "View attachment":
				m.openMediaViewer(*msg)
				return nil
			case "Delete local copy":
				m.deleteMsgID = msg.ID
				m.modal = "delete-message"
				m.choice = 0
				m.choices = []string{"Delete", "Cancel"}
				return nil
			case "Retry":
				m.history.retryID = msg.ID
				m.editor.SetValue(msg.Body)
				m.trackChange()
				return m.send()
			}
			return nil
		case "delete-message":
			if m.choice != 0 || m.deleteMsgID == "" {
				m.modal = ""
				return nil
			}
			return m.deleteLocalCopy(m.deleteMsgID)
		case "open-attachment":
			return m.resolveOpenAttachment(m.choices[m.choice])
		case "compose":
			if m.choice < len(m.picks) {
				return m.pickRecipient(m.picks[m.choice])
			}
			return nil
		case "palette":
			return m.action(m.choices[m.choice])
		case "devices":
			c := m.cfg
			c.KDEConnect.PreferredDevice = m.devices[m.choice].ID
			m.modal = ""
			return m.saveConfig(c)
		case "themes":
			// Theming an entry from the phone promotes it to a contact the user
			// owns; the phone's own copy is untouched.
			c, err := ensureLocal(m.editing)
			if err != nil {
				m.notify("Could not create contact ID", true)
				return nil
			}
			c.Theme = m.choices[m.choice]
			if c.Theme == "automatic" {
				c.Theme = ""
			}
			m.busy = true
			return func() tea.Msg { return mutationMsg{contact: &c, err: m.store.SaveContact(c)} }
		case "ai-models":
			return m.commitAIModel(m.choices[m.choice])
		case "bubble-themes":
			return m.commitBubbleTheme(m.choices[m.choice])
		case "ai-policy":
			return m.commitAIPolicy(m.choices[m.choice])
		case "offline-send":
			return m.resolveOfflineSend(m.choices[m.choice])
		case "schedule":
			return m.resolveSchedule(m.choices[m.choice])
		}
	}
	if m.modal == "compose" && k.String() != "up" && k.String() != "down" && k.String() != "ctrl+j" && k.String() != "ctrl+k" {
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(k)
		m.refreshCompose()
		return cmd
	}
	if m.modal == "palette" && k.String() != "up" && k.String() != "down" && k.String() != "ctrl+j" && k.String() != "ctrl+k" {
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(k)
		m.choices = nil
		_, syncable := m.backend.(backend.ContactsBackend)
		for _, name := range commands {
			if !m.history.enabled && historyCommands[name] {
				continue
			}
			if name == "Sync phone contacts" && !syncable {
				continue
			}
			if fuzzy(name, m.filter.Value()) {
				m.choices = append(m.choices, name)
			}
		}
		m.choice = 0
		return cmd
	}
	return nil
}

// ensureLocal gives an entry a local identity, turning a read-only synced
// contact into one the user owns without touching the phone's copy.
func ensureLocal(c contacts.Contact) (contacts.Contact, error) {
	c.Synced = false
	if c.ID != "" {
		return c, nil
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return c, err
	}
	c.ID = fmt.Sprintf("%x", id)
	return c, nil
}
func fuzzy(s, q string) bool {
	rs := []rune(strings.ToLower(q))
	i := 0
	for _, r := range strings.ToLower(s) {
		if i < len(rs) && r == rs[i] {
			i++
		}
	}
	return i == len(rs)
}
func (m *Model) formKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "tab", "shift+tab", "down", "up":
		m.fields[m.field].Blur()
		step := 1
		if k.String() == "shift+tab" || k.String() == "up" {
			step = len(m.fields) - 1
		}
		m.field = (m.field + step) % len(m.fields)
		return m.fields[m.field].Focus()
	case "enter":
		if len(m.fields) > 1 && m.field == 0 {
			m.fields[0].Blur()
			m.field = 1
			return m.fields[1].Focus()
		}
		phoneField := len(m.fields) - 1
		phone, err := contacts.Normalize(m.fields[phoneField].Value())
		if err != nil {
			m.notify(err.Error(), true)
			return nil
		}
		if m.modal == "number" {
			m.choose(contacts.Contact{Name: phone, PhoneNumber: phone})
			m.modal = ""
			return nil
		}
		name := strings.TrimSpace(contacts.SafeLabel(m.fields[0].Value()))
		if name == "" {
			m.notify("Enter a contact name", true)
			return nil
		}
		c, err := ensureLocal(m.editing)
		if err != nil {
			m.notify("Could not create contact ID", true)
			return nil
		}
		c.Name = name
		c.PhoneNumber = phone
		m.busy = true
		return func() tea.Msg { return mutationMsg{contact: &c, err: m.store.SaveContact(c)} }
	}
	var cmd tea.Cmd
	m.fields[m.field], cmd = m.fields[m.field].Update(k)
	return cmd
}
