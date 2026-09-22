package app

import (
	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tidesms/internal/storage"
)

// Notification modes stored per contact or thread. "inherit" clears an
// override; a thread override beats a contact override, which beats the global
// [notifications] settings.
const (
	notifNormal  = "normal"
	notifMuted   = "muted"
	notifPrivacy = "privacy"
)

// notificationMode resolves the override for a message's thread and sender.
func (m *Model) notificationMode(threadID, sender string) string {
	if m.store == nil {
		return ""
	}
	if threadID != "" {
		if mode, ok, err := m.store.NotificationMode(storage.ScopeThread, threadID); err == nil && ok {
			return mode
		}
	}
	if sender != "" {
		if mode, ok, err := m.store.NotificationMode(storage.ScopeContact, sender); err == nil && ok {
			return mode
		}
	}
	return ""
}

// notificationText builds the title and body for an incoming message, honouring
// the global settings and any per-thread or per-contact override.
func (m *Model) notificationText(msg domain.Message) (title, body string) {
	name := msg.Sender
	for _, c := range m.contacts {
		if c.PhoneNumber == msg.Sender {
			name = c.Name
		}
	}
	title = name
	if !m.cfg.Notifications.ShowSender {
		title = "New message"
	}
	body = msg.Body
	mode := m.notificationMode(msg.ThreadID, msg.Sender)
	if !m.cfg.Notifications.ShowBody || m.cfg.Notifications.Privacy || mode == notifPrivacy {
		body = "New SMS"
	}
	return title, body
}
