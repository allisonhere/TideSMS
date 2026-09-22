package backend

import (
	"context"
	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/domain"
)

type Device struct {
	ID, Name      string
	Connected     bool
	SMSCapability string
}
type SendRequest struct{ DeviceID, PhoneNumber, Message, ThreadID string }
type MessagingBackend interface {
	Devices(context.Context) ([]Device, error)
	Send(context.Context, SendRequest) error
}

// ContactsBackend is optional: a backend that can import the phone's address
// book as a read-only overlay. Imported entries never replace local contacts.
type ContactsBackend interface {
	SyncContacts(context.Context, string) ([]contacts.Synced, error)
}

// ConversationBackend extends the existing sender without forcing CLI-only
// fallback implementations to pretend they support history.
type ConversationBackend interface {
	MessagingBackend
	Threads(context.Context, string) ([]domain.Thread, error)
	Messages(context.Context, string, string, domain.MessageQuery) ([]domain.Message, error)
	Subscribe(context.Context, string) (<-chan domain.Event, error)
}
