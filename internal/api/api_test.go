package api

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidesms/internal/backend/fake"
	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tidesms/internal/storage"
)

const (
	device = "phone"
	amy    = "+15551234567"
	mom    = "+15559876543"
)

var (
	amyThread    = domain.ThreadID(device, "1")
	familyThread = domain.ThreadID(device, "2")
	start        = time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC)
)

func msg(thread, sender, body string, minute int, out, unread bool, people ...string) domain.Message {
	m := domain.Message{DeviceID: device, ThreadID: thread, Sender: sender, Body: body, Timestamp: start.Add(time.Duration(minute) * time.Minute),
		Direction: domain.Incoming, Status: domain.Unknown, Unread: unread}
	if out {
		m.Direction, m.Status, m.Sender = domain.Outgoing, domain.Sent, "You"
	}
	for _, p := range people {
		m.Participants = append(m.Participants, domain.ParticipantFor(p))
	}
	m.BackendID = m.StableID()[:8]
	return m
}

func store(t *testing.T) *storage.Store {
	t.Helper()
	s, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.SaveContact(contacts.Contact{ID: "amy", Name: "Amy", PhoneNumber: amy}); err != nil {
		t.Fatal(err)
	}
	_, err = s.MergeMessages([]domain.Message{
		msg(amyThread, amy, "Are we still on?", 1, false, false, amy),
		msg(amyThread, "", "Yes!", 2, true, false, amy),
		msg(amyThread, amy, "Bring dessert", 30, false, true, amy),
		msg(familyThread, mom, "Call me", 10, false, true, amy, mom),
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Conversations come newest first, named from the address book, with the
// unread total across all of them even when the list is capped.
func TestListThreads(t *testing.T) {
	s := store(t)
	ts, err := ListThreads(s, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if ts.SchemaVersion != 1 || ts.Unread != 2 || len(ts.Threads) != 1 {
		t.Fatalf("threads %+v", ts)
	}
	got := ts.Threads[0]
	if got.ID != amyThread || got.Name != "Amy" || got.Unread != 1 || got.LastMessage != "Bring dessert" || !got.Replyable || got.Group {
		t.Fatalf("first thread %+v", got)
	}
	all, _ := ListThreads(s, 0, true)
	if len(all.Threads) != 2 {
		t.Fatalf("unread-only listed %d", len(all.Threads))
	}
	for _, th := range all.Threads {
		if th.ID == familyThread && (th.Replyable || !th.Group) {
			t.Errorf("a group was offered for reply: %+v", th)
		}
	}
}

// A conversation's messages name their sender and say which are yours.
func TestListMessages(t *testing.T) {
	ms, err := ListMessages(store(t), amyThread, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms.Messages) != 3 || ms.Messages[0].From != "Amy" || !ms.Messages[1].FromMe || ms.Messages[2].Body != "Bring dessert" || !ms.Messages[2].Unread {
		t.Fatalf("messages %+v", ms.Messages)
	}
	if _, err := ListMessages(store(t), "thread:phone:nope", 10); !errors.Is(err, ErrNoThread) {
		t.Errorf("unknown thread: %v", err)
	}
}

// A reply goes to the conversation's one number, is stored so the app shows
// it, and a group is refused before anything is sent.
func TestSend(t *testing.T) {
	s := store(t)
	b := fake.New()
	sent, err := Send(context.Background(), s, b, amyThread, "On my way", nil)
	if err != nil {
		t.Fatal(err)
	}
	if sent.Status != "submitted" || len(b.Sent) != 1 || b.Sent[0].PhoneNumber != amy || b.Sent[0].ThreadID != amyThread || b.Sent[0].Message != "On my way" {
		t.Fatalf("sent %+v via %+v", sent, b.Sent)
	}
	ms, _ := s.Messages(amyThread, 10)
	if last := ms[len(ms)-1]; last.Body != "On my way" || last.Status != domain.Submitted || last.Direction != domain.Outgoing {
		t.Fatalf("stored %+v", last)
	}
	if _, err := Send(context.Background(), s, b, familyThread, "hi", nil); err == nil || len(b.Sent) != 1 {
		t.Fatalf("group send: err %v, sends %d", err, len(b.Sent))
	}
	if _, err := Send(context.Background(), s, b, amyThread, "  ", nil); err == nil {
		t.Fatal("an empty reply was sent")
	}
}

// A failed send is stored as failed, so the app can retry it.
func TestFailedSendIsStored(t *testing.T) {
	s := store(t)
	b := fake.New()
	b.SendError = errors.New("phone disconnected")
	if _, err := Send(context.Background(), s, b, amyThread, "hello?", nil); err == nil {
		t.Fatal("no error")
	}
	ms, _ := s.Messages(amyThread, 10)
	if last := ms[len(ms)-1]; last.Body != "hello?" || last.Status != domain.Failed {
		t.Fatalf("stored %+v", last)
	}
}

// Marking read clears a conversation's unread count.
func TestMarkRead(t *testing.T) {
	s := store(t)
	if err := MarkRead(s, amyThread); err != nil {
		t.Fatal(err)
	}
	ts, _ := ListThreads(s, 0, false)
	if ts.Unread != 1 {
		t.Fatalf("unread after marking one thread read: %d", ts.Unread)
	}
}

// The TideDeck document follows the mail panel's shape: every conversation is
// an openable block, unread ones bright, and the unread total is the badge.
func TestDeck(t *testing.T) {
	s := store(t)
	ts, _ := ListThreads(s, 0, false)
	doc := Deck(ts, start.Add(40*time.Minute), false)
	if doc.SchemaVersion != 1 || doc.Badge == nil || doc.Badge.Text != "2" {
		t.Fatalf("doc %+v", doc)
	}
	var blocks []DeckRow
	for _, r := range doc.Rows {
		if r.Type == "block" {
			blocks = append(blocks, r)
		}
	}
	if len(blocks) != 2 || blocks[0].ID != amyThread || blocks[0].Label != "Amy · 10m · 1 new" || blocks[0].Tone != "good" || blocks[0].Body[0] != "Bring dessert" {
		t.Fatalf("rows %+v", blocks)
	}
	// It must be a document TideDeck can read: plain JSON with its field names.
	raw, _ := json.Marshal(doc)
	for _, field := range []string{`"schemaVersion":1`, `"rows":`, `"type":"block"`, `"id":"` + amyThread + `"`, `"badge":{"text":"2"`} {
		if !strings.Contains(string(raw), field) {
			t.Errorf("document lacks %s: %s", field, raw)
		}
	}
	empty := Deck(Threads{}, start, true)
	if len(empty.Rows) != 1 || empty.Rows[0].Value != "nothing new" || empty.Badge != nil {
		t.Errorf("empty unread panel %+v", empty)
	}
}
