package app

import (
	"errors"
	"github.com/allisonhere/tidesms/internal/contacts"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func phoneBook() []contacts.Synced {
	return []contacts.Synced{
		{UID: "1", Name: "Mom", PhoneNumber: momNumber, RawNumber: "(555) 987-6543"},
		{UID: "2", Name: "Package Co", PhoneNumber: unknown, RawNumber: unknown},
		{UID: "3", Name: "Amy From Phone", PhoneNumber: amyNumber, RawNumber: amyNumber},
	}
}

// Imported names fill in threads that had only a number, a local contact still
// wins over the phone's version of the same number, and nothing the user owns
// is written to.
func TestSyncedContactsNameThreadsWithoutTouchingLocalContacts(t *testing.T) {
	m, b, s, d, _ := conversationFixture(t)
	b.Contacts = phoneBook()
	syncPhone(t, d)
	if name := thread(t, m, unknownThread).DisplayName; name != unknown {
		t.Fatalf("precondition: %q", name)
	}
	d.run(m.syncContacts(true))
	d.settle("import", func() bool { return len(m.synced) == 3 })

	if !strings.Contains(m.notice, "Imported 3") || m.failed {
		t.Fatalf("notice %q", m.notice)
	}
	if name := thread(t, m, unknownThread).DisplayName; name != "Package Co" {
		t.Fatalf("synced name not used: %q", name)
	}
	if name := thread(t, m, amyThread).DisplayName; name != "Amy" {
		t.Fatalf("phone overrode a local contact: %q", name)
	}
	if name := thread(t, m, familyThread).DisplayName; name != "Family" {
		t.Fatalf("group name changed: %q", name)
	}
	local, err := s.Contacts()
	if err != nil || len(local) != 1 || local[0].Name != "Amy" {
		t.Fatalf("local contacts were modified: %+v %v", local, err)
	}

	// Re-importing replaces rather than accumulates, and a contact removed on
	// the phone disappears from the overlay.
	b.Contacts = phoneBook()[:1]
	d.run(m.syncContacts(true))
	d.settle("second import", func() bool { return len(m.synced) == 1 })
	if name := thread(t, m, unknownThread).DisplayName; name != unknown {
		t.Fatalf("removed contact still named a thread: %q", name)
	}
}

// The sidebar lists the user's own contacts plus the imported people they are
// actually in a conversation with. An imported contact with no thread stays out
// of the list but remains reachable when starting a message.
func TestSyncedContactsListedAfterLocalOnes(t *testing.T) {
	m, b, _, d, _ := conversationFixture(t)
	b.Contacts = append(phoneBook(), contacts.Synced{UID: "4", Name: "Dentist", PhoneNumber: "+15550001111", RawNumber: "+15550001111"})
	syncPhone(t, d)
	d.run(m.syncContacts(true))
	d.settle("import", func() bool { return len(m.synced) == 4 })

	got := m.filtered()
	for _, c := range got {
		if c.Name == "Dentist" {
			t.Fatal("an imported contact with no conversation was listed")
		}
	}
	var all []string
	for _, c := range m.allContacts() {
		all = append(all, c.Name)
	}
	if len(m.allContacts()) != 4 || !strings.Contains(strings.Join(all, ","), "Dentist") {
		t.Fatalf("address book incomplete: %v", all)
	}
	if len(got) != 3 {
		t.Fatalf("expected Amy plus two overlay entries: %+v", got)
	}
	if got[0].Name != "Amy" || got[0].Synced {
		t.Fatalf("local contact not first: %+v", got[0])
	}
	if got[1].Name != "Mom" || !got[1].Synced || got[2].Name != "Package Co" || !got[2].Synced {
		t.Fatalf("overlay: %+v", got[1:])
	}
	m.setPane(paneContacts)
	if view := m.View(); !strings.Contains(view, "⟲") {
		t.Fatal("synced entries are not marked in the contact list")
	}
	m.query = "package"
	if got = m.filtered(); len(got) != 1 || got[0].Name != "Package Co" {
		t.Fatalf("search does not reach the overlay: %+v", got)
	}
}

// Editing or theming an entry from the phone keeps a local copy; deleting one is
// refused, because the phone owns it.
func TestSyncedContactPromotesOnEditAndRefusesDelete(t *testing.T) {
	m, b, s, d, _ := conversationFixture(t)
	b.Contacts = phoneBook()
	syncPhone(t, d)
	d.run(m.syncContacts(true))
	d.settle("import", func() bool { return len(m.synced) == 3 })
	m.setPane(paneContacts)
	m.selected = 1 // Mom, from the phone.

	d.run(m.action("Delete contact"))
	if m.modal == "delete" || !m.failed || !strings.Contains(m.notice, "from your phone") {
		t.Fatalf("delete was not refused: modal %q notice %q", m.modal, m.notice)
	}

	d.run(m.action("Edit contact"))
	if m.modal != "edit" || m.fields[0].Value() != "Mom" || m.fields[1].Value() != momNumber {
		t.Fatalf("edit form not prefilled from the overlay: %q", m.modal)
	}
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	d.settle("promotion", func() bool { return !m.busy && len(m.contacts) == 2 })

	local, err := s.Contacts()
	if err != nil || len(local) != 2 {
		t.Fatalf("promotion did not save a local contact: %+v %v", local, err)
	}
	var promoted contacts.Contact
	for _, c := range local {
		if c.PhoneNumber == momNumber {
			promoted = c
		}
	}
	if promoted.ID == "" || promoted.Name != "Mom" || promoted.Synced {
		t.Fatalf("promoted contact: %+v", promoted)
	}
	got := m.filtered()
	if len(got) != 3 {
		t.Fatalf("promotion changed the list length: %+v", got)
	}
	for _, c := range got {
		if c.PhoneNumber == momNumber && c.Synced {
			t.Fatal("the phone's copy still shadows the promoted contact")
		}
	}

	// Theming another overlay entry promotes it the same way. Saving a contact
	// focuses the composer on it, so return to the list first.
	m.setPane(paneContacts)
	m.selected = 2 // Package Co, still from the phone.
	d.run(m.action("Change contact theme"))
	for i, c := range m.choices {
		if c == "tokyo-night" {
			m.choice = i
		}
	}
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	d.settle("themed", func() bool { return !m.busy && len(m.contacts) == 3 })
	local, err = s.Contacts()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range local {
		if c.PhoneNumber == unknown && (c.Theme != "tokyo-night" || c.ID == "") {
			t.Fatalf("themed overlay entry not promoted cleanly: %+v", c)
		}
	}
}

// A failed import is reported when the user asked for it and stays quiet when it
// was automatic, and never disturbs names already cached.
func TestContactImportFailureIsQuietInTheBackground(t *testing.T) {
	m, b, _, d, _ := conversationFixture(t)
	b.Contacts = phoneBook()
	d.run(m.syncContacts(true))
	d.settle("import", func() bool { return len(m.synced) == 3 })

	b.ContactsError = errors.New("phone offline")
	m.notify("", false)
	d.run(m.syncContacts(false))
	d.settle("background attempt", func() bool { return b.ContactCalls == 2 })
	if m.notice != "" || m.failed {
		t.Fatalf("background failure nagged: %q", m.notice)
	}
	if len(m.synced) != 3 {
		t.Fatal("failed import discarded cached names")
	}

	d.run(m.syncContacts(true))
	d.settle("manual attempt", func() bool { return b.ContactCalls == 3 })
	if !m.failed || !strings.Contains(m.notice, "offline") {
		t.Fatalf("manual failure not reported: %q", m.notice)
	}
	if len(m.synced) != 3 {
		t.Fatal("failed import discarded cached names")
	}
}

// The import runs once per phone rather than on every device refresh, and the
// configuration switch turns it off entirely.
func TestContactImportRunsOncePerPhoneAndHonoursConfig(t *testing.T) {
	m, b, _, d, _ := conversationFixture(t)
	b.Contacts = phoneBook()
	d.run(m.maybeSyncContacts())
	d.settle("first import", func() bool { return b.ContactCalls == 1 })
	for i := 0; i < 3; i++ {
		d.run(m.maybeSyncContacts())
	}
	if b.ContactCalls != 1 {
		t.Fatalf("imported %d times for one phone", b.ContactCalls)
	}

	m.cfg.Contacts.SyncFromPhone = false
	if cmd := m.syncContacts(true); cmd != nil {
		t.Fatal("import ran while disabled")
	}
	if !m.failed || !strings.Contains(m.notice, "sync_from_phone") {
		t.Fatalf("disabled import not explained: %q", m.notice)
	}
}

// Starting a message searches the whole address book, reaches people with no
// conversation yet, accepts a typed number, and never opens a second thread for
// someone who already has one.
func TestComposeSearchesTheWholeAddressBook(t *testing.T) {
	m, b, _, d, _ := conversationFixture(t)
	b.Contacts = append(phoneBook(), contacts.Synced{UID: "4", Name: "Dentist", PhoneNumber: "+15550001111", RawNumber: "+15550001111"})
	syncPhone(t, d)
	d.run(m.syncContacts(true))
	d.settle("import", func() bool { return len(m.synced) == 4 })

	d.run(m.openCompose())
	if m.modal != "compose" {
		t.Fatalf("picker did not open: %q", m.modal)
	}
	if len(m.picks) != 4 {
		t.Fatalf("picker should list the whole address book: %+v", m.choices)
	}

	// Someone with no conversation is reachable here and becomes the recipient.
	m.filter.SetValue("dent")
	m.refreshCompose()
	if len(m.picks) != 1 || m.picks[0].Name != "Dentist" {
		t.Fatalf("search: %+v", m.choices)
	}
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	if m.modal != "" || m.recipient.Name != "Dentist" {
		t.Fatalf("recipient %+v modal %q", m.recipient, m.modal)
	}
	if m.history.active != nil {
		t.Fatal("a contact with no history opened a thread")
	}

	// Choosing someone who already has a thread opens it rather than starting over.
	d.run(m.openCompose())
	m.filter.SetValue("Amy")
	m.refreshCompose()
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	if m.history.active == nil || m.history.active.ID != amyThread {
		t.Fatalf("existing thread not reused: %+v", m.history.active)
	}

	// A number typed in full is offered even when nobody matches.
	d.run(m.openCompose())
	m.filter.SetValue("+1 (555) 222-3333")
	m.refreshCompose()
	if len(m.picks) != 1 || m.choices[0] != "Send to +15552223333" {
		t.Fatalf("typed number not offered: %+v", m.choices)
	}
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	if m.recipient.PhoneNumber != "+15552223333" {
		t.Fatalf("recipient %+v", m.recipient)
	}

	// An incomplete number offers nothing to send to.
	d.run(m.openCompose())
	m.filter.SetValue("12")
	m.refreshCompose()
	for _, c := range m.choices {
		if strings.HasPrefix(c, "Send to ") {
			t.Fatalf("offered an unusable number: %q", c)
		}
	}
}

// A contact the phone stores without a country code still opens the existing
// conversation whose address arrived in full international form.
func TestComposeMatchesExistingThreadAcrossNumberFormats(t *testing.T) {
	m, b, _, d, _ := conversationFixture(t)
	b.Contacts = []contacts.Synced{{UID: "9", Name: "Package Co", PhoneNumber: "8165550182", RawNumber: "(816) 555-0182"}}
	syncPhone(t, d)
	d.run(m.syncContacts(true))
	d.settle("import", func() bool { return len(m.synced) == 1 })

	d.run(m.openCompose())
	m.filter.SetValue("Package")
	m.refreshCompose()
	if len(m.picks) != 1 {
		t.Fatalf("%+v", m.choices)
	}
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	if m.history.active == nil || m.history.active.ID != unknownThread {
		t.Fatalf("thread not matched across formats: %+v", m.history.active)
	}
}
