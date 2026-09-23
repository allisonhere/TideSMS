package app

import (
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidesms/internal/domain"
)

// holdFixture opens Amy's thread with a one-second undo window and a draft
// typed, ready for Enter.
func holdFixture(t *testing.T) (*Model, *driver, func() int) {
	t.Helper()
	m, b, _, d, _ := conversationFixture(t)
	m.cfg.Composer.UndoSeconds = 1
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	d.settle("history", func() bool { return len(m.history.view.Messages) == 3 })
	m.setPane(paneComposer)
	typeText(m, "On my way.")
	return m, d, func() int { return len(b.Sent) }
}

// Enter starts the window instead of sending; the message leaves only when it
// runs out, and then clears the composer as an immediate send does.
func TestUndoWindowDelaysTheSend(t *testing.T) {
	m, d, sent := holdFixture(t)
	d.press("enter")
	if m.hold == nil || sent() != 0 {
		t.Fatalf("Enter sent at once: hold %v, sends %d", m.hold, sent())
	}
	if !strings.Contains(m.notice, "Esc undo") {
		t.Errorf("the window does not say how to undo: %q", m.notice)
	}
	for _, msg := range m.history.view.Messages {
		if msg.Direction == domain.Outgoing && msg.Body == "On my way." {
			t.Fatal("a held message was stored before its window closed")
		}
	}
	d.settle("send", func() bool { return sent() == 1 && !m.sending })
	if m.hold != nil || m.editor.Value() != "" {
		t.Fatalf("after sending: hold %v, composer %q", m.hold, m.editor.Value())
	}
}

// Esc inside the window takes the message back: nothing is sent, and the
// draft is still there to edit.
func TestEscUndoesAHeldSend(t *testing.T) {
	m, d, sent := holdFixture(t)
	d.press("enter")
	d.press("esc")
	if m.hold != nil || m.sending {
		t.Fatalf("undo left the send in progress: hold %v, sending %v", m.hold, m.sending)
	}
	if m.editor.Value() != "On my way." {
		t.Fatalf("undo lost the draft: %q", m.editor.Value())
	}
	// Let the window's own tick arrive; it must find nothing to send.
	end := time.Now().Add(1500 * time.Millisecond)
	d.settle("the stale tick", func() bool { return time.Now().After(end) })
	if sent() != 0 {
		t.Fatalf("an undone message was sent %d times", sent())
	}
	// The composer is usable again, and a second send goes through.
	typeText(m, " Soon.")
	if m.editor.Value() != "On my way. Soon." {
		t.Fatalf("the composer stayed locked: %q", m.editor.Value())
	}
}

// A second Enter skips the wait.
func TestEnterSendsAHeldMessageNow(t *testing.T) {
	m, d, sent := holdFixture(t)
	m.cfg.Composer.UndoSeconds = 30
	d.press("enter")
	d.press("enter")
	d.settle("send", func() bool { return sent() == 1 && !m.sending })
	if m.hold != nil {
		t.Fatal("the hold outlived its send")
	}
}

// Quitting inside the window is refused rather than guessing whether the
// message should go.
func TestQuitWaitsForAHeldSend(t *testing.T) {
	m, d, sent := holdFixture(t)
	m.cfg.Composer.UndoSeconds = 30
	d.press("enter")
	if cmd := m.quit(); cmd != nil || m.quitting {
		t.Fatal("quit went ahead with a message in its undo window")
	}
	d.press("esc")
	if sent() != 0 {
		t.Fatal("refusing to quit sent the message")
	}
}
