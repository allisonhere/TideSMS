package app

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/allisonhere/tidesms/internal/clock"
	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tidesms/internal/messaging"
	"github.com/allisonhere/tidesms/internal/queue"
	"github.com/allisonhere/tidesms/internal/scheduler"
	"github.com/allisonhere/tidesms/internal/storage"
	tea "github.com/charmbracelet/bubbletea"
)

// outboxStore is the persistence the queue and schedule need. storage.Store
// implements it; the TUI and the background service both drive the same code.
type outboxStore interface {
	queue.Store
	Enqueue(queue.Item) error
	Queue() ([]queue.Item, error)
	QueueItem(string) (queue.Item, bool, error)
	RemoveQueue(string) error
	CountQueue(...queue.State) (int, error)
	ReleaseOfflineWaits() error
	MessageStatus(string, domain.Status) error
	MergeMessages([]domain.Message) ([]domain.Message, error)
	DeleteLocalMessage(string) error
	Schedule(scheduler.Item) error
	UpdateScheduled(scheduler.Item) error
	DueScheduled(time.Time) ([]scheduler.Item, error)
	ClaimScheduled(string, time.Time) (bool, error)
	Scheduled() ([]scheduler.Item, error)
	ScheduledItem(string) (scheduler.Item, bool, error)
	RemoveScheduled(string) error
	CountScheduled() (int, error)
}

func (m *Model) outbox() (outboxStore, bool) {
	s, ok := m.store.(outboxStore)
	return s, ok
}

// pendingSend is a composed message waiting to be delivered, queued or
// scheduled. It is built once so every path shares the same validation.
type pendingSend struct {
	msg      domain.Message
	draftKey string
	draft    storage.Draft
	phone    string
}

// outboxChangedMsg reports a queue or schedule mutation.
type outboxChangedMsg struct {
	err       error
	queued    bool
	scheduled bool
	draftKey  string
	draft     storage.Draft
	when      time.Time
}

// queueProcessedMsg reports the outcome of a queue/schedule pass.
type queueProcessedMsg struct {
	sent, failed int
	err          error
}

// countsMsg refreshes the status bar's queued and scheduled tallies.
type countsMsg struct{ queued, scheduled int }

// localID is a fresh identifier for a locally composed message.
func localID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return fmt.Sprintf("local-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("local-%x", id)
}

// deviceOnline reports whether a phone can send right now.
func (m *Model) deviceOnline(deviceID string) bool {
	for _, d := range m.devices {
		if d.ID == deviceID {
			return d.Connected && d.SMSCapability == "available"
		}
	}
	return false
}

// prepareSend validates the composer and captures the message to deliver. It
// returns false when sending is not possible, having explained why.
func (m *Model) prepareSend() bool {
	h := &m.history
	if m.sending || !m.loaded {
		return false
	}
	if strings.TrimSpace(m.editor.Value()) == "" {
		m.notify("Write a message before sending", true)
		return false
	}
	if h.active != nil && h.active.IsGroup {
		m.notify("Group history is readable; group SMS sending is not supported by this backend", true)
		return false
	}
	if m.recipient.PhoneNumber == "" {
		m.notify("Choose a recipient first", true)
		return false
	}
	if m.deviceID == "" {
		m.notify("Choose a phone first", true)
		return false
	}
	if h.active != nil && h.active.DeviceID != m.deviceID {
		m.notify("Select this thread’s phone before replying", true)
		return false
	}
	if h.active == nil {
		t := domain.Thread{ID: domain.ThreadID(m.deviceID, "local-"+m.recipient.PhoneNumber), DeviceID: m.deviceID, Participants: []domain.Participant{domain.ParticipantFor(m.recipient.PhoneNumber)}, DisplayName: m.recipient.Name, LastTimestamp: time.Now()}
		h.active = &t
		m.drafts[t.ID] = m.drafts[m.recipient.PhoneNumber]
	}
	t := *h.active
	key := m.draftKey()
	draft := m.drafts[key]
	draft.Body = m.editor.Value()
	m.drafts[key] = draft
	id := localID()
	msg := domain.Message{ID: id, DeviceID: t.DeviceID, ThreadID: t.ID, Sender: "You", Body: draft.Body, Timestamp: time.Now(), Direction: domain.Outgoing, Status: domain.Sending, Participants: t.Participants}
	if h.retryID != "" {
		msg.ID = h.retryID
		h.retryID = ""
	}
	phone := ""
	if len(t.Participants) == 1 {
		phone = t.Participants[0].Number
	}
	m.pending = &pendingSend{msg: msg, draftKey: key, draft: draft, phone: phone}
	return true
}

// deliverPending sends now when a phone is reachable, and otherwise offers to
// queue the message.
func (m *Model) deliverPending() tea.Cmd {
	p := m.pending
	if p == nil {
		return nil
	}
	if !m.deviceOnline(p.msg.DeviceID) {
		m.modal = "offline-send"
		m.choice = 0
		m.choices = []string{"Queue for later", "Keep draft", "Cancel"}
		return nil
	}
	m.pending = nil
	m.sending = true
	m.editor.Focus(false)
	m.notify("Sending…", false)
	msg, key, draft := p.msg, p.draftKey, p.draft
	s := m.history.store
	repo := m.store
	return func() tea.Msg {
		if err := repo.SaveDraft(key, draft); err != nil {
			return outgoingMsg{message: msg, draftKey: key, draft: draft, err: err}
		}
		if _, err := s.MergeMessages([]domain.Message{msg}); err != nil {
			return outgoingMsg{message: msg, draftKey: key, draft: draft, err: err}
		}
		if err := s.MessageStatus(msg.ID, domain.Sending); err != nil {
			return outgoingMsg{message: msg, draftKey: key, draft: draft, err: err}
		}
		return outgoingMsg{message: msg, draftKey: key, draft: draft}
	}
}

// resolveOfflineSend handles the offline-send prompt.
func (m *Model) resolveOfflineSend(choice string) tea.Cmd {
	p := m.pending
	m.modal = ""
	if p == nil {
		return nil
	}
	switch choice {
	case "Queue for later":
		os, ok := m.outbox()
		if !ok {
			m.notify("Queue is unavailable", true)
			m.pending = nil
			return nil
		}
		item := queue.Item{ID: p.msg.ID, DeviceID: p.msg.DeviceID, ThreadID: p.msg.ThreadID, Recipient: p.phone, Body: p.msg.Body, State: queue.Queued, CreatedAt: time.Now()}
		msg := p.msg
		msg.Status = domain.Queued
		key, draft := p.draftKey, p.draft
		m.pending = nil
		return func() tea.Msg {
			if err := os.Enqueue(item); err != nil {
				return outboxChangedMsg{err: err}
			}
			if _, err := os.MergeMessages([]domain.Message{msg}); err != nil {
				return outboxChangedMsg{err: err}
			}
			return outboxChangedMsg{queued: true, draftKey: key, draft: draft}
		}
	case "Keep draft":
		m.pending = nil
		m.notify("Draft kept", false)
	default:
		m.pending = nil
	}
	return nil
}

// openSchedule offers the relative and custom schedule choices.
func (m *Model) openSchedule() tea.Cmd {
	if !m.prepareSend() {
		return nil
	}
	m.modal = "schedule"
	m.choice = 0
	m.choices = []string{"Send now", "In 30 minutes", "This evening", "Tomorrow morning", "Custom date/time…"}
	return nil
}

func (m *Model) resolveSchedule(choice string) tea.Cmd {
	p := m.pending
	m.modal = ""
	if p == nil {
		return nil
	}
	if choice == "Send now" {
		return m.deliverPending()
	}
	if choice == "Custom date/time…" {
		m.schedInput.SetValue(time.Now().Add(time.Hour).Format("2006-01-02 15:04"))
		m.schedInput.CursorEnd()
		m.schedInput.Focus()
		m.modal = "schedule-time"
		return nil
	}
	now := time.Now()
	var at time.Time
	switch choice {
	case "In 30 minutes":
		at = now.Add(30 * time.Minute)
	case "This evening":
		at = time.Date(now.Year(), now.Month(), now.Day(), 18, 0, 0, 0, now.Location())
	case "Tomorrow morning":
		at = time.Date(now.Year(), now.Month(), now.Day()+1, 8, 0, 0, 0, now.Location())
	}
	if !at.After(now) {
		at = now.Add(time.Hour)
	}
	return m.storeSchedule(p, at)
}

func (m *Model) storeSchedule(p *pendingSend, at time.Time) tea.Cmd {
	os, ok := m.outbox()
	if !ok {
		m.notify("Scheduling is unavailable", true)
		m.pending = nil
		return nil
	}
	item := scheduler.Item{ID: p.msg.ID, DeviceID: p.msg.DeviceID, ThreadID: p.msg.ThreadID, Recipient: p.phone, Body: p.msg.Body, SendAfter: at, State: scheduler.Scheduled, CreatedAt: time.Now()}
	msg := p.msg
	msg.Status = domain.Scheduled
	key, draft := p.draftKey, p.draft
	m.pending = nil
	return func() tea.Msg {
		if err := os.Schedule(item); err != nil {
			return outboxChangedMsg{err: err}
		}
		if _, err := os.MergeMessages([]domain.Message{msg}); err != nil {
			return outboxChangedMsg{err: err}
		}
		return outboxChangedMsg{queued: true, scheduled: true, draftKey: key, draft: draft, when: at}
	}
}

// scheduleTimeKey parses the custom date/time prompt.
func (m *Model) scheduleTimeKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		m.modal = ""
		return nil
	case "enter":
		at, err := time.ParseInLocation("2006-01-02 15:04", strings.TrimSpace(m.schedInput.Value()), time.Local)
		if err != nil {
			m.notify("Use YYYY-MM-DD HH:MM", true)
			return nil
		}
		if !at.After(time.Now()) {
			m.notify("Scheduled time must be in the future", true)
			return nil
		}
		p := m.pending
		m.modal = ""
		if p == nil {
			return nil
		}
		return m.storeSchedule(p, at)
	}
	var cmd tea.Cmd
	m.schedInput, cmd = m.schedInput.Update(k)
	return cmd
}

// outboxAction handles the palette's queue and schedule entries.
func (m *Model) outboxAction(name string) (tea.Cmd, bool) {
	switch name {
	case "Schedule message":
		return m.openSchedule(), true
	case "Open outgoing queue":
		m.openQueue()
		return nil, true
	case "Send queued messages":
		return m.processQueue(), true
	}
	return nil, false
}

// processQueue releases due scheduled messages and drains the queue. It is safe
// to call repeatedly: every attempt is guarded by an atomic claim.
func (m *Model) processQueue() tea.Cmd {
	os, ok := m.outbox()
	if !ok {
		return nil
	}
	backend := m.backend
	max := m.cfg.Queue.MaxAttempts
	ctx := m.ctx
	online := m.deviceOnline(m.deviceID)
	return func() tea.Msg {
		if online {
			// A reconnect should not have to wait out the offline delay.
			_ = os.ReleaseOfflineWaits()
		}
		rel := &scheduler.Releaser{Store: os, Queue: os, Clock: clock.System{}}
		if _, err := rel.Release(time.Now()); err != nil {
			return queueProcessedMsg{err: err}
		}
		proc := &queue.Processor{Store: os, Sender: messaging.New(backend), Clock: clock.System{}, MaxAttempts: max, After: messaging.MirrorStatus(os)}
		results, err := proc.ProcessOnce(ctx, 10)
		sent, failed := 0, 0
		for _, r := range results {
			if r.Err == nil {
				sent++
			} else {
				failed++
			}
		}
		return queueProcessedMsg{sent: sent, failed: failed, err: err}
	}
}

// refreshCounts keeps the status bar tallies current.
func (m *Model) refreshCounts() tea.Cmd {
	os, ok := m.outbox()
	if !ok {
		return nil
	}
	return func() tea.Msg {
		q, err := os.CountQueue(queue.Queued, queue.Sending, queue.Failed, queue.Paused)
		if err != nil {
			return countsMsg{}
		}
		s, _ := os.CountScheduled()
		return countsMsg{queued: q, scheduled: s}
	}
}

// openQueue lists the queued and scheduled messages in one place.
func (m *Model) openQueue() {
	os, ok := m.outbox()
	if !ok {
		m.notify("Queue is unavailable", true)
		return
	}
	m.modal = "queue"
	m.choice = 0
	m.outboxEntries = nil
	var choices []string
	if items, err := os.Queue(); err == nil {
		for _, it := range items {
			e := outboxEntry{kind: "queue", id: it.ID, recipient: it.Recipient, body: it.Body, state: string(it.State), when: it.NextAttemptAt}
			m.outboxEntries = append(m.outboxEntries, e)
			choices = append(choices, e.label(m))
		}
	}
	if items, err := os.Scheduled(); err == nil {
		for _, it := range items {
			e := outboxEntry{kind: "schedule", id: it.ID, recipient: it.Recipient, body: it.Body, state: string(it.State), when: it.SendAfter}
			m.outboxEntries = append(m.outboxEntries, e)
			choices = append(choices, e.label(m))
		}
	}
	if len(choices) == 0 {
		choices = []string{"Nothing queued"}
	}
	m.choices = choices
}

// outboxEntry is one row in the queue view.
type outboxEntry struct {
	kind      string
	id        string
	recipient string
	body      string
	state     string
	when      time.Time
}

func (e outboxEntry) label(m *Model) string {
	where := e.state
	if e.kind == "schedule" {
		where = "scheduled " + e.when.Local().Format("Jan 2 15:04")
	}
	return fmt.Sprintf("%s · %s · %s", m.nameFor(e.recipient), where, snippet(e.body))
}

func snippet(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	r := []rune(s)
	if len(r) > 32 {
		return string(r[:32]) + "…"
	}
	return s
}

// nameFor resolves a number to a contact name for display.
func (m *Model) nameFor(number string) string {
	key := contacts.MatchKey(number)
	for _, c := range m.allContacts() {
		if c.PhoneNumber == number || (key != "" && contacts.MatchKey(c.PhoneNumber) == key) {
			if c.Name != "" {
				return contacts.SafeLabel(c.Name)
			}
		}
	}
	return number
}

// queueSummary is the tally line under the queue list.
func (m *Model) queueSummary() string {
	q, s, failed := 0, 0, 0
	for _, e := range m.outboxEntries {
		if e.kind == "queue" {
			q++
			if e.state == string(queue.Failed) {
				failed++
			}
		} else {
			s++
		}
	}
	var parts []string
	if q > 0 {
		parts = append(parts, fmt.Sprintf("%d queued", q))
	}
	if s > 0 {
		parts = append(parts, fmt.Sprintf("%d scheduled", s))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed))
	}
	return strings.Join(parts, " · ")
}

// outboxSuffix is the compact queued/scheduled note for the status bar.
func (m *Model) outboxSuffix() string {
	var parts []string
	if m.queuedCount > 0 {
		parts = append(parts, fmt.Sprintf("%d queued", m.queuedCount))
	}
	if m.scheduledCount > 0 {
		parts = append(parts, fmt.Sprintf("%d scheduled", m.scheduledCount))
	}
	return strings.Join(parts, " · ")
}

// renderQueueItem shows one queued or scheduled message in full.
func (m *Model) renderQueueItem() string {
	e, ok := m.currentOutboxEntry()
	if !ok {
		return ""
	}
	var b strings.Builder
	b.WriteString(m.nameFor(e.recipient) + "\n")
	b.WriteString(e.recipient + "\n\n")
	b.WriteString(e.body + "\n\n")
	if e.kind == "schedule" {
		b.WriteString("Scheduled " + e.when.Local().Format("Jan 2, 2006 3:04 PM"))
	} else {
		b.WriteString("State: " + e.state)
	}
	return b.String()
}

// queueKey drives the queue view.
func (m *Model) queueKey(k tea.KeyMsg) tea.Cmd {
	n := len(m.outboxEntries)
	switch k.String() {
	case "up", "k", "ctrl+k":
		m.choice = max(0, m.choice-1)
	case "down", "j", "ctrl+j":
		m.choice = min(max(0, n-1), m.choice+1)
	case "esc", "q":
		m.modal = ""
	case "s":
		return m.sendOutboxEntry(true)
	case "e":
		return m.editOutboxEntry()
	case "d":
		return m.removeOutboxEntry()
	case "p":
		return m.pauseOutboxEntry()
	case "enter":
		m.modal = "queue-item"
	}
	return nil
}

func (m *Model) currentOutboxEntry() (outboxEntry, bool) {
	if m.choice < 0 || m.choice >= len(m.outboxEntries) {
		return outboxEntry{}, false
	}
	return m.outboxEntries[m.choice], true
}

func (m *Model) sendOutboxEntry(now bool) tea.Cmd {
	e, ok := m.currentOutboxEntry()
	if !ok {
		return nil
	}
	os, ok := m.outbox()
	if !ok {
		return nil
	}
	m.modal = ""
	id := e.id
	if now {
		return func() tea.Msg {
			if e.kind == "schedule" {
				item, found, err := os.ScheduledItem(id)
				if err != nil || !found {
					return outboxChangedMsg{}
				}
				item.SendAfter = time.Now().Add(-time.Second)
				item.State = scheduler.Scheduled
				if err := os.UpdateScheduled(item); err != nil {
					return outboxChangedMsg{err: err}
				}
				// outboxChangedMsg runs a processing pass, so "send now" takes
				// effect immediately instead of waiting for the next tick.
				return outboxChangedMsg{}
			}
			it, found, err := os.QueueItem(id)
			if err != nil || !found {
				return outboxChangedMsg{}
			}
			it.State = queue.Queued
			it.NextAttemptAt = time.Time{}
			if err := os.UpdateQueue(it); err != nil {
				return outboxChangedMsg{err: err}
			}
			return outboxChangedMsg{}
		}
	}
	// Not forcing a send: just close.
	return nil
}

func (m *Model) pauseOutboxEntry() tea.Cmd {
	e, ok := m.currentOutboxEntry()
	if !ok || e.kind != "queue" {
		return nil
	}
	os, ok := m.outbox()
	if !ok {
		return nil
	}
	id := e.id
	return func() tea.Msg {
		it, found, err := os.QueueItem(id)
		if err != nil || !found {
			return outboxChangedMsg{}
		}
		switch it.State {
		case queue.Paused:
			it.State = queue.Queued
			it.NextAttemptAt = time.Time{}
			it.LastError = ""
		case queue.Queued:
			it.State = queue.Paused
		}
		if err := os.UpdateQueue(it); err != nil {
			return outboxChangedMsg{err: err}
		}
		return outboxChangedMsg{}
	}
}

func (m *Model) removeOutboxEntry() tea.Cmd {
	e, ok := m.currentOutboxEntry()
	if !ok {
		return nil
	}
	os, ok := m.outbox()
	if !ok {
		return nil
	}
	m.modal = ""
	id, kind := e.id, e.kind
	return func() tea.Msg {
		var err error
		if kind == "schedule" {
			err = os.RemoveScheduled(id)
		} else {
			err = os.RemoveQueue(id)
		}
		if err != nil {
			return outboxChangedMsg{err: err}
		}
		_ = os.DeleteLocalMessage(id)
		return outboxChangedMsg{}
	}
}

// editOutboxEntry loads the message back into the composer and drops it from
// the outbox, so it can be changed before it is sent.
func (m *Model) editOutboxEntry() tea.Cmd {
	e, ok := m.currentOutboxEntry()
	if !ok {
		return nil
	}
	os, ok := m.outbox()
	if !ok {
		return nil
	}
	m.modal = ""
	id, kind := e.id, e.kind
	recipient, body := e.recipient, e.body
	return func() tea.Msg {
		var err error
		if kind == "schedule" {
			err = os.RemoveScheduled(id)
		} else {
			err = os.RemoveQueue(id)
		}
		if err != nil {
			return outboxChangedMsg{err: err}
		}
		_ = os.DeleteLocalMessage(id)
		return editOutboxMsg{recipient: recipient, body: body}
	}
}

// editOutboxMsg asks the update loop to place a message back in the composer.
type editOutboxMsg struct{ recipient, body string }

// prepareEdit loads a discarded outbox message into the composer for editing.
func (m *Model) prepareEdit(v editOutboxMsg) tea.Cmd {
	c := contacts.Contact{Name: m.nameFor(v.recipient), PhoneNumber: v.recipient}
	for _, known := range m.allContacts() {
		if known.PhoneNumber == v.recipient {
			c = known
			break
		}
	}
	m.choose(c)
	m.editor.SetValue(v.body)
	m.trackChange()
	m.modal = ""
	m.setPane(paneComposer)
	return m.loadCache()
}
