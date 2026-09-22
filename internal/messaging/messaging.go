// Package messaging turns a queued item into a backend send. Both the terminal
// client and the optional background service use it, so delivery rules are not
// duplicated.
package messaging

import (
	"context"
	"github.com/allisonhere/tidesms/internal/backend"
	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tidesms/internal/queue"
)

// Backend is the part of the messaging backend the sender needs.
type Backend interface {
	Devices(context.Context) ([]backend.Device, error)
	Send(context.Context, backend.SendRequest) error
}

// Sender delivers one queue item. It reports queue.ErrOffline when no suitable
// phone is reachable, which the queue treats as a delay rather than a failure.
type Sender struct{ Backend Backend }

func New(b Backend) *Sender { return &Sender{Backend: b} }

func (s *Sender) Send(ctx context.Context, item queue.Item) error {
	devices, err := s.Backend.Devices(ctx)
	if err != nil {
		return err
	}
	dev, ok := Choose(devices, item.DeviceID)
	if !ok {
		return queue.ErrOffline
	}
	// The body is passed straight through and never logged here.
	return s.Backend.Send(ctx, backend.SendRequest{DeviceID: dev.ID, PhoneNumber: item.Recipient, Message: item.Body, ThreadID: item.ThreadID})
}

// StatusStore is the persistence the status mirror needs.
type StatusStore interface {
	MergeMessages([]domain.Message) ([]domain.Message, error)
	MessageStatus(string, domain.Status) error
}

// MirrorStatus keeps the local message row in step with a queue item, so a
// message queued offline or released from the schedule shows the same status in
// the conversation wherever it is read. The queue id doubles as the message id.
func MirrorStatus(s StatusStore) func(queue.Item) {
	return func(it queue.Item) {
		status := domain.Queued
		switch it.State {
		case queue.Sent:
			status = domain.Submitted
		case queue.Failed:
			status = domain.Failed
		}
		msg := domain.Message{
			ID: it.ID, DeviceID: it.DeviceID, ThreadID: it.ThreadID, Sender: "You", Body: it.Body,
			Timestamp: it.CreatedAt, Direction: domain.Outgoing, Status: status,
			Participants: []domain.Participant{domain.ParticipantFor(it.Recipient)},
		}
		if _, err := s.MergeMessages([]domain.Message{msg}); err != nil {
			return
		}
		_ = s.MessageStatus(it.ID, status)
	}
}

// Choose picks the device to send on: the requested one when it is connected
// and SMS-capable, otherwise the first connected SMS-capable device. It returns
// false when none can send.
func Choose(devices []backend.Device, want string) (backend.Device, bool) {
	var fallback *backend.Device
	for i := range devices {
		d := devices[i]
		if !d.Connected || d.SMSCapability != "available" {
			continue
		}
		if d.ID == want {
			return d, true
		}
		if fallback == nil {
			fallback = &devices[i]
		}
	}
	if fallback != nil {
		return *fallback, true
	}
	return backend.Device{}, false
}
