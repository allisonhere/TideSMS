package app

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/allisonhere/tidesms/internal/backend"
	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tidesms/internal/media"
	tea "github.com/charmbracelet/bubbletea"
)

// attachmentOpenedMsg reports the outcome of an external open.
type attachmentOpenedMsg struct{ err error }

// attachmentSavedMsg reports a copy to the download directory.
type attachmentSavedMsg struct {
	path string
	err  error
}

// openMediaViewer shows one message's attachments. Nothing is opened or
// downloaded automatically; the viewer only lists what exists.
func (m *Model) openMediaViewer(msg domain.Message) {
	if len(msg.Attachments) == 0 {
		m.notify("No attachment on this message", true)
		return
	}
	m.mediaAtts = msg.Attachments
	m.mediaIndex = 0
	m.mediaMsgID = msg.ID
	m.modal = "media"
	// When the terminal can draw and the file is already local, show it at
	// once; v returns to the details and actions.
	m.mediaPreview = m.canPreview()
}

// canPreview reports whether the current part can be drawn inline.
func (m *Model) canPreview() bool {
	a, ok := m.currentAttachment()
	if !ok || localFile(a) == "" {
		return false
	}
	return m.graphics == media.Kitty || m.graphics == media.ITerm
}

// localFile returns an attachment's path only when it names a real file. It
// guards against stale metadata, such as a base64 string that was mistakenly
// stored as a path, or a cached file that has since been removed.
func localFile(a domain.Attachment) string {
	if a.LocalPath == "" {
		return ""
	}
	if fi, err := os.Stat(a.LocalPath); err == nil && !fi.IsDir() {
		return a.LocalPath
	}
	return ""
}

func (m *Model) currentAttachment() (domain.Attachment, bool) {
	if m.mediaIndex < 0 || m.mediaIndex >= len(m.mediaAtts) {
		return domain.Attachment{}, false
	}
	return m.mediaAtts[m.mediaIndex], true
}

// mediaKey drives the attachment viewer.
func (m *Model) mediaKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc", "q":
		m.mediaPreview = false
		m.modal = ""
		return nil
	case "left", "h", "p":
		m.mediaIndex = max(0, m.mediaIndex-1)
		m.mediaPreview = false
		return nil
	case "right", "l", "n":
		m.mediaIndex = min(len(m.mediaAtts)-1, m.mediaIndex+1)
		m.mediaPreview = false
		return nil
	case "c":
		a, ok := m.currentAttachment()
		if !ok || localFile(a) == "" {
			m.notify("No local path — press d to download", true)
			return nil
		}
		return m.copyText(localFile(a))
	case "o":
		a, ok := m.currentAttachment()
		if !ok || localFile(a) == "" {
			m.notify("No local copy to open — press d to download", true)
			return nil
		}
		// Never open without an explicit confirmation.
		m.pendingOpenPath = localFile(a)
		m.modal = "open-attachment"
		m.choice = 0
		m.choices = []string{"Open externally", "Cancel"}
		return nil
	case "s":
		return m.saveAttachment()
	case "d":
		return m.fetchAttachment()
	case "v":
		m.toggleMediaPreview()
	}
	return nil
}

// attachmentFetchedMsg reports a downloaded attachment.
type attachmentFetchedMsg struct {
	id   string
	path string
	err  error
}

// fetchAttachment asks the backend for the file behind the current part, then
// records it so preview, open and save become available.
func (m *Model) fetchAttachment() tea.Cmd {
	a, ok := m.currentAttachment()
	if !ok {
		return nil
	}
	if localFile(a) != "" {
		m.notify("Already downloaded", false)
		return nil
	}
	b, ok := m.backend.(backend.AttachmentBackend)
	if !ok {
		m.notify("This backend cannot fetch attachments", true)
		return nil
	}
	ctx := m.ctx
	device := m.deviceID
	id := a.ID
	partID := a.PartID
	uid := a.RemoteID
	m.notify("Downloading attachment…", false)
	return func() tea.Msg {
		path, err := b.FetchAttachment(ctx, device, partID, uid)
		return attachmentFetchedMsg{id: id, path: path, err: err}
	}
}

// applyFetchedAttachment records a downloaded part in memory and on disk.
func (m *Model) applyFetchedAttachment(v attachmentFetchedMsg) tea.Cmd {
	if v.err != nil || v.path == "" {
		m.notify("Could not fetch the attachment", true)
		return nil
	}
	for i := range m.mediaAtts {
		if m.mediaAtts[i].ID == v.id {
			m.mediaAtts[i].LocalPath = v.path
			m.mediaAtts[i].State = domain.AttachmentAvailable
			if w, h, ok := media.ImageSize(v.path); ok {
				m.mediaAtts[i].Width, m.mediaAtts[i].Height = w, h
			}
		}
	}
	m.notify("Attachment ready", false)
	if s, ok := m.store.(interface {
		SetAttachmentState(string, domain.AttachmentState, string) error
	}); ok {
		return func() tea.Msg {
			_ = s.SetAttachmentState(v.id, domain.AttachmentAvailable, v.path)
			return nil
		}
	}
	return nil
}

func (m *Model) toggleMediaPreview() {
	a, ok := m.currentAttachment()
	if !ok {
		return
	}
	if localFile(a) == "" {
		m.notify("No local copy to preview — press d to download", true)
		return
	}
	if m.graphics == media.None || m.graphics == media.Sixel {
		m.notify("This terminal cannot draw images; open externally instead", true)
		return
	}
	m.mediaPreview = !m.mediaPreview
}

// renderMediaFullscreen draws the image with the terminal's graphics protocol.
// It replaces the whole view rather than composing into a TideUI panel, so the
// escape sequence is never measured as text.
func (m *Model) renderMediaFullscreen() string {
	a, ok := m.currentAttachment()
	if !ok {
		return ""
	}
	cols := max(20, min(80, m.width-8))
	rows := max(6, min(24, m.height-6))
	seq, ok := media.Render(m.graphics, localFile(a), cols, rows)
	if !ok {
		m.mediaPreview = false
		return ""
	}
	kind, size := media.Describe(a.MIMEType, a.Filename, a.Size)
	name := a.Filename
	if name == "" {
		name = strings.ToLower(kind)
	}
	return "\x1b[2J\x1b[H" + seq + "\n\n" + name + " · " + size + "\n\nv or Esc back"
}

// mediaViewerLines is the text shown when no image is drawn.
func (m *Model) mediaViewerLines() string {
	a, ok := m.currentAttachment()
	if !ok {
		return ""
	}
	kind, size := media.Describe(a.MIMEType, a.Filename, a.Size)
	name := a.Filename
	if name == "" {
		name = strings.ToLower(kind)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", name)
	meta := kind + " · " + size
	if d := media.Dimensions(a.Width, a.Height); d != "" {
		meta = kind + " · " + d + " · " + size
	}
	fmt.Fprintf(&b, "%s\n", meta)
	fmt.Fprintf(&b, "State: %s\n", a.State)
	local := localFile(a)
	if local != "" {
		fmt.Fprintf(&b, "File: %s\n", local)
	} else {
		b.WriteString("No local copy: press d to download\n")
	}
	if m.graphics != media.None && m.graphics != media.Sixel && local != "" {
		b.WriteString("v previews inline\n")
	}
	return b.String()
}

func (m *Model) mediaTitle() string {
	return fmt.Sprintf("Attachment %d / %d", m.mediaIndex+1, len(m.mediaAtts))
}

// resolveOpenAttachment handles the confirmation.
func (m *Model) resolveOpenAttachment(choice string) tea.Cmd {
	path := m.pendingOpenPath
	m.pendingOpenPath = ""
	m.modal = "media"
	if choice != "Open externally" || path == "" {
		return nil
	}
	return openExternally(path)
}

// openExternally hands the file to the desktop's opener, releasing the terminal
// while the external program runs.
func openExternally(path string) tea.Cmd {
	cmd := exec.Command("xdg-open", path)
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return attachmentOpenedMsg{err: err} })
}

// saveAttachment copies a locally available part to the download directory.
// Files that are metadata-only are not fetched here.
func (m *Model) saveAttachment() tea.Cmd {
	a, ok := m.currentAttachment()
	src := ""
	if ok {
		src = localFile(a)
	}
	if src == "" {
		m.notify("No local copy to save — press d to download", true)
		return nil
	}
	name := a.Filename
	if name == "" {
		name = filepath.Base(src)
	}
	return func() tea.Msg {
		dst, err := copyToDownloads(src, name)
		return attachmentSavedMsg{path: dst, err: err}
	}
}

// copyToDownloads writes src into the user's download directory (XDG if set),
// never overwriting: a numeric suffix is added on collision.
func copyToDownloads(src, name string) (string, error) {
	dir := os.Getenv("XDG_DOWNLOAD_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, "Downloads")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	dst := uniquePath(dir, name)
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	return dst, nil
}

func uniquePath(dir, name string) string {
	candidate := filepath.Join(dir, name)
	for i := 1; ; i++ {
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
		ext := filepath.Ext(name)
		base := strings.TrimSuffix(name, ext)
		candidate = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
	}
}
