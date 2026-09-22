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
	"github.com/allisonhere/tidesms/internal/media"
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

// The viewer lists metadata, previews inline only when the terminal can draw,
// and never opens anything without confirmation.
func TestMediaViewerPreviewSaveAndOpen(t *testing.T) {
	path := tempPNG(t)
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	m.graphics = media.Kitty
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

	m.mediaKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	if !m.mediaPreview {
		t.Fatal("preview did not enable")
	}
	if view := m.View(); !strings.Contains(view, "\x1b_G") {
		t.Fatalf("inline image escape missing: %q", view)
	}
	m.mediaPreview = false

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

// Downloading fetches the part through the backend, records it, and enables
// preview, open and save.
func TestMediaDownloadUsesBackend(t *testing.T) {
	path := tempPNG(t)
	m, b, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	b.AttachmentPath = path
	m.graphics = media.Kitty
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
	m.mediaKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	if !m.mediaPreview {
		t.Fatal("preview should work after download")
	}
}

// v in the conversation opens the viewer and, with Kitty and a local file,
// draws the image immediately.
func TestConversationVPreviews(t *testing.T) {
	path := tempPNG(t)
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	openThreadByID(t, d, amyThread)
	d.settle("history", func() bool { return len(m.history.view.Messages) == 3 })
	m.graphics = media.Kitty
	m.history.view.Messages[0].Attachments = []domain.Attachment{{
		ID: "a1", MIMEType: "image/png", Filename: "pic.png", LocalPath: path, State: domain.AttachmentAvailable,
	}}
	m.setPane(paneConversation)
	m.history.view.Selected = 0
	d.press("v")
	if m.modal != "media" {
		t.Fatalf("viewer modal = %q", m.modal)
	}
	if !m.mediaPreview {
		t.Fatal("Kitty with a local file should preview at once")
	}
	if view := m.View(); !strings.Contains(view, "\x1b_G") {
		t.Fatalf("inline image escape missing: %q", view)
	}
}

func TestMediaViewerWithoutLocalCopy(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	m.mediaAtts = []domain.Attachment{{ID: "a1", MIMEType: "image/jpeg", Filename: "x.jpg", State: domain.AttachmentMetadata}}
	m.mediaIndex = 0
	m.modal = "media"
	m.graphics = media.Kitty

	m.mediaKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	if m.mediaPreview {
		t.Fatal("preview should not enable without a local file")
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
