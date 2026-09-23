package app

import (
	"testing"

	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tidesms/internal/themes"
)

func themesNamesForTest() []string { return themes.Names }

// A thread's row shows the palette its conversation will open in: its own
// override when it has one, otherwise the theme of the contact it belongs to.
// Without the second half, a theme set on a contact was visible only once that
// conversation was already open.
func TestThreadThemesFallBackToTheContact(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	d.settle("threads", func() bool { return len(m.history.threads) > 0 })

	var solo *domain.Thread
	for i := range m.history.threads {
		if !m.history.threads[i].IsGroup && len(m.history.threads[i].Participants) == 1 {
			solo = &m.history.threads[i]
			break
		}
	}
	if solo == nil {
		t.Skip("fixture has no one-to-one thread")
	}
	number := solo.Participants[0].Number

	// The fixture seeds a themed contact, so start from a known state: with no
	// contact carrying a theme, no row may claim one.
	for i := range m.contacts {
		m.contacts[i].Theme = ""
	}
	solo.ThemeID = ""
	if got := m.threadThemes()[solo.ID]; got != "" {
		t.Fatalf("an unthemed thread reported %q", got)
	}

	// A theme on the contact reaches the thread's row.
	m.contacts = append(m.contacts, contacts.Contact{
		ID: "c-themed", Name: "Themed", PhoneNumber: number, Theme: "nord",
	})
	if got := m.threadThemes()[solo.ID]; got != "nord" {
		t.Errorf("contact theme did not reach the thread row: %q", got)
	}

	// A thread override beats the contact.
	solo.ThemeID = "gruvbox-dark"
	if got := m.threadThemes()[solo.ID]; got != "gruvbox-dark" {
		t.Errorf("thread override lost to the contact: %q", got)
	}
}

// A group belongs to no single contact, so it shows only an explicit thread
// theme rather than borrowing one member's.
func TestGroupThreadTakesOnlyItsOwnTheme(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	d.settle("threads", func() bool { return len(m.history.threads) > 0 })

	m.history.threads = []domain.Thread{{
		ID:      "group-1",
		IsGroup: true,
		Participants: []domain.Participant{
			{Number: "+15550001"}, {Number: "+15550002"},
		},
	}}
	for i := range m.contacts {
		m.contacts[i].Theme = ""
	}
	m.contacts = append(m.contacts, contacts.Contact{
		ID: "c1", Name: "One", PhoneNumber: "+15550001", Theme: "nord",
	})
	if got := m.threadThemes()["group-1"]; got != "" {
		t.Errorf("a group borrowed a member's theme: %q", got)
	}

	m.history.threads[0].ThemeID = "nord"
	if got := m.threadThemes()["group-1"]; got != "nord" {
		t.Errorf("a group ignored its own theme: %q", got)
	}
}

// A row prefers a conversation palette to a bubble palette, and within each
// the thread's own setting to the contact's. ThemeOut never colours a row: it
// is your own messages in their thread, not a mark of who they are.
func TestThreadThemeResolutionOrder(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	d.settle("threads", func() bool { return len(m.history.threads) > 0 })

	const number = "+15550001"
	m.contacts = []contacts.Contact{{ID: "c1", Name: "One", PhoneNumber: number}}
	m.history.threads = []domain.Thread{{
		ID: "t1", Participants: []domain.Participant{{Number: number}},
	}}
	thread := &m.history.threads[0]
	contact := &m.contacts[0]

	themeOf := func() string { return m.threadThemes()["t1"] }

	if got := themeOf(); got != "" {
		t.Fatalf("nothing set, got %q", got)
	}

	// An outgoing bubble palette alone colours nothing.
	contact.ThemeOut, thread.ThemeOut = "gruvbox-dark", "gruvbox-dark"
	if got := themeOf(); got != "" {
		t.Errorf("an outgoing bubble palette coloured the row: %q", got)
	}

	// The contact's incoming bubble palette is the weakest thing that does.
	contact.ThemeIn = "nord"
	if got := themeOf(); got != "nord" {
		t.Errorf("contact bubble palette: got %q, want nord", got)
	}
	// The thread's own bubble palette is more specific.
	thread.ThemeIn = "dracula"
	if got := themeOf(); got != "dracula" {
		t.Errorf("thread bubble palette: got %q, want dracula", got)
	}
	// A whole-conversation theme beats any bubble palette.
	contact.Theme = "rose-pine"
	if got := themeOf(); got != "rose-pine" {
		t.Errorf("contact theme should beat a bubble palette: got %q", got)
	}
	// And the thread's own theme beats the contact's.
	thread.ThemeID = "tokyo-night"
	if got := themeOf(); got != "tokyo-night" {
		t.Errorf("thread theme should win outright: got %q", got)
	}
}

// rowTheme is the palette the sidebar would draw for a contact right now.
func rowTheme(m *Model, phone string) string {
	for _, c := range m.contactRows() {
		if c.PhoneNumber == phone {
			return c.Theme
		}
	}
	return "(absent)"
}

// The picker t opens previews the sidebar row as it is moved through.
func TestThemePickerPreviewsTheSidebarRow(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	d.settle("contacts", func() bool { return len(m.filtered()) > 0 })

	target := m.filtered()[0]
	m.editing = target
	m.modal = "themes"
	m.choices = append([]string{"automatic"}, themesNamesForTest()...)
	m.choice = 2

	want := m.choices[2]
	if got := rowTheme(m, target.PhoneNumber); got != want {
		t.Errorf("the picker did not preview the row: got %q, want %q", got, want)
	}
	// "automatic" previews no override rather than the literal word.
	m.choice = 0
	if got := rowTheme(m, target.PhoneNumber); got != "" {
		t.Errorf("automatic previewed as %q, want no override", got)
	}
}

// A preview belongs to the contact being edited and to no other row.
func TestPreviewColoursOnlyItsOwnRow(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)

	// Two contacts of our own, so the preview has somewhere wrong to land.
	m.contacts = []contacts.Contact{
		{ID: "c-target", Name: "Target", PhoneNumber: "+15550001"},
		{ID: "c-other", Name: "Other", PhoneNumber: "+15550002"},
	}
	m.query = ""
	list := m.filtered()
	if len(list) < 2 {
		t.Fatalf("expected both contacts to be listed, got %d", len(list))
	}
	target, other := list[0], list[1]
	m.editing = target
	m.modal = "themes"
	m.choices = append([]string{"automatic"}, themesNamesForTest()...)
	m.choice = 2

	if got := rowTheme(m, target.PhoneNumber); got != m.choices[2] {
		t.Errorf("the edited contact was not previewed: %q", got)
	}
	if got := rowTheme(m, other.PhoneNumber); got == m.choices[2] {
		t.Errorf("a different contact borrowed the preview: %q", got)
	}
}

// Changing a contact's theme from the thread list acts on the highlighted
// thread's person. It used to fall back to the hidden contact list's cursor,
// which sits at zero, so the theme landed on whoever sorted first.
func TestContactThemeTargetsTheHighlightedThread(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	d.settle("threads", func() bool { return len(m.history.threads) > 0 })

	var solo *domain.Thread
	for i := range m.history.threads {
		if !m.history.threads[i].IsGroup && len(m.history.threads[i].Participants) == 1 {
			solo = &m.history.threads[i]
			break
		}
	}
	if solo == nil {
		t.Skip("fixture has no one-to-one thread")
	}
	// Someone who sorts first, and the person the thread is actually with.
	m.contacts = append([]contacts.Contact{{ID: "c-first", Name: "Aaron", PhoneNumber: "+15550009999"}}, m.contacts...)
	m.contacts = append(m.contacts, contacts.Contact{ID: "c-thread", Name: "Zed", PhoneNumber: solo.Participants[0].Number})
	for i := range m.contacts {
		if m.contacts[i].PhoneNumber == solo.Participants[0].Number && m.contacts[i].ID != "c-thread" {
			m.contacts = append(m.contacts[:i], m.contacts[i+1:]...)
			break
		}
	}
	m.selected = 0
	m.setPane(paneThreads)
	m.history.threadSelected = solo.ID

	got, ok := m.targetContact()
	if !ok || got.ID != "c-thread" {
		t.Errorf("the thread list targeted %q (%v), not the highlighted thread's contact", got.Name, ok)
	}

	// An open conversation targets its recipient even without the composer.
	m.openThread(*solo)
	if got, ok := m.targetContact(); !ok || got.ID != "c-thread" {
		t.Errorf("the conversation targeted %q (%v), not its recipient", got.Name, ok)
	}

	// The contacts pane is still about its own cursor.
	m.setPane(paneContacts)
	if got, ok := m.selectedContact(); ok {
		if target, _ := m.targetContact(); target.ID != got.ID {
			t.Errorf("the contacts pane targeted %q, not the selected %q", target.Name, got.Name)
		}
	}
}

// A contact saved without a country code is still the contact of a thread
// addressed with one, so Contact theme stays in settings for that thread.
func TestThreadContactMatchesNumbersWrittenDifferently(t *testing.T) {
	m, _, _, _, _ := conversationFixture(t)
	m.contacts = []contacts.Contact{{ID: "c-amy", Name: "Amy", PhoneNumber: "8165550182"}}
	thread := domain.Thread{ID: "t-amy", DisplayName: "Amy", Participants: []domain.Participant{{Number: "+18165550182"}}}
	if got := m.threadContact(thread); got.ID != "c-amy" {
		t.Errorf("the thread's contact was not found across number formats: %+v", got)
	}

	// Two contacts sharing the key are ambiguous, so neither is chosen.
	m.contacts = append(m.contacts, contacts.Contact{ID: "c-other", Name: "Other", PhoneNumber: "1-816-555-0182"})
	if got := m.threadContact(thread); got.ID != "" {
		t.Errorf("an ambiguous number resolved to %q", got.Name)
	}
}
