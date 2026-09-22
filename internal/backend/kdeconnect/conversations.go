package kdeconnect

import (
	"context"
	"errors"
	"fmt"
	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/godbus/dbus/v5"
	"sort"
	"strconv"
	"strings"
	"time"
)

const service = "org.kde.kdeconnect"
const conversations = "org.kde.kdeconnect.device.conversations"

func devicePath(id string) dbus.ObjectPath {
	return dbus.ObjectPath("/modules/kdeconnect/devices/" + id)
}

// KDE Connect filters device identifiers to [A-Za-z0-9_] when it generates them,
// because the daemon exports each device at a path built from the identifier.
// Anything else cannot name a real device, so it is refused rather than rewritten
// into a path that might address a different phone.
func checkDevice(id string) error {
	if id == "" {
		return errors.New("no phone selected")
	}
	for _, r := range id {
		if r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			continue
		}
		return errors.New("this phone's KDE Connect identifier cannot be addressed on D-Bus; reselect the device")
	}
	return nil
}

type wireAddress struct{ Address string }
type wireAttachment struct {
	PartID                      int64
	MIME, Thumbnail, Identifier string
}
type wireMessage struct {
	Event        int32
	Body         string
	Addresses    []wireAddress
	Date         int64
	Type, Read   int32
	Thread       int64
	UID          int32
	Subscription int64
	Attachments  []wireAttachment
}

func decodeMessage(device string, v dbus.Variant) (domain.Message, error) {
	var w wireMessage
	if err := dbus.Store([]any{v.Value()}, &w); err != nil {
		return domain.Message{}, fmt.Errorf("unsupported KDE Connect message format: %w", err)
	}
	if w.Thread < 0 || w.Date < 0 {
		return domain.Message{}, errors.New("invalid KDE Connect message identity")
	}
	m := domain.Message{DeviceID: device, ThreadID: domain.ThreadID(device, strconv.FormatInt(w.Thread, 10)), Body: w.Body, Timestamp: time.UnixMilli(w.Date), Status: domain.Unknown, Direction: domain.Incoming, Unread: w.Read == 0}
	if w.UID >= 0 {
		m.BackendID = strconv.FormatInt(int64(w.UID), 10)
	}
	for _, a := range w.Addresses {
		m.Participants = append(m.Participants, domain.ParticipantFor(a.Address))
	}
	// KDE Connect's multi-target flag is only corroborating: it is set whenever
	// the phone listed several addresses, including the same person twice. The
	// number of distinct people is what actually decides whether this is a group.
	m.Participants = domain.DedupeParticipants(m.Participants)
	m.IsGroup = len(m.Participants) > 1
	if len(m.Participants) == 1 {
		m.Sender = m.Participants[0].Number
	} else {
		m.Sender = "Group participant"
	}
	switch w.Type {
	case 2:
		m.Direction = domain.Outgoing
		m.Status = domain.Sent
	case 3:
		m.Direction = domain.Outgoing
	case 4:
		m.Direction = domain.Outgoing
		m.Status = domain.Sending
	case 5:
		m.Direction = domain.Outgoing
		m.Status = domain.Failed
	case 6:
		m.Direction = domain.Outgoing
		m.Status = domain.Queued
	}
	if m.Direction == domain.Outgoing {
		m.Sender = "You"
		m.Unread = false
	}
	m.ID = m.StableID()
	// Attachments are metadata-only until fetched; the message renders without
	// waiting for them.
	for _, a := range w.Attachments {
		m.Attachments = append(m.Attachments, domain.Attachment{
			ID:        m.ID + ":" + strconv.FormatInt(a.PartID, 10),
			MessageID: m.ID,
			MIMEType:  a.MIME,
			RemoteID:  a.Identifier,
			State:     domain.AttachmentMetadata,
		})
	}
	return m, nil
}
func connect(ctx context.Context) (*dbus.Conn, error) {
	conn, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if err != nil {
		return nil, errors.New("KDE Connect D-Bus unavailable; cached history remains usable")
	}
	return conn, nil
}
func signals(conn *dbus.Conn, device string) (chan *dbus.Signal, error) {
	ch := make(chan *dbus.Signal, 2048)
	conn.Signal(ch)
	err := conn.AddMatchSignal(dbus.WithMatchSender(service), dbus.WithMatchObjectPath(devicePath(device)))
	if err != nil {
		conn.RemoveSignal(ch)
		return nil, err
	}
	return ch, nil
}
func call(ctx context.Context, conn *dbus.Conn, device, method string, args ...any) *dbus.Call {
	return conn.Object(service, devicePath(device)).CallWithContext(ctx, conversations+"."+method, 0, args...)
}
func (c *Client) Threads(ctx context.Context, device string) ([]domain.Thread, error) {
	if err := checkDevice(device); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := connect(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	ch, err := signals(conn, device)
	if err != nil {
		return nil, err
	}
	if err = call(ctx, conn, device, "requestAllConversationThreads").Err; err != nil {
		return nil, errors.New("SMS conversations unavailable; enable the SMS plugin and reconnect")
	}
	// The refresh has no completion signal. Wait for a bounded quiet period, then
	// take the daemon's snapshot. Later arrivals continue through Subscribe.
	idle := time.NewTimer(900 * time.Millisecond)
	defer idle.Stop()
	deadline := time.NewTimer(4 * time.Second)
	defer deadline.Stop()
wait:
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-idle.C:
			break wait
		case <-deadline.C:
			break wait
		case s, ok := <-ch:
			if !ok {
				return nil, errors.New("D-Bus connection closed")
			}
			if strings.HasPrefix(s.Name, conversations+".") {
				if !idle.Stop() {
					select {
					case <-idle.C:
					default:
					}
				}
				idle.Reset(500 * time.Millisecond)
			}
		}
	}
	var values []dbus.Variant
	if err = call(ctx, conn, device, "activeConversations").Store(&values); err != nil {
		return nil, errors.New("could not read KDE Connect conversations")
	}
	out := make([]domain.Thread, 0, len(values))
	for _, v := range values {
		m, e := decodeMessage(device, v)
		if e != nil {
			return nil, e
		}
		t := m.Thread()
		t.LastBackendID = m.BackendID
		t.BackendID = strings.TrimPrefix(t.ID, domain.ThreadID(device, ""))
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastTimestamp.After(out[j].LastTimestamp) })
	return out, nil
}
func (c *Client) Messages(ctx context.Context, device, thread string, q domain.MessageQuery) ([]domain.Message, error) {
	if err := checkDevice(device); err != nil {
		return nil, err
	}
	id, err := strconv.ParseInt(thread, 10, 64)
	if err != nil {
		return nil, errors.New("thread is not yet known to the phone")
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	conn, err := connect(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	ch, err := signals(conn, device)
	if err != nil {
		return nil, err
	}
	target := max(1, q.Offset+q.Limit)
	if target > 100000 {
		return nil, errors.New("history window exceeds 100,000 messages")
	}
	// Always start at zero: several KDE versions dereference out-of-range offsets.
	// Replayed rows are deduplicated locally; only the requested page is returned.
	if err = call(ctx, conn, device, "requestConversation", id, int32(0), int32(target)).Err; err != nil {
		return nil, errors.New("could not request message history")
	}
	got := map[string]domain.Message{}
	idle := time.NewTimer(3 * time.Second)
	defer idle.Stop()
	for {
		select {
		case <-ctx.Done():
			if len(got) == 0 {
				return nil, errors.New("message history timed out; retry when the phone is online")
			}
			return messagePage(got, q), nil
		case <-idle.C:
			return messagePage(got, q), nil
		case s, ok := <-ch:
			if !ok {
				return nil, errors.New("D-Bus connection closed")
			}
			if (s.Name != conversations+".conversationUpdated" && s.Name != conversations+".conversationCreated") || len(s.Body) == 0 {
				continue
			}
			v, ok := s.Body[0].(dbus.Variant)
			if !ok {
				continue
			}
			m, e := decodeMessage(device, v)
			if e != nil {
				return nil, e
			}
			if m.ThreadID != domain.ThreadID(device, thread) {
				continue
			}
			got[m.ID] = m
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(1500 * time.Millisecond)
			if len(got) >= target {
				return messagePage(got, q), nil
			}
		}
	}
}
func messagePage(got map[string]domain.Message, q domain.MessageQuery) []domain.Message {
	out := make([]domain.Message, 0, len(got))
	for _, m := range got {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Timestamp.Equal(out[j].Timestamp) {
			return out[i].ID > out[j].ID
		}
		return out[i].Timestamp.After(out[j].Timestamp)
	})
	start := min(max(0, q.Offset), len(out))
	end := min(len(out), start+max(1, q.Limit))
	return out[start:end]
}
func (c *Client) Subscribe(ctx context.Context, device string) (<-chan domain.Event, error) {
	if err := checkDevice(device); err != nil {
		return nil, err
	}
	conn, err := connect(ctx)
	if err != nil {
		return nil, err
	}
	ch, err := signals(conn, device)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err = conn.AddMatchSignal(dbus.WithMatchInterface("org.freedesktop.DBus"), dbus.WithMatchMember("NameOwnerChanged"), dbus.WithMatchArg(0, service)); err != nil {
		_ = conn.Close()
		return nil, err
	}
	out := make(chan domain.Event, 256)
	go func() {
		defer close(out)
		defer func() { _ = conn.Close() }()
		emit := func(e domain.Event) bool {
			select {
			case out <- e:
				return true
			case <-ctx.Done():
				return false
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-conn.Context().Done():
				emit(domain.Event{Kind: domain.EventConnection})
				return
			case s, ok := <-ch:
				if !ok {
					return
				}
				if s == nil {
					continue
				}
				switch s.Name {
				case conversations + ".conversationCreated", conversations + ".conversationUpdated":
					if len(s.Body) == 0 {
						continue
					}
					v, ok := s.Body[0].(dbus.Variant)
					if !ok {
						continue
					}
					m, e := decodeMessage(device, v)
					if e != nil {
						if !emit(domain.Event{Kind: domain.EventError, Err: e}) {
							return
						}
						continue
					}
					if !emit(domain.Event{Kind: domain.EventMessage, Message: &m}) {
						return
					}
				case "org.kde.kdeconnect.device.reachableChanged", "org.kde.kdeconnect.device.pluginsChanged", "org.freedesktop.DBus.NameOwnerChanged":
					if !emit(domain.Event{Kind: domain.EventRefresh}) {
						return
					}
				}
			}
		}
	}()
	return out, nil
}
