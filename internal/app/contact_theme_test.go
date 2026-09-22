package app

import (
	"testing"

	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/domain"
)

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

// The settings panel edits whichever contact is selected, which need not be
// the one on screen. Cycling one person's theme must not repaint another
// person's conversation with a colour that will never be applied to it.
func TestContactThemePreviewStaysOnItsOwnConversation(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	d.settle("history", func() bool { return len(m.history.view.Messages) > 0 })

	before := m.conversationTheme().Name

	// Edit somebody who is not the open conversation's recipient.
	m.setPane(paneContacts)
	m.contacts = append(m.contacts, contacts.Contact{
		ID: "c-other", Name: "Someone Else", PhoneNumber: "+15559998888",
	})
	m.selected = len(m.filtered()) - 1
	other, ok := m.selectedContact()
	if !ok || other.PhoneNumber == m.recipient.PhoneNumber {
		t.Skip("could not select a contact other than the recipient")
	}

	d.run(m.action("Open settings"))
	selectSetting(t, m, "Contact theme")
	for i := 0; i < 3; i++ {
		m.settingsAdjust(1)
	}
	if _, previewing := m.settingsContactPreview(); previewing {
		t.Error("a preview was offered for a contact whose conversation is not open")
	}
	if after := m.conversationTheme().Name; after != before {
		t.Errorf("the open conversation repainted from another contact's preview: %q -> %q", before, after)
	}
}

// The preview still works where it belongs: on the conversation of the contact
// actually being edited.
func TestContactThemePreviewAppliesToItsOwnConversation(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	d.settle("history", func() bool { return len(m.history.view.Messages) > 0 })
	if m.recipient.PhoneNumber == "" {
		t.Skip("fixture opened no recipient")
	}

	// Select the recipient themselves, as pressing t on them would.
	m.setPane(paneContacts)
	for i, c := range m.filtered() {
		if c.PhoneNumber == m.recipient.PhoneNumber {
			m.selected = i
		}
	}
	if c, ok := m.settingsContact(); !ok || c.PhoneNumber != m.recipient.PhoneNumber {
		t.Skip("the recipient is not selectable in this fixture")
	}

	d.run(m.action("Open settings"))
	selectSetting(t, m, "Contact theme")
	m.settingsAdjust(1)
	preview, ok := m.settingsContactPreview()
	if !ok {
		t.Fatal("no preview offered for the conversation's own contact")
	}
	if preview != "" && m.conversationTheme().Name != preview {
		t.Errorf("the preview did not reach its own conversation: want %q, got %q", preview, m.conversationTheme().Name)
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
