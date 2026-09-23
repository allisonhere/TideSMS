package app

import (
	"strings"
	"testing"

	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tidesms/internal/storage"
)

// cycleDetail moves a colour row of the details screen to the named theme.
func cycleDetail(t *testing.T, m *Model, row detailsRow, want string) {
	t.Helper()
	for i, f := range m.detailsFields() {
		if f.id == row {
			m.choice = i
		}
	}
	for i := 0; i < len(contactThemeNames()) && m.detailsValue(row) != want; i++ {
		m.detailsAdjust(1)
	}
	if got := m.detailsValue(row); got != want {
		t.Fatalf("could not cycle to %q: %q", want, got)
	}
}

func storedContact(t *testing.T, st *storage.Store, phone string) (contacts.Contact, bool) {
	t.Helper()
	cs, err := st.Contacts()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		if c.PhoneNumber == phone {
			return c, true
		}
	}
	return contacts.Contact{}, false
}

func storedThread(t *testing.T, st *storage.Store, id string) domain.Thread {
	t.Helper()
	ts, err := st.Threads(device)
	if err != nil {
		t.Fatal(err)
	}
	for _, th := range ts {
		if th.ID == id {
			return th
		}
	}
	t.Fatalf("thread %s not stored", id)
	return domain.Thread{}
}

// i in the conversation opens details for that conversation.
func TestDetailsOpensForTheOpenConversation(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	d.press("i")
	if m.modal != "details" {
		t.Fatalf("modal = %q", m.modal)
	}
	if m.details.group {
		t.Error("a one-to-one chat opened as a group")
	}
	if !strings.Contains(m.detailsTitle(), m.recipient.Name) {
		t.Errorf("title %q does not name %q", m.detailsTitle(), m.recipient.Name)
	}
}

// From the thread list, i means the highlighted thread, not whichever contact
// happens to sort first.
func TestDetailsFromThreadListOpensTheHighlightedThread(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	d.settle("threads", func() bool { return len(m.history.threads) == 3 })
	m.setPane(paneThreads)
	m.history.threadSelected = unknownThread
	d.press("i")
	if m.modal != "details" {
		t.Fatalf("modal = %q", m.modal)
	}
	if m.history.active == nil || m.history.active.ID != unknownThread {
		t.Fatalf("details opened on the wrong thread: %+v", m.history.active)
	}
	if m.recipient.PhoneNumber != unknown {
		t.Errorf("details describe %q, not the highlighted thread", m.recipient.PhoneNumber)
	}
}

// Ctrl+L reaches details while typing, without typing into the draft; Ctrl+D
// keeps its old meanings.
func TestCtrlLOpensDetailsFromTheComposer(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	m.setPane(paneComposer)
	typeText(m, "hi")
	d.press("ctrl+l")
	if m.modal != "details" {
		t.Fatalf("Ctrl+L did not open details: %q", m.modal)
	}
	if got := m.editor.Value(); got != "hi" {
		t.Errorf("the draft changed: %q", got)
	}
	d.press("esc")
	m.setPane(paneConversation)
	d.press("ctrl+d")
	if m.modal != "" {
		t.Errorf("Ctrl+D opened %q instead of scrolling", m.modal)
	}
}

// A one-to-one chat's colours preview everywhere at once, write nothing until
// Ctrl+S, and then belong to the person.
func TestDetailsPreviewThenSaveToThePerson(t *testing.T) {
	m, _, st, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	phone := m.recipient.PhoneNumber
	d.press("i")

	cycleDetail(t, m, detailsTheme, "nord")
	cycleDetail(t, m, detailsIn, "dracula")
	if got := m.conversationTheme().Name; got != "nord" {
		t.Errorf("conversation did not preview: %q", got)
	}
	if got := m.threadThemes()[amyThread]; got != "nord" {
		t.Errorf("thread row did not preview: %q", got)
	}
	if got := m.bubblePalette(m.conversationTheme(), false).Name; got != "dracula" {
		t.Errorf("incoming bubbles did not preview: %q", got)
	}
	if c, ok := storedContact(t, st, phone); ok && (c.Theme == "nord" || c.ThemeIn == "dracula") {
		t.Fatal("colours were written before Ctrl+S")
	}
	if !strings.Contains(m.detailsTitle(), "unsaved") {
		t.Error("title does not say the draft is unsaved")
	}

	d.press("ctrl+s")
	d.settle("saved", func() bool { return !m.busy })
	if m.modal != "details" {
		t.Errorf("saving closed the screen: %q", m.modal)
	}
	c, ok := storedContact(t, st, phone)
	if !ok || c.Theme != "nord" || c.ThemeIn != "dracula" {
		t.Fatalf("not saved to the contact: %+v", c)
	}
	d.press("esc")
	if got := m.conversationTheme().Name; got != "nord" {
		t.Errorf("closing after saving lost the theme: %q", got)
	}
}

// Esc puts back what was saved.
func TestDetailsEscDiscards(t *testing.T) {
	m, _, st, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	before := m.conversationTheme().Name
	d.press("i")
	cycleDetail(t, m, detailsTheme, "nord")
	d.press("esc")
	if m.modal != "" {
		t.Fatalf("Esc left %q open", m.modal)
	}
	if got := m.conversationTheme().Name; got != before {
		t.Errorf("discarded preview stuck: %q, want %q", got, before)
	}
	if c, ok := storedContact(t, st, m.recipient.PhoneNumber); ok && c.Theme == "nord" {
		t.Error("Esc saved the draft")
	}
}

// A group has no single person, so its colours go on the thread and no member
// is touched.
func TestDetailsGroupSavesToTheThread(t *testing.T) {
	m, _, st, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, familyThread)
	d.press("i")
	if !m.details.group {
		t.Fatal("the group opened as a one-to-one chat")
	}
	for _, f := range m.detailsFields() {
		if f.id == detailsRename {
			t.Error("a group offered Rename")
		}
	}
	cycleDetail(t, m, detailsTheme, "nord")
	d.press("ctrl+s")
	d.settle("saved", func() bool { return !m.busy })
	if got := storedThread(t, st, familyThread).ThemeID; got != "nord" {
		t.Errorf("thread theme = %q", got)
	}
	cs, _ := st.Contacts()
	for _, c := range cs {
		if c.Theme == "nord" {
			t.Errorf("a member was given the group's theme: %s", c.Name)
		}
	}
}

// A one-to-one thread may carry an override from the palette's thread
// commands. Details starts from it and moves it onto the person on save, so the
// two cannot disagree afterwards.
func TestDetailsMovesAThreadOverrideOntoThePerson(t *testing.T) {
	m, _, st, d, _ := conversationFixture(t)
	syncPhone(t, d)
	if err := st.ThreadTheme(amyThread, "dracula"); err != nil {
		t.Fatal(err)
	}
	openThreadByID(t, d, amyThread)
	d.settle("override loaded", func() bool { return m.history.active != nil && m.history.active.ThemeID == "dracula" })
	d.press("i")
	if got := m.detailsValue(detailsTheme); got != "dracula" {
		t.Fatalf("details started from %q, not the override in effect", got)
	}
	cycleDetail(t, m, detailsTheme, "nord")
	d.press("ctrl+s")
	d.settle("saved", func() bool { return !m.busy })
	if c, _ := storedContact(t, st, m.recipient.PhoneNumber); c.Theme != "nord" {
		t.Errorf("contact theme = %q", c.Theme)
	}
	if got := storedThread(t, st, amyThread).ThemeID; got != "" {
		t.Errorf("thread override %q survived and would hide the person's theme", got)
	}
}

// A number that is not a contact can still be given colours; it becomes a
// local contact to hold them.
func TestDetailsColoursAnUnsavedNumber(t *testing.T) {
	m, _, st, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, unknownThread)
	d.press("i")
	cycleDetail(t, m, detailsTheme, "nord")
	d.press("ctrl+s")
	d.settle("saved", func() bool { return !m.busy })
	if c, ok := storedContact(t, st, unknown); !ok || c.Theme != "nord" || c.ID == "" {
		t.Fatalf("unsaved number was not promoted with its theme: %+v ok=%v", c, ok)
	}
}

// Muting from details is stored per person for a one-to-one chat.
func TestDetailsMuteIsStoredPerPerson(t *testing.T) {
	m, _, st, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	d.press("i")
	for i, f := range m.detailsFields() {
		if f.id == detailsNotify {
			m.choice = i
		}
	}
	d.press("enter")
	if m.detailsValue(detailsNotify) != "muted" {
		t.Fatal("Enter did not change the row")
	}
	if _, ok, _ := st.NotificationMode(storage.ScopeContact, m.recipient.PhoneNumber); ok {
		t.Fatal("muted before Ctrl+S")
	}
	d.press("ctrl+s")
	d.settle("saved", func() bool { return !m.busy })
	if mode, ok, _ := st.NotificationMode(storage.ScopeContact, m.recipient.PhoneNumber); !ok || mode != notifMuted {
		t.Fatalf("mute not stored: %q %v", mode, ok)
	}
}

// Settings holds app-wide defaults only.
func TestSettingsNoLongerHasContactTheme(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	d.run(m.action("Open settings"))
	for _, f := range m.settingsFields() {
		if f.label == "Contact theme" {
			t.Fatal("Settings still lists Contact theme")
		}
	}
}
