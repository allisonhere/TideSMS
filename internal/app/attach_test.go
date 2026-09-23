package app

import (
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tidesms/internal/media"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// picture writes a small PNG and keeps prepared copies out of the real cache.
func picture(t *testing.T, name string) string {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, 8, 6))); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// key sends one key through the model and runs what it starts.
func key(m *Model, d *driver, k tea.KeyMsg) {
	_, cmd := m.Update(k)
	d.run(cmd)
}

// attachTo opens Amy's thread in the composer and attaches a picture.
func attachTo(t *testing.T) (*Model, *driver, func() []string) {
	t.Helper()
	m, b, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	m.setPane(paneComposer)
	d.run(m.attachFile(picture(t, "trail.png")))
	if len(m.attachments()) != 1 {
		t.Fatalf("not attached: %q", m.notice)
	}
	sent := func() []string {
		if len(b.Sent) == 0 {
			return nil
		}
		return b.Sent[len(b.Sent)-1].Attachments
	}
	return m, d, sent
}

// A picture can be sent with no text. The phone is given the prepared copy,
// the conversation shows it straight away, and the draft is cleared after.
func TestSendAPictureOnItsOwn(t *testing.T) {
	m, d, sent := attachTo(t)
	if view := ansi.Strip(m.View()); !strings.Contains(view, "Attached: trail.png") {
		t.Fatalf("the composer does not show the picture:\n%s", view)
	}
	path := m.attachments()[0].Path
	d.run(m.send())
	d.settle("send", func() bool { return !m.sending && len(sent()) == 1 })
	if sent()[0] != path {
		t.Fatalf("sent %q, want the prepared copy %q", sent(), path)
	}
	var shown *domain.Message
	for i := range m.history.view.Messages {
		if msg := m.history.view.Messages[i]; msg.Direction == domain.Outgoing && len(msg.Attachments) == 1 {
			shown = &m.history.view.Messages[i]
		}
	}
	if shown == nil || shown.Attachments[0].LocalPath != path || shown.Attachments[0].State != domain.AttachmentAvailable {
		t.Fatalf("the sent picture is not in the conversation: %+v", shown)
	}
	if len(m.attachments()) != 0 {
		t.Error("the picture stayed attached after sending")
	}
}

// Backspace in an empty composer takes the picture back; with text, it edits.
func TestBackspaceRemovesThePicture(t *testing.T) {
	m, d, _ := attachTo(t)
	typeText(m, "hi")
	key(m, d, tea.KeyMsg{Type: tea.KeyBackspace})
	if len(m.attachments()) != 1 || m.editor.Value() != "h" {
		t.Fatalf("backspace with text: %d pictures, %q", len(m.attachments()), m.editor.Value())
	}
	key(m, d, tea.KeyMsg{Type: tea.KeyBackspace})
	key(m, d, tea.KeyMsg{Type: tea.KeyBackspace})
	if len(m.attachments()) != 0 {
		t.Fatal("backspace in an empty composer kept the picture")
	}
}

// Pictures belong to their conversation's draft, as text does.
func TestPicturesStayWithTheirConversation(t *testing.T) {
	m, d, _ := attachTo(t)
	openThreadByID(t, d, unknownThread)
	if len(m.attachments()) != 0 {
		t.Fatal("the picture followed to another conversation")
	}
	openThreadByID(t, d, amyThread)
	if len(m.attachments()) != 1 {
		t.Fatal("the picture was lost on coming back")
	}
}

// The queue and the scheduler hold text only, so a picture is refused there
// rather than silently dropped, and stays attached.
func TestPicturesAreNotQueuedOrScheduled(t *testing.T) {
	m, d, sent := attachTo(t)
	if m.openSchedule(); m.modal == "schedule" || !strings.Contains(m.notice, "can't be scheduled") {
		t.Fatalf("scheduling a picture: modal %q, notice %q", m.modal, m.notice)
	}
	m.devices[0].Connected = false
	d.run(m.sendHistory())
	if m.modal == "offline-send" || !strings.Contains(m.notice, "can't be queued") || len(sent()) != 0 {
		t.Fatalf("offline picture: modal %q, notice %q, sent %v", m.modal, m.notice, sent())
	}
	if len(m.attachments()) != 1 {
		t.Error("a refused picture was dropped")
	}
}

// A group cannot be sent anything, so nothing can be attached to it.
func TestNoPicturesForGroups(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, familyThread)
	if cmd := m.openAttachPicker(); cmd != nil || m.modal == "attach" || !strings.Contains(m.notice, "Group") {
		t.Fatalf("group picker: modal %q, notice %q", m.modal, m.notice)
	}
}

// A failed picture message comes back with its picture for a retry.
func TestRetryBringsThePictureBack(t *testing.T) {
	m, b, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	m.setPane(paneComposer)
	d.run(m.attachFile(picture(t, "trail.png")))
	b.SendError = errors.New("phone disconnected")
	d.run(m.send())
	d.settle("failure", func() bool { return !m.sending })
	b.SendError = nil
	if len(m.attachments()) != 1 {
		t.Fatal("a failed send cleared the picture")
	}
	m.attached = map[string][]media.Outgoing{}
	m.setPane(paneConversation)
	d.press("G")
	if cur := m.history.view.Current(); cur == nil || cur.Status != domain.Failed {
		t.Fatalf("failed message not selected: %+v", cur)
	}
	d.press("r")
	if len(m.attachments()) != 1 {
		t.Fatal("retry did not restore the picture")
	}
}

// The picker lists recent pictures and attaches the chosen one.
func TestPickerAttachesARecentPicture(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	home := t.TempDir()
	t.Setenv("HOME", home)
	src := picture(t, "unused.png")
	dest := filepath.Join(home, "Pictures", "Screenshots", "shot.png")
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(src)
	if err := os.WriteFile(dest, data, 0o600); err != nil {
		t.Fatal(err)
	}
	key(m, d, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a"), Alt: true})
	d.settle("recent pictures", func() bool { return len(m.choices) > 0 })
	if m.modal != "attach" || !strings.Contains(m.choices[0], "shot.png") {
		t.Fatalf("picker: modal %q, choices %q", m.modal, m.choices)
	}
	key(m, d, tea.KeyMsg{Type: tea.KeyEnter})
	d.settle("attach", func() bool { return len(m.attachments()) == 1 })
	if m.modal != "" || m.attachments()[0].Name != "shot.png" {
		t.Fatalf("after Enter: modal %q, attached %+v", m.modal, m.attachments())
	}
}
