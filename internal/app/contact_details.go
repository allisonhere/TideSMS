package app

import (
	"strings"

	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/storage"
	"github.com/allisonhere/tidesms/internal/themes"
	tea "github.com/charmbracelet/bubbletea"
)

// openContactDetails shows a summary of the selected contact plus the actions
// that already exist elsewhere in the interface.
func (m *Model) openContactDetails() {
	m.modal = "contact"
	m.choice = 0
	actions := []string{"Message", "Rename locally", "Change theme", "Change AI policy"}
	if mode, ok, err := m.store.NotificationMode(storage.ScopeContact, m.editing.PhoneNumber); err == nil && ok && mode == notifMuted {
		actions = append(actions, "Unmute notifications")
	} else {
		actions = append(actions, "Mute notifications")
	}
	m.choices = append(actions, "Copy number", "Close")
}

// contactDetailsHeader renders the summary above the action list.
func (m *Model) contactDetailsHeader() string {
	c := m.editing
	var b strings.Builder
	b.WriteString(c.Name + "\n")
	b.WriteString(c.PhoneNumber + "\n")
	theme := c.Theme
	if theme == "" {
		theme = "automatic"
	}
	b.WriteString("Theme: " + theme + "\n")
	policy := "inherit"
	notif := "normal"
	if m.store != nil {
		key := c.ID
		if key == "" {
			key = c.PhoneNumber
		}
		if p, _, _, ok, err := m.store.AIPolicy(storage.ScopeContact, key); err == nil && ok {
			policy = p
		}
		if mode, ok, err := m.store.NotificationMode(storage.ScopeContact, c.PhoneNumber); err == nil && ok {
			notif = mode
		}
	}
	b.WriteString("AI: " + policy + "\n")
	b.WriteString("Notifications: " + notif)
	return b.String()
}

// toggleContactMute flips the per-contact notification override.
func (m *Model) toggleContactMute() tea.Cmd {
	c := m.editing
	repo := m.store
	if repo == nil {
		return nil
	}
	muted := false
	if mode, ok, err := repo.NotificationMode(storage.ScopeContact, c.PhoneNumber); err == nil && ok && mode == notifMuted {
		muted = true
	}
	num := c.PhoneNumber
	return func() tea.Msg {
		if muted {
			return historySavedMsg{repo.ClearNotificationMode(storage.ScopeContact, num)}
		}
		return historySavedMsg{repo.SetNotificationMode(storage.ScopeContact, num, notifMuted)}
	}
}

// contactDetailsKey handles the inspector's action list.
func (m *Model) contactDetailsKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc", "q":
		m.modal = ""
		return nil
	case "up", "k", "ctrl+k":
		m.choice = max(0, m.choice-1)
		return nil
	case "down", "j", "ctrl+j":
		m.choice = min(len(m.choices)-1, m.choice+1)
		return nil
	case "enter":
		if m.choice < 0 || m.choice >= len(m.choices) {
			return nil
		}
		action := m.choices[m.choice]
		m.modal = ""
		switch action {
		case "Message":
			return m.pickRecipient(m.editing)
		case "Rename locally":
			m.openForm("edit", m.editing)
		case "Change theme":
			m.modal = "themes"
			m.choice = 0
			m.choices = append([]string{"automatic"}, themes.Names...)
		case "Change AI policy":
			m.openAIPolicyPicker(storage.ScopeContact)
		case "Mute notifications", "Unmute notifications":
			return m.toggleContactMute()
		case "Copy number":
			return m.copyText(m.editing.PhoneNumber)
		}
		return nil
	}
	return nil
}

// contactForDetails resolves the contact the inspector should edit.
func (m *Model) contactForDetails() (contacts.Contact, bool) {
	c, ok := m.selectedContact()
	if m.focus && (m.recipient.ID != "" || m.recipient.Synced) {
		c = m.recipient
		ok = true
	}
	return c, ok
}
