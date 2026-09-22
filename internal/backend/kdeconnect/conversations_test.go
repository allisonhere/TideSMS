package kdeconnect

import (
	"context"
	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/godbus/dbus/v5"
	"os"
	"testing"
	"time"
)

func TestDecodeMessage(t *testing.T) {
	w := wireMessage{Event: 1, Body: "Hello 世界", Addresses: []wireAddress{{"+1 (555) 123-4567"}}, Date: 1700000000000, Type: 1, Thread: 42, UID: 9, Subscription: -1}
	m, err := decodeMessage("phone", dbus.MakeVariant(w))
	if err != nil {
		t.Fatal(err)
	}
	if m.Direction != domain.Incoming || !m.Unread || m.Sender != "+15551234567" || m.BackendID != "9" {
		t.Fatalf("%+v", m)
	}
	w.Type = 2
	w.Event = 3
	w.Addresses = append(w.Addresses, wireAddress{"12345"})
	m, err = decodeMessage("phone", dbus.MakeVariant(w))
	if err != nil || !m.IsGroup || m.Direction != domain.Outgoing || m.Status != domain.Sent || m.Unread {
		t.Fatalf("%+v %v", m, err)
	}
}
func TestLiveConversations(t *testing.T) {
	id := os.Getenv("TIDESMS_TEST_DEVICE")
	if id == "" {
		t.Skip("opt-in read-only history integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	c := New(nil, false)
	events, err := c.Subscribe(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range events {
		}
	}()
	ts, err := c.Threads(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("discovered %d threads (content redacted)", len(ts))
	if len(ts) == 0 {
		t.Fatal("no conversation metadata returned")
	}
	ms, err := c.Messages(ctx, id, ts[0].BackendID, domain.MessageQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) == 0 {
		t.Fatal("history request returned no messages")
	}
	t.Logf("decoded %d history messages (content redacted)", len(ms))
}

func TestDeviceIdentifiersMustBeExportable(t *testing.T) {
	for _, id := range []string{"2fe57f2a_4a0e_4bcb_a2e0_1f6b5c7d9e00", "Pixel9"} {
		if err := checkDevice(id); err != nil {
			t.Fatalf("rejected a real identifier %q: %v", id, err)
		}
	}
	for _, id := range []string{"", "test-phone", "a/b", "dev ice", "../escape"} {
		if err := checkDevice(id); err == nil {
			t.Fatalf("accepted %q", id)
		}
		if _, err := (&Client{}).Messages(context.Background(), id, "1", domain.MessageQuery{Limit: 1}); err == nil {
			t.Fatalf("Messages accepted %q", id)
		}
	}
}

// A phone that lists one person's address more than once, or in two formats,
// must not turn a one-to-one conversation into an unanswerable group.
func TestDuplicateAddressesAreNotAGroup(t *testing.T) {
	build := func(addresses ...string) domain.Message {
		var as []wireAddress
		for _, a := range addresses {
			as = append(as, wireAddress{a})
		}
		m, err := decodeMessage("phone", dbus.MakeVariant(wireMessage{
			Event: 3, Body: "hi", Addresses: as, Date: 1700000000000, Type: 1, Thread: 7, UID: 9,
		}))
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	for _, addresses := range [][]string{
		{"+18165550182"},
		{"+18165550182", "+18165550182"},
		{"+18165550182", "+1 (816) 555-0182"},
		{"+18165550182", "8165550182"},
	} {
		m := build(addresses...)
		if m.IsGroup {
			t.Errorf("%v became a group", addresses)
		}
		if len(m.Participants) != 1 {
			t.Errorf("%v kept %d participants", addresses, len(m.Participants))
		}
		if m.Sender != "+18165550182" {
			t.Errorf("%v lost its sender: %q", addresses, m.Sender)
		}
	}
	m := build("+18165550182", "+15559876543")
	if !m.IsGroup || len(m.Participants) != 2 || m.Sender != "Group participant" {
		t.Fatalf("a real group was flattened: %+v", m)
	}
}
