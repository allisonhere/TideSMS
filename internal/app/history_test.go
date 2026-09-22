package app

import (
	"context"
	"errors"
	"github.com/allisonhere/tidesms/internal/backend/fake"
	"github.com/allisonhere/tidesms/internal/config"
	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tidesms/internal/storage"
	"github.com/allisonhere/tidesms/internal/themes"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"io"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	device    = "phone"
	amyNumber = "+15551234567"
	momNumber = "+15559876543"
	unknown   = "+18165550182"
)

var (
	amyThread     = domain.ThreadID(device, "1")
	familyThread  = domain.ThreadID(device, "2")
	unknownThread = domain.ThreadID(device, "3")
	base          = time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
)

// driver applies commands the way Bubble Tea would. Commands that block, such as
// the event-stream wait and the draft debounce, stay pending across calls instead
// of being discarded, so no synchronization update is ever silently swallowed.
type driver struct {
	t       *testing.T
	m       *Model
	queue   []tea.Cmd
	pending []chan tea.Msg
}

func (d *driver) step(grace time.Duration) bool {
	for len(d.queue) > 0 {
		cmd := d.queue[0]
		d.queue = d.queue[1:]
		if cmd == nil {
			continue
		}
		out := make(chan tea.Msg, 1)
		go func() { out <- cmd() }()
		d.pending = append(d.pending, out)
	}
	if grace > 0 {
		time.Sleep(grace)
	}
	applied := false
	for i := 0; i < len(d.pending); i++ {
		var msg tea.Msg
		select {
		case msg = <-d.pending[i]:
		default:
			continue
		}
		d.pending = append(d.pending[:i], d.pending[i+1:]...)
		i--
		applied = true
		if batch, ok := msg.(tea.BatchMsg); ok {
			d.queue = append(d.queue, batch...)
			continue
		}
		if msg == nil {
			continue
		}
		_, next := d.m.Update(msg)
		d.queue = append(d.queue, next)
	}
	return applied
}
func (d *driver) run(cmds ...tea.Cmd) {
	d.t.Helper()
	d.queue = append(d.queue, cmds...)
	// Storage and backend work runs on real goroutines, so allow a short idle
	// window before concluding that nothing further is arriving.
	for idle := 0; idle < 6; idle++ {
		if d.step(5 * time.Millisecond) {
			idle = -1
		}
	}
}
func (d *driver) settle(what string, ok func() bool) {
	d.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			d.t.Fatalf("timed out waiting for %s (status %q)", what, d.m.history.status)
		}
		d.step(20 * time.Millisecond)
	}
	d.run()
}
func (d *driver) press(s string) {
	d.t.Helper()
	k := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	switch s {
	case "enter":
		k = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		k = tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		k = tea.KeyMsg{Type: tea.KeyTab}
	}
	_, cmd := d.m.Update(k)
	d.run(cmd)
}

type notice struct {
	mu    sync.Mutex
	calls [][2]string
}

func (n *notice) show(_ context.Context, title, body string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls = append(n.calls, [2]string{title, body})
	return nil
}
func (n *notice) seen() [][2]string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([][2]string{}, n.calls...)
}

func incoming(thread, sender, body string, minute int) domain.Message {
	m := domain.Message{DeviceID: device, ThreadID: thread, Sender: sender, Body: body,
		Timestamp: base.Add(time.Duration(minute) * time.Minute), Direction: domain.Incoming,
		Status: domain.Unknown, Unread: true, Participants: []domain.Participant{domain.ParticipantFor(sender)}}
	m.BackendID = m.StableID()[:8]
	m.ID = m.StableID()
	return m
}
func outgoing(thread, body string, minute int) domain.Message {
	m := domain.Message{DeviceID: device, ThreadID: thread, Sender: "You", Body: body,
		Timestamp: base.Add(time.Duration(minute) * time.Minute), Direction: domain.Outgoing,
		Status: domain.Sent, Participants: []domain.Participant{domain.ParticipantFor(amyNumber)}}
	m.BackendID = m.StableID()[:8]
	m.ID = m.StableID()
	return m
}

// seed fills a fake phone with one contact thread, one group, and one thread from
// a number that is not in the address book.
func seed(b *fake.Backend) {
	amy := []domain.Message{incoming(amyThread, amyNumber, "Are we still meeting around seven?", 41),
		outgoing(amyThread, "Yep, I'll be there.", 42),
		incoming(amyThread, amyNumber, "Bring dessert 😄", 58)}
	family := []domain.Message{incoming(familyThread, momNumber, "Call me when you can.", 12)}
	family[0].IsGroup = true
	family[0].Participants = []domain.Participant{domain.ParticipantFor(amyNumber), domain.ParticipantFor(momNumber)}
	stranger := []domain.Message{incoming(unknownThread, unknown, "Your package has shipped.", 5)}
	b.History[amyThread] = amy
	b.History[familyThread] = family
	b.History[unknownThread] = stranger
	b.ThreadList = []domain.Thread{
		{ID: amyThread, DeviceID: device, BackendID: "1", LastMessage: amy[2].Body, LastTimestamp: amy[2].Timestamp,
			LastBackendID: amy[2].BackendID, Participants: []domain.Participant{domain.ParticipantFor(amyNumber)}},
		{ID: familyThread, DeviceID: device, BackendID: "2", DisplayName: "Family", IsGroup: true,
			LastMessage: family[0].Body, LastTimestamp: family[0].Timestamp, LastBackendID: family[0].BackendID,
			Participants: family[0].Participants},
		{ID: unknownThread, DeviceID: device, BackendID: "3", LastMessage: stranger[0].Body,
			LastTimestamp: stranger[0].Timestamp, LastBackendID: stranger[0].BackendID,
			Participants: []domain.Participant{domain.ParticipantFor(unknown)}},
	}
}

func conversationFixture(t *testing.T) (*Model, *fake.Backend, *storage.Store, *driver, *notice) {
	t.Helper()
	dir := t.TempDir()
	s, err := storage.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	b := fake.New()
	seed(b)
	m, n := attach(t, s, b, dir)
	return m, b, s, &driver{t: t, m: m}, n
}

// attach builds a model over an existing store, as a restart would.
func attach(t *testing.T, s *storage.Store, b *fake.Backend, dir string) (*Model, *notice) {
	t.Helper()
	n := &notice{}
	m := New(context.Background(), s, b, config.Default(), filepath.Join(dir, "config.toml"),
		slog.New(slog.NewJSONHandler(io.Discard, nil)), nil)
	m.notifier = n.show
	m.loaded = true
	m.contacts = []contacts.Contact{{ID: "amy", Name: "Amy", PhoneNumber: amyNumber, Theme: "rose"}}
	// Thread display names are resolved in SQL, so the address book must be stored.
	for _, c := range m.contacts {
		if err := s.SaveContact(c); err != nil {
			t.Fatal(err)
		}
	}
	drafts, err := s.Drafts()
	if err != nil {
		t.Fatal(err)
	}
	m.drafts = drafts
	m.devices, _ = b.Devices(context.Background())
	m.deviceID = device
	if !m.history.enabled {
		t.Fatal("conversation history did not activate for a conversation backend")
	}
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	return m, n
}

func syncPhone(t *testing.T, d *driver) {
	t.Helper()
	d.run(d.m.startSession(true))
	d.settle("initial sync", func() bool { return strings.HasPrefix(d.m.history.status, "Synced") })
}
func loadCached(t *testing.T, d *driver) {
	t.Helper()
	d.run(d.m.loadCache())
	d.settle("cached threads", func() bool { return len(d.m.history.threads) > 0 })
}
func openThreadByID(t *testing.T, d *driver, id string) {
	t.Helper()
	d.m.setPane(paneThreads)
	d.m.history.threadSelected = id
	d.press("enter")
	if d.m.history.active == nil || d.m.history.active.ID != id {
		t.Fatalf("thread %s did not open", id)
	}
}

// selectSetting moves the static settings panel cursor to the row with the
// given label, failing if the panel does not offer it.
func selectSetting(t *testing.T, m *Model, label string) {
	t.Helper()
	for i, f := range m.settingsFields() {
		if f.label == label {
			m.choice = i
			return
		}
	}
	t.Fatalf("setting %q not offered", label)
}

// pickChoice moves a modal's cursor to the entry with the given label.
func pickChoice(t *testing.T, m *Model, want string) {
	t.Helper()
	for i, c := range m.choices {
		if c == want {
			m.choice = i
			return
		}
	}
	t.Fatalf("choice %q not offered in %v", want, m.choices)
}

func thread(t *testing.T, m *Model, id string) domain.Thread {
	t.Helper()
	for _, v := range m.history.threads {
		if v.ID == id {
			return v
		}
	}
	t.Fatalf("thread %s missing from %d cached threads", id, len(m.history.threads))
	return domain.Thread{}
}

// Startup renders from SQLite alone: no phone call is made before the first frame,
// and unread state survives the restart rather than being cleared by it.
func TestCachedConversationsRenderBeforeThePhoneAnswers(t *testing.T) {
	m, b, s, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	d.settle("read receipt", func() bool { return thread(t, m, amyThread).UnreadCount == 0 })
	if err := m.FlushDrafts(); err != nil {
		t.Fatal(err)
	}

	cold := fake.New()
	cold.Connection(false)
	restarted, _ := attach(t, s, cold, t.TempDir())
	cache := &driver{t: t, m: restarted}
	loadCached(t, cache)
	if len(restarted.history.threads) != 3 {
		t.Fatalf("cached threads not restored: %+v", restarted.history.threads)
	}
	if cold.ThreadCalls != 0 || len(cold.Requests) != 0 {
		t.Fatal("the first frame waited on the phone")
	}
	if restarted.history.status != "Cached" {
		t.Fatalf("status %q", restarted.history.status)
	}
	if thread(t, restarted, familyThread).UnreadCount != 1 {
		t.Fatal("startup marked an unopened thread read")
	}
	if thread(t, restarted, amyThread).UnreadCount != 0 {
		t.Fatal("a thread read before the restart came back unread")
	}
	view := ansi.Strip(restarted.View())
	for _, want := range []string{"Amy", "Family", unknown, "Bring dessert"} {
		if !strings.Contains(view, want) {
			t.Fatalf("cached view missing %q:\n%s", want, view)
		}
	}
	_ = b
}

// Synchronization fills the thread list, resolves contacts, keeps unknown numbers
// as first-class threads, and separates incoming from outgoing messages.
func TestSyncBuildsThreadsAndRendersBothDirections(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	if len(m.history.threads) != 3 {
		t.Fatalf("threads: %+v", m.history.threads)
	}
	if got := m.history.threads[0].ID; got != amyThread {
		t.Fatalf("threads not ordered by activity: %s first", got)
	}
	if name := thread(t, m, amyThread).DisplayName; name != "Amy" {
		t.Fatalf("contact not resolved: %q", name)
	}
	if name := thread(t, m, unknownThread).DisplayName; name != unknown {
		t.Fatalf("unknown sender lost its number: %q", name)
	}
	if !thread(t, m, familyThread).IsGroup {
		t.Fatal("group flag lost")
	}
	if n := thread(t, m, amyThread).UnreadCount; n != 2 {
		t.Fatalf("unread count %d", n)
	}

	openThreadByID(t, d, amyThread)
	d.settle("history", func() bool { return len(m.history.view.Messages) == 3 })
	ms := m.history.view.Messages
	if ms[0].Direction != domain.Incoming || ms[1].Direction != domain.Outgoing {
		t.Fatalf("directions: %+v", ms)
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"Replying to Amy", "Are we still meeting", "You", "Yep, I'll be there."} {
		if !strings.Contains(view, want) {
			t.Fatalf("conversation missing %q:\n%s", want, view)
		}
	}
	d.settle("read receipt", func() bool { return thread(t, m, amyThread).UnreadCount == 0 })

	// Group threads are readable, and sending is refused with an explanation
	// rather than silently dropped.
	openThreadByID(t, d, familyThread)
	d.settle("group history", func() bool { return len(m.history.view.Messages) == 1 })
	m.setPane(paneComposer)
	typeText(m, "hello all")
	if cmd := m.send(); cmd != nil {
		t.Fatal("group send was attempted")
	}
	if !m.failed || !strings.Contains(m.notice, "not supported") {
		t.Fatalf("no explanation for group sending: %q", m.notice)
	}
	if header := ansi.Strip(m.View()); !strings.Contains(header, "Family") || !strings.Contains(header, "2 people") {
		t.Fatalf("group header:\n%s", header)
	}
}

// A live message reaches the open thread without a notification; one for another
// thread raises unread state and notifies.
func TestIncomingMessagesUpdateThreadsAndNotifySelectively(t *testing.T) {
	m, b, _, d, n := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	d.settle("history", func() bool { return len(m.history.view.Messages) == 3 })

	live := incoming(amyThread, amyNumber, "Running five minutes late", 59)
	live.Timestamp = time.Now()
	b.Emit(domain.Event{Kind: domain.EventMessage, Message: &live})
	d.settle("live message", func() bool { return len(m.history.view.Messages) == 4 })
	if body := m.history.view.Messages[3].Body; body != live.Body {
		t.Fatalf("open thread not updated: %q", body)
	}
	if thread(t, m, amyThread).UnreadCount != 0 {
		t.Fatal("visible thread kept an unread badge")
	}
	if calls := n.seen(); len(calls) != 0 {
		t.Fatalf("notified for the visible thread: %v", calls)
	}

	other := incoming(unknownThread, unknown, "Out for delivery", 60)
	other.Timestamp = time.Now()
	b.Emit(domain.Event{Kind: domain.EventMessage, Message: &other})
	d.settle("unread badge", func() bool { return thread(t, m, unknownThread).UnreadCount == 2 })
	calls := n.seen()
	if len(calls) != 1 || calls[0][0] != unknown || calls[0][1] != other.Body {
		t.Fatalf("notification: %v", calls)
	}
	if preview := thread(t, m, unknownThread).LastMessage; preview != other.Body {
		t.Fatalf("thread preview stale: %q", preview)
	}
	if m.history.active.ID != amyThread {
		t.Fatal("a background message stole the open thread")
	}

	// The privacy switch hides the body but still announces the message.
	m.cfg.Notifications.ShowBody = false
	third := incoming(unknownThread, unknown, "Delivered to the porch", 61)
	third.Timestamp = time.Now()
	b.Emit(domain.Event{Kind: domain.EventMessage, Message: &third})
	d.settle("second notification", func() bool { return len(n.seen()) == 2 })
	if body := n.seen()[1][1]; body != "New SMS" || strings.Contains(body, "porch") {
		t.Fatalf("body preview leaked: %q", body)
	}

	// A muted thread stays quiet even for a background message.
	if err := m.store.SetNotificationMode(storage.ScopeThread, unknownThread, notifMuted); err != nil {
		t.Fatal(err)
	}
	fourth := incoming(unknownThread, unknown, "Another parcel", 62)
	fourth.Timestamp = time.Now()
	b.Emit(domain.Event{Kind: domain.EventMessage, Message: &fourth})
	d.settle("muted message", func() bool { return thread(t, m, unknownThread).LastMessage == fourth.Body })
	if len(n.seen()) != 2 {
		t.Fatalf("muted thread notified: %v", n.seen())
	}
}

// Quote inserts the message as text composition, and delete removes only the
// local cached copy.
func TestQuoteAndDeleteLocalCopy(t *testing.T) {
	m, _, st, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	d.settle("history", func() bool { return len(m.history.view.Messages) == 3 })

	find := func(body string) int {
		for i, msg := range m.history.view.Messages {
			if msg.Body == body {
				return i
			}
		}
		t.Fatalf("message %q not loaded", body)
		return -1
	}
	const target = "Bring dessert 😄"
	m.history.view.Selected = find(target)

	d.press("enter")
	pickChoice(t, m, "Quote")
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	if got := m.editor.Value(); !strings.Contains(got, "> "+target) {
		t.Fatalf("quote not inserted: %q", got)
	}

	// Quoting leaves the composer focused; go back to the conversation.
	m.setPane(paneConversation)
	m.history.view.Selected = find(target)
	d.press("enter")
	pickChoice(t, m, "Delete local copy")
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	if m.modal != "delete-message" {
		t.Fatalf("confirm modal = %q", m.modal)
	}
	pickChoice(t, m, "Delete")
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))

	for _, msg := range m.history.view.Messages {
		if msg.Body == target {
			t.Fatal("deleted message still in the view")
		}
	}
	remaining, err := st.Messages(amyThread, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range remaining {
		if msg.Body == target {
			t.Fatal("deleted message still in the cache")
		}
	}
}

// The contact inspector summarises a person and reuses the existing actions;
// muting from it is stored per contact.
func TestContactDetailsMuteToggle(t *testing.T) {
	m, _, st, d, _ := conversationFixture(t)
	syncPhone(t, d)
	if _, ok := m.contactForDetails(); !ok {
		t.Fatal("no contact to inspect")
	}
	m.editing = m.contacts[0]
	m.openContactDetails()
	if m.modal != "contact" {
		t.Fatalf("modal = %q", m.modal)
	}
	pickChoice(t, m, "Mute notifications")
	if cmd := m.contactDetailsKey(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		cmd()
	}
	mode, ok, _ := st.NotificationMode(storage.ScopeContact, m.contacts[0].PhoneNumber)
	if !ok || mode != notifMuted {
		t.Fatalf("mute not stored: mode=%q ok=%v", mode, ok)
	}
	m.openContactDetails()
	pickChoice(t, m, "Unmute notifications")
	if cmd := m.contactDetailsKey(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		cmd()
	}
	if _, ok, _ := st.NotificationMode(storage.ScopeContact, m.contacts[0].PhoneNumber); ok {
		t.Fatal("unmute did not clear the override")
	}
}

func TestComposerEstimate(t *testing.T) {
	m, _, _ := fixture(t)
	m.choose(m.contacts[0])
	typeText(m, strings.Repeat("a", 161))
	if est := m.composerEstimate(); !strings.Contains(est, "161 chars") || !strings.Contains(est, "2 SMS") {
		t.Fatalf("estimate = %q", est)
	}
	m.editor.SetValue("")
	if est := m.composerEstimate(); est != "" {
		t.Fatalf("empty estimate = %q", est)
	}
}

// Outgoing messages appear immediately as sending, survive failure with their text
// intact, and retry in place instead of duplicating.
func TestSendShowsProgressFailsCleanlyAndRetriesInPlace(t *testing.T) {
	m, b, s, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	d.settle("history", func() bool { return len(m.history.view.Messages) == 3 })

	b.SendError = errors.New("phone disconnected")
	m.setPane(paneComposer)
	typeText(m, "On my way.")
	cmd := m.send()
	if cmd == nil {
		t.Fatal("no send command")
	}
	first := cmd()
	pending, err := s.Messages(amyThread, 100)
	if err != nil {
		t.Fatal(err)
	}
	if last := pending[len(pending)-1]; last.Status != domain.Sending || last.Direction != domain.Outgoing {
		t.Fatalf("message was not shown as sending: %+v", last)
	}
	d.run(func() tea.Msg { return first })
	d.settle("failure", func() bool { return !m.sending })

	ms, err := s.Messages(amyThread, 100)
	if err != nil || len(ms) != 4 || ms[3].Status != domain.Failed {
		t.Fatalf("%+v %v", ms, err)
	}
	if m.editor.Value() != "On my way." {
		t.Fatalf("failed send cleared the composer: %q", m.editor.Value())
	}
	if !m.failed {
		t.Fatal("failure was not reported")
	}

	b.SendError = nil
	m.setPane(paneConversation)
	d.press("G")
	if cur := m.history.view.Current(); cur == nil || cur.Status != domain.Failed {
		t.Fatalf("failed message not selectable: %+v", cur)
	}
	d.press("r")
	if m.history.retryID != ms[3].ID || m.editor.Value() != "On my way." {
		t.Fatalf("retry did not prepare the message: %q %q", m.history.retryID, m.editor.Value())
	}
	d.run(m.send())
	d.settle("retry", func() bool { return !m.sending })
	ms, err = s.Messages(amyThread, 100)
	if err != nil || len(ms) != 4 {
		t.Fatalf("retry duplicated the message: %+v %v", ms, err)
	}
	if ms[3].Status != domain.Submitted {
		t.Fatalf("retry status %q", ms[3].Status)
	}
	if len(b.Sent) != 2 || b.Sent[1].PhoneNumber != amyNumber {
		t.Fatalf("sends: %+v", b.Sent)
	}
	if m.editor.Value() != "" {
		t.Fatal("accepted submission did not clear the composer")
	}
}

// Each thread keeps its own unfinished text, across switches and across a restart.
func TestDraftsAreThreadScopedAndSurviveRestart(t *testing.T) {
	m, b, s, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	m.setPane(paneComposer)
	typeText(m, "I'll be there around")
	openThreadByID(t, d, familyThread)
	m.setPane(paneComposer)
	typeText(m, "Can you send me the")
	openThreadByID(t, d, amyThread)
	if got := m.editor.Value(); got != "I'll be there around" {
		t.Fatalf("switching threads destroyed a draft: %q", got)
	}
	if err := m.FlushDrafts(); err != nil {
		t.Fatal(err)
	}

	restarted, _ := attach(t, s, b, t.TempDir())
	next := &driver{t: t, m: restarted}
	loadCached(t, next)
	openThreadByID(t, next, familyThread)
	if got := restarted.editor.Value(); got != "Can you send me the" {
		t.Fatalf("draft lost across restart: %q", got)
	}
	openThreadByID(t, next, amyThread)
	if got := restarted.editor.Value(); got != "I'll be there around" {
		t.Fatalf("draft lost across restart: %q", got)
	}
}

// Search reads the local cache, highlights matches, and restores the full thread.
func TestSearchWithinThreadUsesTheCache(t *testing.T) {
	m, b, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	d.settle("history", func() bool { return len(m.history.view.Messages) == 3 })
	requests := len(b.Requests)

	m.setPane(paneConversation)
	d.press("/")
	if !m.history.search {
		t.Fatal("search did not open")
	}
	d.press("dessert")
	d.settle("matches", func() bool { return len(m.history.searchResults) == 1 })
	if body := m.history.searchResults[0].Body; !strings.Contains(body, "dessert") {
		t.Fatalf("match %q", body)
	}
	if len(b.Requests) != requests {
		t.Fatal("search queried the phone instead of SQLite")
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "1 matches") {
		t.Fatalf("search state not shown:\n%s", view)
	}
	d.press("enter")
	d.press("n")
	if m.history.searchIndex != 0 {
		t.Fatalf("single match should stay put: %d", m.history.searchIndex)
	}
	d.press("esc")
	d.settle("restored history", func() bool { return len(m.history.view.Messages) == 3 })
	if m.history.searchQuery != "" || m.history.searchResults != nil {
		t.Fatal("search state outlived Esc")
	}
}

// Thread themes beat contact themes, groups get a stable identity accent, and both
// survive a restart.
func TestThreadThemeOverridesContactAndGroupAccentsAreStable(t *testing.T) {
	m, b, s, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	if name := m.conversationRenderer().Styles.Theme.Name; name != "rose" {
		t.Fatalf("contact accent not applied: %q", name)
	}
	d.run(m.action("Change thread theme"))
	if m.modal != "thread-themes" {
		t.Fatalf("modal %q", m.modal)
	}
	for i, c := range m.choices {
		if c == "nord" {
			m.choice = i
		}
	}
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	if name := m.conversationRenderer().Styles.Theme.Name; name != "nord" {
		t.Fatalf("thread theme did not win immediately: %q", name)
	}

	openThreadByID(t, d, familyThread)
	group := m.conversationRenderer().Styles.Theme.Name
	if want := themes.Resolve(m.cfg.General.Theme, "", familyThread).Name; group != want {
		t.Fatalf("group accent %q is not derived from the thread id (%q)", group, want)
	}
	if err := m.FlushDrafts(); err != nil {
		t.Fatal(err)
	}

	restarted, _ := attach(t, s, b, t.TempDir())
	next := &driver{t: t, m: restarted}
	loadCached(t, next)
	openThreadByID(t, next, amyThread)
	if name := restarted.conversationRenderer().Styles.Theme.Name; name != "nord" {
		t.Fatalf("thread theme lost across restart: %q", name)
	}
	openThreadByID(t, next, familyThread)
	if name := restarted.conversationRenderer().Styles.Theme.Name; name != group {
		t.Fatalf("group accent regenerated: %q then %q", group, name)
	}
}

// Losing the phone keeps cached history browsable; regaining it resubscribes and
// collects what was missed, with no restart.
func TestOfflineBrowsingThenReconnectCatchesUp(t *testing.T) {
	m, b, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	d.settle("history", func() bool { return len(m.history.view.Messages) == 3 })
	generation := m.history.generation

	b.Connection(false)
	d.settle("offline status", func() bool { return strings.Contains(m.history.status, "cached") })
	d.run(m.discover()) // The periodic device refresh notices the phone has gone.
	if len(m.history.threads) != 3 || len(m.history.view.Messages) != 3 {
		t.Fatal("cached conversation became unusable offline")
	}
	m.setPane(paneComposer)
	typeText(m, "drafted while offline")
	if m.drafts[amyThread].Body != "drafted while offline" {
		t.Fatal("composing offline failed")
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "Offline") {
		t.Fatalf("offline state not shown:\n%s", view)
	}

	missed := incoming(amyThread, amyNumber, "Missed you while you were away", 70)
	b.History[amyThread] = append(b.History[amyThread], missed)
	b.ThreadList[0].LastBackendID = missed.BackendID
	b.ThreadList[0].LastTimestamp = missed.Timestamp
	b.ThreadList[0].LastMessage = missed.Body
	b.Connection(true)
	d.settle("catch-up", func() bool { return len(m.history.view.Messages) == 4 })
	d.run(m.discover())
	if view := ansi.Strip(m.View()); strings.Contains(view, "Offline") {
		t.Fatalf("still shown offline after reconnecting:\n%s", view)
	}
	if body := m.history.view.Messages[3].Body; body != missed.Body {
		t.Fatalf("missed message not collected: %q", body)
	}
	if m.history.generation != generation {
		t.Fatal("reconnecting required a new session rather than restoring one")
	}
	if !strings.HasPrefix(m.history.status, "Synced") {
		t.Fatalf("status after reconnect: %q", m.history.status)
	}
	if m.drafts[amyThread].Body != "drafted while offline" {
		t.Fatal("reconnect discarded the offline draft")
	}
}

// Every adaptive width stays inside the terminal and keeps the user's place.
func TestAdaptiveLayoutPreservesSelectionWithinBounds(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	d.settle("history", func() bool { return len(m.history.view.Messages) == 3 })
	m.setPane(paneConversation)
	d.press("g")
	selected := m.history.view.Selected
	for _, size := range [][2]int{{140, 40}, {110, 30}, {90, 24}, {60, 18}, {40, 12}, {1, 1}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for _, modal := range []string{"", "message", "thread-themes", "compose", "help"} {
			m.modal = modal
			switch modal {
			case "message", "thread-themes":
				m.choices = []string{"Copy", "Reply", "Close"}
			case "compose":
				m.refreshCompose()
			}
			view := m.View()
			if view == "" {
				continue
			}
			lines := strings.Split(view, "\n")
			if len(lines) > size[1] {
				t.Fatalf("%v %s height %d", size, modal, len(lines))
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > size[0] {
					t.Fatalf("%v %s width %d", size, modal, ansi.StringWidth(line))
				}
			}
		}
		m.modal = ""
		if m.history.active.ID != amyThread || m.history.threadSelected != amyThread {
			t.Fatalf("%v lost the open thread", size)
		}
		if m.history.view.Selected != selected {
			t.Fatalf("%v moved the message selection to %d", size, m.history.view.Selected)
		}
	}
}

// The inspector reports what the backend actually provides, and marking a thread
// unread is a deliberate action rather than a side effect.
func TestMessageInspectorAndManualUnread(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	d.settle("read receipt", func() bool { return thread(t, m, amyThread).UnreadCount == 0 })
	m.setPane(paneConversation)
	d.press("G")
	d.press("enter")
	if m.modal != "message" {
		t.Fatalf("inspector did not open: %q", m.modal)
	}
	details := ansi.Strip(m.View())
	for _, want := range []string{"Message details", "Amy", amyNumber, "incoming", "Backend ID"} {
		if !strings.Contains(details, want) {
			t.Fatalf("inspector missing %q:\n%s", want, details)
		}
	}
	d.press("esc")

	d.run(m.action("Mark thread unread"))
	d.settle("manual unread", func() bool { return thread(t, m, amyThread).UnreadCount == 1 })
	d.run(m.action("Jump to newest"))
	d.settle("cleared again", func() bool { return thread(t, m, amyThread).UnreadCount == 0 })
}

// Editing the contact behind an open thread must not close the conversation or
// throw away what the user was typing.
func TestContactEditKeepsTheOpenThreadAndDraft(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	m.setPane(paneComposer)
	typeText(m, "still typing")
	d.run(m.action("Change contact theme"))
	if m.modal != "themes" {
		t.Fatalf("theme chooser did not open: %q", m.modal)
	}
	for i, c := range m.choices {
		if c == "dracula" {
			m.choice = i
		}
	}
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	d.settle("saved contact", func() bool { return !m.busy })
	if m.history.active == nil || m.history.active.ID != amyThread {
		t.Fatal("editing the contact closed the thread")
	}
	if m.editor.Value() != "still typing" {
		t.Fatalf("editing the contact discarded the draft: %q", m.editor.Value())
	}
	if name := m.conversationRenderer().Styles.Theme.Name; name != "dracula" {
		t.Fatalf("new contact accent not applied: %q", name)
	}
}

// Leaving a compose for someone with no thread yet has no conversation to
// return to, so it falls back to the thread list instead of an empty pane.
func TestEscapeFromNewComposeFallsBackToThreads(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	d.m.setPane(paneThreads)
	none := contacts.Contact{ID: "new", Name: "New Person", PhoneNumber: "+15550001111"}
	m.choose(none)
	if !m.focus || m.history.active != nil {
		t.Fatalf("expected a focused composer with no thread: focus=%v active=%v", m.focus, m.history.active)
	}
	d.press("esc")
	if m.history.pane != paneThreads {
		t.Fatalf("esc from a new compose landed on pane %d, want the thread list", m.history.pane)
	}
}

// The settings panel is one static list: a theme preview neither saves nor
// closes on Esc, Enter commits, and a boolean toggle leaves the panel open.
func TestSettingsPanelIsStaticAndNonDestructive(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	d.run(m.action("Open settings"))
	if m.modal != "settings" {
		t.Fatalf("settings did not open: %q", m.modal)
	}
	if f, ok := m.selectedSetting(); !ok || f.id != settingTheme {
		t.Fatalf("first row should be Theme, got %+v", f)
	}
	before := m.cfg.General.Theme
	m.settingsAdjust(1)
	if m.cfg.General.Theme != before {
		t.Fatal("cycling previewed by saving")
	}
	d.press("esc")
	if m.modal != "" || m.cfg.General.Theme != before {
		t.Fatalf("Esc did not discard the preview: modal=%q theme=%q", m.modal, m.cfg.General.Theme)
	}

	d.run(m.action("Open settings"))
	m.settingsAdjust(1)
	want := themes.Names[m.themeCursor]
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	d.settle("theme saved", func() bool { return !m.busy && m.cfg.General.Theme == want })
	if m.modal != "settings" {
		t.Fatal("committing a theme closed the panel")
	}

	selectSetting(t, m, "Message bubbles")
	was := m.cfg.Conversation.Bubbles
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	d.settle("bubbles saved", func() bool { return !m.busy && m.cfg.Conversation.Bubbles != was })
	if m.modal != "settings" {
		t.Fatal("toggling a setting closed the panel")
	}
}

// Bubble themes resolve global -> contact -> thread, preview from the settings
// panel, and persist.
func TestBubbleThemesFromSettingsAndPalette(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	d.settle("history", func() bool { return len(m.history.view.Messages) == 3 })

	// The global incoming bubble theme is set from the static panel.
	d.run(m.action("Open settings"))
	selectSetting(t, m, "Incoming bubbles")
	for i := 0; i < len(contactThemeNames()) && m.settingsValue(settingBubbleIn, true) != "dracula"; i++ {
		m.settingsAdjust(1)
	}
	if pal := m.bubblePalette(m.conversationTheme(), false); pal.Name != "dracula" {
		t.Fatalf("preview palette = %q", pal.Name)
	}
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	d.settle("global bubble saved", func() bool { return !m.busy && m.cfg.Conversation.IncomingTheme == "dracula" })
	saved, err := config.Load(m.configPath)
	if err != nil || saved.Conversation.IncomingTheme != "dracula" {
		t.Fatalf("not persisted: %+v err=%v", saved.Conversation, err)
	}
	d.press("esc")

	// A contact override through the palette.
	d.run(m.action("Change incoming bubble theme"))
	if m.modal != "bubble-themes" {
		t.Fatalf("picker modal = %q", m.modal)
	}
	pickChoice(t, m, "nord")
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	d.settle("contact bubble saved", func() bool { return !m.busy && m.recipient.ThemeIn == "nord" })
	if pal := m.bubblePalette(m.conversationTheme(), false); pal.Name != "nord" {
		t.Fatalf("contact override not used: %q", pal.Name)
	}

	// A thread override wins over the contact.
	d.run(m.action("Change thread incoming bubble theme"))
	if m.modal != "bubble-themes" {
		t.Fatalf("thread picker modal = %q", m.modal)
	}
	pickChoice(t, m, "gruvbox-light")
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	d.settle("thread bubble saved", func() bool { return m.history.active != nil && m.history.active.ThemeIn == "gruvbox-light" })
	if pal := m.bubblePalette(m.conversationTheme(), false); pal.Name != "gruvbox-light" {
		t.Fatalf("thread override not used: %q", pal.Name)
	}
	// The outgoing direction is untouched.
	if pal := m.bubblePalette(m.conversationTheme(), true); pal.Name != "" {
		t.Fatalf("outgoing direction changed: %q", pal.Name)
	}
}

// Settings offers the open contact's theme directly, so the accent can be
// changed without leaving the settings menu for the command palette.
func TestContactThemeReachableFromSettings(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	d.run(m.action("Open settings"))
	if m.modal != "settings" {
		t.Fatalf("settings did not open: %q", m.modal)
	}
	selectSetting(t, m, "Contact theme")
	// Cycle the inline value until it lands on nord, then commit.
	for i := 0; i < len(contactThemeNames()) && m.settingsValue(settingContactTheme, true) != "nord"; i++ {
		m.settingsAdjust(1)
	}
	if got := m.settingsValue(settingContactTheme, true); got != "nord" {
		t.Fatalf("could not cycle to nord: %q", got)
	}
	if name := m.conversationRenderer().Styles.Theme.Name; name != "nord" {
		t.Fatalf("contact theme did not preview: %q", name)
	}
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	d.settle("saved contact", func() bool { return !m.busy })
	if name := m.conversationRenderer().Styles.Theme.Name; name != "nord" {
		t.Fatalf("contact accent not applied: %q", name)
	}
}

// The contact list is hidden until it is wanted: the sidebar shows threads, and
// c swaps it in, Esc swaps it back, without disturbing the open conversation.
func TestContactsPaneAppearsOnlyWhenRequested(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	m.setPane(paneThreads)

	view := ansi.Strip(m.View())
	if strings.Contains(view, "Contacts") {
		t.Fatalf("contact list shown unasked:\n%s", view)
	}
	if !strings.Contains(view, "Threads") {
		t.Fatal("thread list missing")
	}
	wide := m.history.view.Width

	d.press("c")
	if m.history.pane != paneContacts {
		t.Fatalf("c did not open contacts: %d", m.history.pane)
	}
	if view = ansi.Strip(m.View()); !strings.Contains(view, "Contacts") {
		t.Fatalf("contacts not shown:\n%s", view)
	}
	if m.history.view.Width != wide {
		t.Errorf("conversation resized with the sidebar: %d then %d", wide, m.history.view.Width)
	}
	if m.history.active == nil || m.history.active.ID != amyThread {
		t.Fatal("opening contacts closed the conversation")
	}

	d.press("esc")
	if m.history.pane != paneThreads {
		t.Fatalf("esc did not return to threads: %d", m.history.pane)
	}
	if view = ansi.Strip(m.View()); strings.Contains(view, "Contacts") {
		t.Fatal("contacts stayed visible after esc")
	}

	// Tab skips the hidden pane rather than cycling through it.
	m.setPane(paneThreads)
	seen := map[int]bool{}
	for i := 0; i < 3; i++ {
		m.cyclePane(false)
		seen[m.history.pane] = true
	}
	if seen[paneContacts] {
		t.Fatal("Tab cycled into the hidden contact list")
	}
}

// The editing mode belongs in the status bar, not above the composer. The line
// it occupied is only used to say why sending is unavailable.
func TestComposerShowsNoModeLabel(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	m.setPane(paneComposer)
	view := ansi.Strip(m.View())
	if strings.Contains(view, "· Ripple") {
		t.Errorf("composer still labels the editor:\n%s", view)
	}
	if !strings.Contains(view, "INSERT") {
		t.Error("editing mode disappeared from the status bar")
	}
	rows := m.history.view.Height

	// A group still explains why it cannot be replied to, and pays a row for it.
	openThreadByID(t, d, familyThread)
	d.settle("group history", func() bool { return len(m.history.view.Messages) == 1 })
	if view = ansi.Strip(m.View()); !strings.Contains(view, "sending unavailable") {
		t.Errorf("group gives no explanation:\n%s", view)
	}
	if m.history.view.Height != rows-1 {
		t.Errorf("notice did not come out of the conversation: %d then %d", rows, m.history.view.Height)
	}
	if lines := strings.Split(m.View(), "\n"); len(lines) > m.height {
		t.Errorf("view grew to %d lines for a %d-row terminal", len(lines), m.height)
	}
}

// Message frames are on by default and can be turned off from settings, which
// persists and takes effect immediately.
func TestMessageBubblesToggleFromSettings(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	d.settle("history", func() bool { return len(m.history.view.Messages) == 3 })
	if !m.cfg.Conversation.Bubbles {
		t.Fatal("frames should be the default")
	}
	frames := func() bool {
		return strings.Contains(ansi.Strip(m.history.view.View(m.conversationRenderer(), false)), "╭")
	}
	if !frames() {
		t.Fatal("no frames drawn")
	}

	d.run(m.action("Open settings"))
	if m.modal != "settings" {
		t.Fatalf("modal %q", m.modal)
	}
	selectSetting(t, m, "Message bubbles")
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	d.settle("saved", func() bool { return !m.busy && !m.cfg.Conversation.Bubbles })
	if frames() {
		t.Fatal("frames survived the toggle")
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "Are we still meeting") {
		t.Fatalf("message text lost with frames off:\n%s", view)
	}
	if m.history.active == nil || m.history.active.ID != amyThread {
		t.Fatal("saving a setting closed the conversation")
	}

	// The choice is written to disk, so a restart keeps it.
	saved, err := config.Load(m.configPath)
	if err != nil || saved.Conversation.Bubbles {
		t.Fatalf("not persisted: %+v %v", saved.Conversation, err)
	}
	d.run(m.action("Toggle message bubbles"))
	d.settle("back on", func() bool { return m.cfg.Conversation.Bubbles })
	if !frames() {
		t.Fatal("frames did not come back")
	}
}

// The corner style is a setting of its own, applied live and persisted.
func TestBubbleCornersToggleFromSettings(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	d.settle("history", func() bool { return len(m.history.view.Messages) == 3 })
	drawn := func(r rune) bool {
		return strings.ContainsRune(ansi.Strip(m.history.view.View(m.conversationRenderer(), false)), r)
	}
	if m.cfg.Conversation.Corners != "round" || !drawn('╭') {
		t.Fatalf("round should be the default, got %q", m.cfg.Conversation.Corners)
	}

	d.run(m.action("Open settings"))
	selectSetting(t, m, "Bubble corners")
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	d.settle("square", func() bool { return !m.busy && m.cfg.Conversation.Corners == "square" })
	if !drawn('┌') || drawn('╭') {
		t.Fatal("corners did not become square")
	}
	if m.history.active == nil {
		t.Fatal("changing corners closed the conversation")
	}
	saved, err := config.Load(m.configPath)
	if err != nil || saved.Conversation.Corners != "square" {
		t.Fatalf("not persisted: %+v %v", saved.Conversation, err)
	}

	d.run(m.action("Toggle bubble corners"))
	d.settle("round again", func() bool { return m.cfg.Conversation.Corners == "round" })
	if !drawn('╭') {
		t.Fatal("corners did not return to round")
	}
}

// A contact's theme colours their conversation and nothing else: the lists, the
// status bar and the modals stay on the global theme.
func TestContactThemeAppliesOnlyToTheConversation(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	shell := m.renderer().Styles.Theme
	if shell.Name != m.cfg.General.Theme {
		t.Fatalf("shell is not on the global theme: %q vs %q", shell.Name, m.cfg.General.Theme)
	}

	openThreadByID(t, d, amyThread)
	if got := m.conversationRenderer().Styles.Theme.Name; got != "rose" {
		t.Fatalf("conversation ignored the contact theme: %q", got)
	}
	if got := m.renderer().Styles.Theme; got.Name != shell.Name || got.Bg != shell.Bg {
		t.Fatalf("contact theme leaked into the shell: %+v", got)
	}

	// A full TideUI theme on the thread repaints the conversation, still not the shell.
	d.run(m.action("Change thread theme"))
	for i, c := range m.choices {
		if c == "gruvbox-dark" {
			m.choice = i
		}
	}
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	conv := m.conversationRenderer().Styles.Theme
	if conv.Name != "gruvbox-dark" {
		t.Fatalf("thread theme not applied: %q", conv.Name)
	}
	if got := m.renderer().Styles.Theme; got.Bg != shell.Bg || got.Fg != shell.Fg {
		t.Fatalf("thread theme leaked into the shell: %+v", got)
	}

	// Switching to a thread with no theme returns the conversation to the global
	// palette, accented for that person.
	openThreadByID(t, d, unknownThread)
	if got := m.conversationRenderer().Styles.Theme; got.Bg != shell.Bg {
		t.Fatalf("a themeless thread kept another palette: %+v", got)
	}
	if got := m.renderer().Styles.Theme.Bg; got != shell.Bg {
		t.Fatal("shell drifted")
	}
}

// bgSequence is the true-colour background escape a colour actually paints
// with. It is taken from a real render rather than computed from the hex,
// because lipgloss round-trips some values a shade off.
func bgSequence(c lipgloss.Color) string {
	return regexp.MustCompile(`48;2;\d+;\d+;\d+`).FindString(lipgloss.NewStyle().Background(c).Render("x"))
}

// The conversation is painted on its own background, so a themed thread shows
// its palette rather than only its text colours, and the shell keeps its own.
func TestConversationPaintsItsOwnBackground(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	d.settle("history", func() bool { return len(m.history.view.Messages) == 3 })
	shell := m.renderer().Styles.Theme.Bg

	d.run(m.action("Change thread theme"))
	for i, c := range m.choices {
		if c == "gruvbox-dark" {
			m.choice = i
		}
	}
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	d.settle("themed", func() bool { return m.history.active.ThemeID == "gruvbox-dark" })

	conv := m.conversationRenderer().Styles.Theme.Bg
	if conv == shell {
		t.Fatal("test needs two different backgrounds")
	}
	view := m.View()
	if !strings.Contains(view, bgSequence(conv)) {
		t.Error("the conversation is not painted with its own background")
	}
	if !strings.Contains(view, bgSequence(shell)) {
		t.Error("the shell lost its background")
	}
}

// Moving through a theme picker shows the palette immediately, and the shell
// stays put while a conversation theme is being chosen.
func TestThemePickersPreviewLive(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	shell := m.renderer().Styles.Theme.Bg

	d.run(m.action("Change thread theme"))
	for i, c := range m.choices {
		if c == "coral-sunset" {
			m.choice = i
		}
	}
	preview := m.conversationRenderer().Styles.Theme
	if preview.Name != "coral-sunset" {
		t.Fatalf("picker does not preview: %q", preview.Name)
	}
	if view := m.View(); !strings.Contains(view, bgSequence(preview.Bg)) {
		t.Error("the previewed palette is not drawn")
	}
	if m.renderer().Styles.Theme.Bg != shell {
		t.Error("previewing a thread theme moved the shell")
	}
	// Nothing is committed until Enter.
	if m.history.active.ThemeID == "coral-sunset" || m.cfg.General.Theme == "coral-sunset" {
		t.Error("the preview was saved without confirmation")
	}

	// The static settings panel previews the shell from its Theme row.
	m.openSettings()
	m.choice = 0
	for i, c := range themes.Names {
		if c == "gruvbox-light" {
			m.themeCursor = i
		}
	}
	if got := m.renderer().Styles.Theme.Name; got != "gruvbox-light" {
		t.Errorf("settings theme row does not preview the shell: %q", got)
	}
	if m.cfg.General.Theme == "gruvbox-light" {
		t.Error("preview was saved without Enter")
	}
}

// Bubble fill is a setting of its own, applied live and persisted.
func TestBubbleFillToggleFromSettings(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	d.settle("history", func() bool { return len(m.history.view.Messages) == 3 })
	surface := bgSequence(m.conversationRenderer().Styles.Theme.Overlay)
	filled := func() bool {
		return strings.Contains(m.history.view.View(m.conversationRenderer(), false), surface)
	}
	if !m.cfg.Conversation.FillBubbles || !filled() {
		t.Fatal("fill should be the default")
	}

	d.run(m.action("Open settings"))
	selectSetting(t, m, "Bubble fill")
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	d.settle("unfilled", func() bool { return !m.busy && !m.cfg.Conversation.FillBubbles })
	if filled() {
		t.Error("fill survived the toggle")
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "╭") {
		t.Error("turning fill off removed the frames")
	}
	saved, err := config.Load(m.configPath)
	if err != nil || saved.Conversation.FillBubbles {
		t.Fatalf("not persisted: %+v %v", saved.Conversation, err)
	}

	d.run(m.action("Toggle bubble fill"))
	d.settle("filled again", func() bool { return m.cfg.Conversation.FillBubbles })
	if !filled() {
		t.Error("fill did not come back")
	}
}
