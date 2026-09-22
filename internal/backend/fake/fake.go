// Package fake provides deterministic, concurrent-safe backend fixtures.
package fake

import (
	"context"
	"errors"
	"github.com/allisonhere/tidesms/internal/backend"
	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/domain"
	"sort"
	"sync"
	"time"
)

// Request records one history page the engine asked for, so tests can assert
// that repeated synchronization does not re-download known messages.
type Request struct {
	Thread string
	Query  domain.MessageQuery
}
type Backend struct {
	mu         sync.Mutex
	DeviceList []backend.Device
	ThreadList []domain.Thread
	History    map[string][]domain.Message
	SendError  error
	// SendDelay simulates a slow transport; it is applied even when SendError
	// is set, so tests can combine the two.
	SendDelay     time.Duration
	ThreadsError  error
	Sent          []backend.SendRequest
	Requests      []Request
	ThreadCalls   int
	Contacts      []contacts.Synced
	ContactsError error
	ContactCalls  int
	// AttachmentPath is returned by FetchAttachment; empty means a missing file.
	AttachmentPath  string
	AttachmentError error
	AttachmentCalls []int64
	subs            map[chan domain.Event]context.Context
}

func New() *Backend {
	return &Backend{DeviceList: []backend.Device{{ID: "phone", Name: "Fixture phone", Connected: true, SMSCapability: "available", Capabilities: backend.Capabilities{SendText: true, ReceiveText: true, Groups: true, ReceiveMedia: true, ContactSync: true}}}, History: map[string][]domain.Message{}, subs: map[chan domain.Event]context.Context{}}
}
func (b *Backend) Devices(context.Context) ([]backend.Device, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]backend.Device{}, b.DeviceList...), nil
}
func (b *Backend) Threads(ctx context.Context, device string) ([]domain.Thread, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if b.ThreadsError != nil {
		return nil, b.ThreadsError
	}
	if len(b.DeviceList) == 0 || !b.DeviceList[0].Connected {
		return nil, errors.New("phone offline")
	}
	b.ThreadCalls++
	return append([]domain.Thread{}, b.ThreadList...), nil
}
func (b *Backend) Messages(ctx context.Context, device, thread string, q domain.MessageQuery) ([]domain.Message, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if len(b.DeviceList) == 0 || !b.DeviceList[0].Connected {
		return nil, errors.New("phone offline")
	}
	b.Requests = append(b.Requests, Request{thread, q})
	ms := append([]domain.Message{}, b.History[domain.ThreadID(device, thread)]...)
	sort.Slice(ms, func(i, j int) bool { return ms[i].Timestamp.After(ms[j].Timestamp) })
	start := min(len(ms), q.Offset)
	return ms[start:min(len(ms), start+q.Limit)], nil
}
func (b *Backend) SyncContacts(ctx context.Context, device string) ([]contacts.Synced, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	b.ContactCalls++
	if b.ContactsError != nil {
		return nil, b.ContactsError
	}
	if len(b.DeviceList) == 0 || !b.DeviceList[0].Connected {
		return nil, errors.New("phone offline")
	}
	return append([]contacts.Synced{}, b.Contacts...), nil
}
func (b *Backend) FetchAttachment(_ context.Context, _ string, partID int64, _ string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.AttachmentCalls = append(b.AttachmentCalls, partID)
	if b.AttachmentError != nil {
		return "", b.AttachmentError
	}
	return b.AttachmentPath, nil
}

func (b *Backend) Send(ctx context.Context, r backend.SendRequest) error {
	b.mu.Lock()
	delay, sendErr := b.SendDelay, b.SendError
	b.mu.Unlock()
	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Sent = append(b.Sent, r)
	return sendErr
}
func (b *Backend) Subscribe(ctx context.Context, device string) (<-chan domain.Event, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan domain.Event, 128)
	b.subs[ch] = ctx
	go func() { <-ctx.Done(); b.mu.Lock(); defer b.mu.Unlock(); delete(b.subs, ch); close(ch) }()
	return ch, nil
}
func (b *Backend) Emit(e domain.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if e.Message != nil {
		m := *e.Message
		b.History[m.ThreadID] = append(b.History[m.ThreadID], m)
	}
	for ch, ctx := range b.subs {
		select {
		case ch <- e:
		case <-ctx.Done():
		}
	}
}
func (b *Backend) Connection(online bool) {
	b.mu.Lock()
	b.DeviceList[0].Connected = online
	b.mu.Unlock()
	b.Emit(domain.Event{Kind: domain.EventRefresh, Connected: online})
}
