package app

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/allisonhere/tidesms/internal/domain"
	tea "github.com/charmbracelet/bubbletea"
)

func tempPNG(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	img.Set(0, 0, color.RGBA{G: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "pic.png")
	if err := os.WriteFile(path, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The viewer lists metadata, and never opens anything without confirmation.
func TestMediaViewerSaveAndOpen(t *testing.T) {
	path := tempPNG(t)
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	m.mediaAtts = []domain.Attachment{{
		ID: "a1", MIMEType: "image/png", Filename: "pic.png", Size: 1234,
		LocalPath: path, State: domain.AttachmentAvailable,
	}}
	m.mediaIndex = 0
	m.modal = "media"

	body := m.mediaViewerLines()
	if !strings.Contains(body, "pic.png") || !strings.Contains(body, "Image") || !strings.Contains(body, "State: available") {
		t.Fatalf("viewer body:\n%s", body)
	}

	// v launches the suspended external viewer, which the tests must not run.
	if cmd := m.mediaKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")}); cmd == nil {
		t.Fatal("v should launch the external viewer for a local image")
	}
	if m.modal != "media" {
		t.Fatalf("v should not change the modal: %q", m.modal)
	}

	// Opening requires explicit confirmation.
	m.mediaKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if m.modal != "open-attachment" || m.pendingOpenPath != path {
		t.Fatalf("open confirmation not raised: modal=%q path=%q", m.modal, m.pendingOpenPath)
	}
	m.resolveOpenAttachment("Cancel")
	if m.modal != "media" || m.pendingOpenPath != "" {
		t.Fatal("cancel did not return to the viewer")
	}

	// Save copies the file into the download directory.
	dir := t.TempDir()
	t.Setenv("XDG_DOWNLOAD_DIR", dir)
	saved, ok := m.saveAttachment()().(attachmentSavedMsg)
	if !ok || saved.err != nil {
		t.Fatalf("save = %+v", saved)
	}
	if _, err := os.Stat(saved.path); err != nil {
		t.Fatalf("saved file missing: %v", err)
	}
}

// Downloading fetches the part through the backend and records it.
func TestMediaDownloadUsesBackend(t *testing.T) {
	path := tempPNG(t)
	m, b, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	b.AttachmentPath = path
	m.mediaAtts = []domain.Attachment{{ID: "a1", MIMEType: "image/png", Filename: "pic.png", PartID: 12, RemoteID: "PART_x", State: domain.AttachmentMetadata}}
	m.mediaIndex = 0
	m.modal = "media"

	cmd := m.fetchAttachment()
	if cmd == nil {
		t.Fatal("download produced no command")
	}
	if _, next := m.Update(cmd()); next != nil {
		next()
	}
	if len(b.AttachmentCalls) != 1 || b.AttachmentCalls[0] != 12 {
		t.Fatalf("backend calls = %v", b.AttachmentCalls)
	}
	if m.mediaAtts[0].State != domain.AttachmentAvailable || m.mediaAtts[0].LocalPath != path {
		t.Fatalf("attachment not updated: %+v", m.mediaAtts[0])
	}
	if m.mediaAtts[0].Width != 4 || m.mediaAtts[0].Height != 2 {
		t.Fatalf("dimensions not read: %+v", m.mediaAtts[0])
	}
	if cmd := m.mediaKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")}); cmd == nil {
		t.Fatal("v should launch the external viewer after download")
	}
}

// v in the conversation launches the image when a local copy exists, and opens
// the metadata viewer otherwise.
func TestConversationVOpensImage(t *testing.T) {
	path := tempPNG(t)
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	d.settle("history", func() bool { return len(m.history.view.Messages) == 3 })
	m.setPane(paneConversation)
	m.history.view.Selected = 0

	// No local copy: the viewer opens so the message can explain d download.
	m.history.view.Messages[0].Attachments = []domain.Attachment{{ID: "a1", MIMEType: "image/png", State: domain.AttachmentMetadata}}
	cmd := m.conversationKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	if cmd != nil || m.modal != "media" {
		t.Fatalf("no local copy should open the viewer: cmd=%v modal=%q", cmd != nil, m.modal)
	}
	m.modal = ""

	// Local copy: launch the external image viewer without a command in tests.
	m.history.view.Messages[0].Attachments = []domain.Attachment{{ID: "a1", MIMEType: "image/png", Filename: "pic.png", LocalPath: path, State: domain.AttachmentAvailable}}
	if cmd := m.conversationKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")}); cmd == nil {
		t.Fatal("v should launch the external viewer for a local image")
	}
	if m.modal != "" {
		t.Fatalf("v should not open the metadata modal: %q", m.modal)
	}
}

func TestMediaViewerWithoutLocalCopy(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	m.mediaAtts = []domain.Attachment{{ID: "a1", MIMEType: "image/jpeg", Filename: "x.jpg", State: domain.AttachmentMetadata}}
	m.mediaIndex = 0
	m.modal = "media"

	if cmd := m.mediaKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")}); cmd != nil {
		t.Fatal("v should do nothing without a local file")
	}
	if cmd := m.mediaKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")}); cmd != nil {
		t.Fatal("open should not run without a local file")
	}
	if m.modal != "media" {
		t.Fatalf("modal = %q", m.modal)
	}
}

// The message modal offers View attachment only when there is media.
func TestMessageActionsOfferAttachment(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	d.settle("history", func() bool { return len(m.history.view.Messages) == 3 })
	m.setPane(paneConversation)
	m.history.view.Selected = 0
	d.press("enter")
	if len(m.choices) == 0 || contains(m.choices, "View attachment") {
		t.Fatalf("no attachment yet should not offer the action: %v", m.choices)
	}
	m.modal = ""
	m.history.view.Messages[0].Attachments = []domain.Attachment{{ID: "a1"}}
	m.history.view.Selected = 0
	d.press("enter")
	if !contains(m.choices, "View attachment") {
		t.Fatalf("attachment action missing: %v", m.choices)
	}
	pickChoice(t, m, "View attachment")
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	if m.modal != "media" {
		t.Fatalf("viewer modal = %q", m.modal)
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
