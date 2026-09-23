package reply

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidesms/internal/backend/fake"
	"github.com/allisonhere/tidesms/internal/config"
	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tidesms/internal/storage"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

var (
	amyThread    = domain.ThreadID("phone", "1")
	familyThread = domain.ThreadID("phone", "2")
)

func setup(t *testing.T, undo int) (*Model, *fake.Backend, *storage.Store) {
	t.Helper()
	s, err := storage.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	_ = s.SaveContact(contacts.Contact{ID: "amy", Name: "Amy", PhoneNumber: "+15551234567"})
	at := time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC)
	in := domain.Message{DeviceID: "phone", ThreadID: amyThread, Sender: "+15551234567", Body: "Bring dessert", Timestamp: at,
		Direction: domain.Incoming, Unread: true, Participants: []domain.Participant{domain.ParticipantFor("+15551234567")}, BackendID: "a1"}
	group := domain.Message{DeviceID: "phone", ThreadID: familyThread, Sender: "+15559876543", Body: "Call me", Timestamp: at,
		Direction: domain.Incoming, BackendID: "g1",
		Participants: []domain.Participant{domain.ParticipantFor("+15551234567"), domain.ParticipantFor("+15559876543")}}
	if _, err := s.MergeMessages([]domain.Message{in, group}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Composer.UndoSeconds = undo
	b := fake.New()
	m, err := New(context.Background(), s, b, cfg, amyThread)
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m, b, s
}

// run feeds the model a key and follows the commands it returns until one
// would wait, as Bubble Tea would.
func run(m *Model, msg tea.Msg) {
	_, cmd := m.Update(msg)
	for i := 0; cmd != nil && i < 10; i++ {
		out := cmd()
		if batch, ok := out.(tea.BatchMsg); ok && len(batch) == 0 {
			return
		}
		if _, ok := out.(tea.QuitMsg); ok {
			return
		}
		_, cmd = m.Update(out)
	}
}

func typeText(m *Model, s string) { m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}) }

// The window shows the conversation, and opening it marks it read.
func TestWindowShowsTheConversation(t *testing.T) {
	m, _, s := setup(t, 0)
	view := ansi.Strip(m.View())
	for _, want := range []string{"Amy", "Bring dessert", "Reply"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	ms, _ := s.Messages(amyThread, 10)
	if ms[0].Unread {
		t.Error("opening the reply window left the message unread")
	}
}

// Enter sends to the conversation's number and closes the window.
func TestEnterSends(t *testing.T) {
	m, b, _ := setup(t, 0)
	typeText(m, "On my way")
	run(m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(b.Sent) != 1 || b.Sent[0].Message != "On my way" || b.Sent[0].PhoneNumber != "+15551234567" {
		t.Fatalf("sent %+v", b.Sent)
	}
	if !m.Sent || !m.done {
		t.Fatalf("after sending: sent %v, closed %v", m.Sent, m.done)
	}
}

// Esc closes without sending, and the reply waits as a draft the app shows.
func TestEscKeepsTheDraft(t *testing.T) {
	m, b, s := setup(t, 0)
	typeText(m, "half a thought")
	run(m, tea.KeyMsg{Type: tea.KeyEsc})
	if len(b.Sent) != 0 || !m.done {
		t.Fatalf("esc: sent %d, closed %v", len(b.Sent), m.done)
	}
	ds, _ := s.Drafts()
	if ds[amyThread].Body != "half a thought" {
		t.Fatalf("draft %q", ds[amyThread].Body)
	}
	// And it is there again next time.
	again, err := New(context.Background(), s, b, config.Default(), amyThread)
	if err != nil || again.editor.Value() != "half a thought" {
		t.Fatalf("reopened with %q, %v", again.editor.Value(), err)
	}
}

// The undo window applies here too: Esc inside it takes the reply back.
func TestUndoWindow(t *testing.T) {
	m, b, _ := setup(t, 30)
	typeText(m, "oops")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(m.notice, "Esc undo") || len(b.Sent) != 0 {
		t.Fatalf("notice %q, sent %d", m.notice, len(b.Sent))
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.sending || m.done || m.editor.Value() != "oops" {
		t.Fatalf("undo: sending %v, closed %v, text %q", m.sending, m.done, m.editor.Value())
	}
}

// A group cannot be replied to, so the window refuses to open on one.
func TestGroupsAreRefused(t *testing.T) {
	_, b, s := setup(t, 0)
	if _, err := New(context.Background(), s, b, config.Default(), familyThread); err == nil || !strings.Contains(err.Error(), "group") {
		t.Fatalf("group: %v", err)
	}
}
