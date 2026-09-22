package backend

import (
	"context"
	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/domain"
)

// Capabilities describes what a backend can actually do for a device, so the
// interface never offers a feature the transport cannot carry. It is
// deliberately conservative: an unknown capability is false.
type Capabilities struct {
	SendText       bool
	ReceiveText    bool
	Groups         bool
	ReceiveMedia   bool
	SendMedia      bool
	DeliveryStatus bool
	ContactSync    bool
}

type Device struct {
	ID, Name      string
	Connected     bool
	SMSCapability string
	Capabilities  Capabilities
}
type SendRequest struct{ DeviceID, PhoneNumber, Message, ThreadID string }
type MessagingBackend interface {
	Devices(context.Context) ([]Device, error)
	Send(context.Context, SendRequest) error
}

// AttachmentBackend is optional: a backend that can fetch a message part's
// file. It returns a local path to the downloaded file.
type AttachmentBackend interface {
	FetchAttachment(ctx context.Context, device string, partID int64, uniqueIdentifier string) (string, error)
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
