package app

import (
	"context"
	"errors"
	"github.com/allisonhere/tidesms/internal/backend"
	"github.com/allisonhere/tidesms/internal/config"
	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/keys"
	"github.com/allisonhere/tidesms/internal/storage"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
)

type fakeBackend struct {
	err  error
	sent []backend.SendRequest
}

func (f *fakeBackend) Devices(context.Context) ([]backend.Device, error) {
	return []backend.Device{{ID: "phone", Name: "Pixel", Connected: true, SMSCapability: "available"}}, nil
}
func (f *fakeBackend) Send(_ context.Context, r backend.SendRequest) error {
	f.sent = append(f.sent, r)
	return f.err
}
func fixture(t *testing.T) (*Model, *fakeBackend, *storage.Store) {
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
	f := &fakeBackend{}
	m := New(context.Background(), s, f, config.Default(), filepath.Join(dir, "config.toml"), slog.New(slog.NewJSONHandler(io.Discard, nil)), nil)
	m.loaded = true
	m.contacts = []contacts.Contact{{ID: "amy", Name: "Amy", PhoneNumber: "+15551234567", Theme: "rose"}, {ID: "chris", Name: "Chris", PhoneNumber: "+15557654321"}}
	m.devices, _ = f.Devices(context.Background())
	m.deviceID = "phone"
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m, f, s
}
func typeText(m *Model, s string) { m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}) }
func TestDraftSwitchFailureSuccessAndRestart(t *testing.T) {
	m, f, s := fixture(t)
	amy, chris := m.contacts[0], m.contacts[1]
	m.choose(amy)
	typeText(m, "Hello Amy")
	m.choose(chris)
	typeText(m, "Hello Chris")
	m.choose(amy)
	if m.editor.Value() != "Hello Amy" {
		t.Fatal("draft lost switching")
	}
	f.err = errors.New("phone disconnected")
	cmd := m.send()
	if cmd == nil {
		t.Fatal("no send command")
	}
	m.Update(cmd())
	if m.editor.Value() != "Hello Amy" || !m.failed {
		t.Fatal("failed send cleared draft")
	}
	f.err = nil
	cmd = m.send()
	if cmd == nil {
		t.Fatal("no retry")
	}
	_, save := m.Update(cmd())
	if save != nil {
		m.Update(save())
	}
	if m.editor.Value() != "" || m.failed {
		t.Fatal("successful submission did not clear")
	}
	m.choose(chris)
	if err := m.FlushDrafts(); err != nil {
		t.Fatal(err)
	}
	ds, err := s.Drafts()
	if err != nil || ds[chris.PhoneNumber].Body != "Hello Chris" || ds[amy.PhoneNumber].Body != "" {
		t.Fatalf("%v %v", ds, err)
	}
	if len(f.sent) != 2 || f.sent[1].PhoneNumber != amy.PhoneNumber {
		t.Fatalf("wrong sends: %+v", f.sent)
	}
}
func TestSendCompletionDoesNotClearAnotherRecipient(t *testing.T) {
	m, _, _ := fixture(t)
	m.choose(m.contacts[0])
	typeText(m, "Amy")
	cmd := m.send()
	m.choose(m.contacts[1])
	m.editor.SetValue("Chris")
	m.trackChange()
	m.Update(cmd())
	if m.editor.Value() != "Chris" {
		t.Fatal("send cleared another draft")
	}
}
func TestComposerOwnsVimKeysAndShiftEnterNewlines(t *testing.T) {
	m, f, _ := fixture(t)
	m.cfg.Composer.Mode = "vim"
	m.choose(m.contacts[0])
	typeText(m, "i")
	for _, s := range []string{"j", "k", "q", "n", "e", "?"} {
		typeText(m, s)
	}
	m.Update(keys.ActionNewline)
	if m.editor.Value() != "jkqne?\n" || len(f.sent) != 0 || m.modal != "" {
		t.Fatalf("editor=%q modal=%s", m.editor.Value(), m.modal)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.editor.Mode() != "NORMAL" || !m.focus {
		t.Fatal("Esc stolen")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc, Alt: true})
	if m.focus {
		t.Fatal("Alt+Esc failed")
	}
}

// Enter sends; Shift+Enter and Alt+Enter insert a newline. Turning enter_sends
// off restores Enter-as-newline for terminals that cannot tell them apart.
func TestEnterSendsAndShiftEnterNewlines(t *testing.T) {
	m, f, _ := fixture(t)
	m.choose(m.contacts[0])
	typeText(m, "hello")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter produced no send")
	}
	m.Update(cmd())
	if len(f.sent) != 1 || f.sent[0].Message != "hello" {
		t.Fatalf("sent = %+v", f.sent)
	}

	m.cfg.Composer.EnterSends = false
	m.choose(m.contacts[1])
	typeText(m, "line one")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.editor.Value() != "line one\n" {
		t.Fatalf("enter_sends off should newline: %q", m.editor.Value())
	}
	// Alt+Enter sends again in that mode.
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if cmd == nil {
		t.Fatal("Alt+Enter should send when enter_sends is off")
	}
}

// In normal editing Esc has no editor meaning, so it leaves the composer and
// keeps the draft; Vim keeps Esc for its own modes and leaves on a second one.
func TestEscapeLeavesComposer(t *testing.T) {
	m, _, _ := fixture(t)
	m.choose(m.contacts[0])
	typeText(m, "half-written")
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.focus {
		t.Fatal("Esc did not leave the composer")
	}
	if m.editor.Value() != "half-written" {
		t.Fatalf("Esc discarded the draft: %q", m.editor.Value())
	}

	m.cfg.Composer.Mode = "vim"
	m.editor.SetMode("vim")
	m.choose(m.contacts[0])
	typeText(m, "i")
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.focus || m.editor.Mode() != "NORMAL" {
		t.Fatalf("first Esc should reach Vim's Normal mode: focus=%v mode=%s", m.focus, m.editor.Mode())
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	for i := 0; cmd != nil && m.focus && i < 5; i++ {
		_, cmd = m.Update(cmd())
	}
	if m.focus {
		t.Fatal("second Esc did not leave the composer")
	}
}

func TestPaletteSearchAndForm(t *testing.T) {
	m, _, _ := fixture(t)
	m.openPalette()
	typeText(m, "add")
	if len(m.choices) != 1 || m.choices[0] != "Add contact" {
		t.Fatalf("%v", m.choices)
	}
	m.modalKey(tea.KeyMsg{Type: tea.KeyEnter})
	typeText(m, "Mom")
	m.modalKey(tea.KeyMsg{Type: tea.KeyEnter})
	typeText(m, "+1 (555) 987-6543")
	cmd := m.modalKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("no save")
	}
	m.Update(cmd())
	if m.recipient.Name != "Mom" || m.recipient.PhoneNumber != "+15559876543" {
		t.Fatalf("%+v", m.recipient)
	}
}
func TestViewsBoundedAndFooterVisible(t *testing.T) {
	m, _, _ := fixture(t)
	m.choose(m.contacts[0])
	typeText(m, strings.Repeat("Hello 界 👩‍💻 ", 80))
	for _, size := range [][2]int{{100, 30}, {80, 24}, {54, 16}, {20, 8}, {1, 1}, {0, 0}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for _, modal := range []string{"", "help", "palette", "devices", "settings", "themes", "compose"} {
			m.modal = modal
			m.choices = commands
			view := m.View()
			if size[0] == 0 && view != "" {
				t.Fatal("zero window")
			}
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
			if modal == "" && size[0] >= 80 && !strings.Contains(ansi.Strip(view), "Shift+Enter") {
				t.Fatalf("send footer clipped at %v:\n%s", size, ansi.Strip(view))
			}
		}
	}
}
func TestEmptyAndOfflineSendValidation(t *testing.T) {
	m, f, _ := fixture(t)
	if m.send() != nil {
		t.Fatal("sent without recipient")
	}
	m.choose(m.contacts[0])
	if m.send() != nil {
		t.Fatal("sent empty")
	}
	typeText(m, "hello")
	m.devices[0].Connected = false
	if m.send() != nil || len(f.sent) != 0 {
		t.Fatal("sent offline")
	}
}

// Enter submits the draft and does not reach the editor as a newline.
func TestEnterSendsWithoutTouchingTheEditor(t *testing.T) {
	m, f, _ := fixture(t)
	m.choose(m.contacts[0])
	typeText(m, "ready")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter did not send")
	}
	if m.editor.Value() != "ready" {
		t.Fatalf("Enter edited the message: %q", m.editor.Value())
	}
	_, save := m.Update(cmd())
	if save != nil {
		m.Update(save())
	}
	if len(f.sent) != 1 || f.sent[0].Message != "ready" {
		t.Fatalf("%+v", f.sent)
	}
	if m.editor.Value() != "" {
		t.Fatalf("composer not cleared: %q", m.editor.Value())
	}
}
