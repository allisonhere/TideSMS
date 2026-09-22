package app

import (
	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tidesms/internal/queue"
	tea "github.com/charmbracelet/bubbletea"
	"testing"
	"time"
)

// composeForTest puts a message in the composer on the open thread.
func composeForTest(t *testing.T, m *Model, d *driver, body string) {
	t.Helper()
	m.setPane(paneComposer)
	typeText(m, body)
}

func TestOfflineSendQueuesThenSendsOnReconnect(t *testing.T) {
	m, b, st, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	composeForTest(t, m, d, "queued hello")

	// Take the phone offline in both the model's view and the backend, so the
	// queue really cannot send until reconnect.
	b.Connection(false)
	m.devices[0].Connected = false
	if cmd := m.sendHistory(); cmd != nil {
		t.Fatal("offline send should prompt, not return a send command")
	}
	if m.modal != "offline-send" {
		t.Fatalf("modal = %q", m.modal)
	}
	m.choice = 0 // Queue for later
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))

	if n, err := st.CountQueue(queue.Queued); err != nil || n != 1 {
		t.Fatalf("queued = %d err=%v", n, err)
	}
	if m.editor.Value() != "" {
		t.Fatalf("queued draft not cleared: %q", m.editor.Value())
	}

	// Still offline: processing must leave it queued.
	b.Connection(false)
	d.run(m.processQueue())
	if n, _ := st.CountQueue(queue.Queued); n != 1 {
		t.Fatalf("offline item was not left queued: %d", n)
	}

	// Reconnect and process: it sends exactly once.
	m.devices[0].Connected = true
	b.Connection(true)
	d.run(m.processQueue())
	if len(b.Sent) != 1 || b.Sent[0].Message != "queued hello" {
		t.Fatalf("sent = %+v", b.Sent)
	}
	if n, _ := st.CountQueue(queue.Queued); n != 0 {
		t.Fatal("item still queued after send")
	}
	// A duplicate pass must not send again.
	d.run(m.processQueue())
	if len(b.Sent) != 1 {
		t.Fatalf("duplicate send: %+v", b.Sent)
	}
	msgs, err := st.Messages(amyThread, 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, msg := range msgs {
		if msg.Body == "queued hello" {
			found = true
			if msg.Status != domain.Submitted {
				t.Fatalf("status = %s", msg.Status)
			}
		}
	}
	if !found {
		t.Fatal("queued message missing from the conversation")
	}
}

// A Google Voice-style thread listing one number in several formats is still a
// single recipient, so sending is allowed even if the stored flag is stale.
func TestSameNumberFormatsAllowSending(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	m.history.active.IsGroup = true
	m.history.active.Participants = []domain.Participant{domain.ParticipantFor("+15124100124"), domain.ParticipantFor("5124100124")}
	m.setPane(paneComposer)
	typeText(m, "hello gv")
	if !m.prepareSend() {
		t.Fatal("one person written two ways was treated as a group")
	}
	if m.pending == nil || m.pending.phone != "+15124100124" {
		t.Fatalf("pending = %+v", m.pending)
	}
}

func TestScheduledMessageReleasesWhenDue(t *testing.T) {
	m, b, st, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	composeForTest(t, m, d, "see you tomorrow")

	if cmd := m.openSchedule(); cmd != nil {
		t.Fatal("schedule should open a picker first")
	}
	if m.modal != "schedule" {
		t.Fatalf("modal = %q", m.modal)
	}
	pickChoice(t, m, "In 30 minutes")
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))

	if n, err := st.CountScheduled(); err != nil || n != 1 {
		t.Fatalf("scheduled = %d err=%v", n, err)
	}
	items, _ := st.Scheduled()
	if len(items) != 1 || !items[0].SendAfter.After(time.Now()) {
		t.Fatalf("scheduled item = %+v", items)
	}
	if m.editor.Value() != "" {
		t.Fatalf("scheduled draft not cleared: %q", m.editor.Value())
	}

	// Nothing goes out before the time arrives.
	d.run(m.processQueue())
	if len(b.Sent) != 0 {
		t.Fatal("scheduled message left early")
	}

	// Make it due and process; it becomes a normal outgoing message.
	item := items[0]
	item.SendAfter = time.Now().Add(-time.Minute)
	if err := st.UpdateScheduled(item); err != nil {
		t.Fatal(err)
	}
	b.Connection(true)
	d.run(m.processQueue())
	if len(b.Sent) != 1 || b.Sent[0].Message != "see you tomorrow" {
		t.Fatalf("sent = %+v", b.Sent)
	}
	if n, _ := st.CountScheduled(); n != 0 {
		t.Fatal("scheduled item not cleared on release")
	}
}

func TestQueueViewRemovesAndCounts(t *testing.T) {
	m, b, st, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	composeForTest(t, m, d, "discard me")
	b.Connection(false)
	m.devices[0].Connected = false
	m.sendHistory()
	m.choice = 0
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	_ = b

	d.run(m.refreshCounts())
	if m.queuedCount != 1 {
		t.Fatalf("status count = %d", m.queuedCount)
	}

	if cmd, ok := m.outboxAction("Open outgoing queue"); !ok || cmd != nil {
		t.Fatal("queue should open synchronously")
	}
	if m.modal != "queue" || len(m.outboxEntries) != 1 {
		t.Fatalf("queue view: modal=%q entries=%d", m.modal, len(m.outboxEntries))
	}
	m.choice = 0
	d.run(m.removeOutboxEntry())
	if n, _ := st.CountQueue(); n != 0 {
		t.Fatal("queue item not removed")
	}
	msgs, _ := st.Messages(amyThread, 10)
	for _, msg := range msgs {
		if msg.Body == "discard me" {
			t.Fatal("removed queued message still in the conversation")
		}
	}
}
