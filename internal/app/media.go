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

// measureTerminal re-reads what the terminal can draw and how large a cell is.
// Both are needed before an image can be placed in the conversation, and the
// cell size is re-read on every resize because moving a window between displays
// of different scales changes it without changing the column count.
func (m *Model) measureTerminal() {
	m.graphics = media.Detect(os.Getenv)
	if w, h, ok := media.CellPixels(os.Stdout.Fd()); ok {
		m.cellW, m.cellH = w, h
	}
}

// inlineGraphics reports whether the conversation should place real images. The
// braille fallback covers every other terminal, so this only has to be true
// where the protocol genuinely works.
func (m *Model) inlineGraphics() bool {
	return m.cfg.Conversation.InlineMedia && media.Placeholders(m.graphics)
}

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

// thumbFile returns the backend's preview if it is still on disk. Thumbnails
// live in the temporary directory, so one recorded in an earlier run may be
// gone; a missing file is simply no preview.
func thumbFile(a domain.Attachment) string {
	if a.ThumbPath == "" {
		return ""
	}
	if fi, err := os.Stat(a.ThumbPath); err == nil && !fi.IsDir() {
		return a.ThumbPath
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
		m.modal = ""
		return nil
	case "left", "h", "p":
		m.mediaIndex = max(0, m.mediaIndex-1)
		return nil
	case "right", "l", "n":
		m.mediaIndex = min(len(m.mediaAtts)-1, m.mediaIndex+1)
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
		return m.previewAttachment()
	}
	return nil
}

// previewAttachment shows the part at full size. It fetches the real file
// first when only a thumbnail is on disk: the backend's preview is 100x100, so
// opening a full-screen view on it would show a blur, and the point of the
// view is to see the image. The thumbnail is never used as a stand-in here —
// the fetch is cheap, and a view that silently showed the preview instead is
// what hid the real image in the first place.
func (m *Model) previewAttachment() tea.Cmd {
	a, ok := m.currentAttachment()
	if !ok {
		return nil
	}
	if localFile(a) != "" {
		return m.viewLocal(a)
	}
	if _, isAttachmentBackend := m.backend.(backend.AttachmentBackend); !isAttachmentBackend {
		// No way to fetch the part, so the preview is all there is. Better a
		// small image than nothing, as long as it is described as a preview.
		if thumb := thumbFile(a); thumb != "" {
			m.notify("Showing the preview; this backend cannot fetch the full image", true)
			return externalPreview(thumb)
		}
		m.notify("This backend cannot fetch attachments", true)
		return nil
	}
	m.previewAfterFetch = a.ID
	return m.fetchAttachment()
}

// externalPreview suspends the TUI and lets the binary draw the image on the
// real terminal. TideUI panes pad every line, so a raw graphics escape cannot
// survive inside them; a suspended child is the reliable path, and it works
// for Kitty, iTerm2 and a braille fallback alike.
func externalPreview(path string) tea.Cmd {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	cmd := exec.Command(exe, "image", path)
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return attachmentOpenedMsg{err: err} })
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
	// Only the part itself counts as downloaded. A thumbnail sitting in
	// ThumbPath is a preview the backend volunteered, and refusing the fetch
	// because of it would leave the real image permanently out of reach.
	if localFile(a) != "" {
		m.notify("Already downloaded", false)
		m.previewAfterFetch = ""
		return nil
	}
	b, ok := m.backend.(backend.AttachmentBackend)
	if !ok {
		m.notify("This backend cannot fetch attachments", true)
		m.previewAfterFetch = ""
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
		// Convert a photo the app cannot decode while still off the UI
		// thread, so it can be drawn as soon as the download is reported.
		if err == nil && media.NeedsConversion(path) {
			_, _ = media.Convert(path)
		}
		return attachmentFetchedMsg{id: id, path: path, err: err}
	}
}

// applyFetchedAttachment records a downloaded part in memory and on disk.
func (m *Model) applyFetchedAttachment(v attachmentFetchedMsg) tea.Cmd {
	wanted := m.previewAfterFetch == v.id
	m.previewAfterFetch = ""
	if v.err != nil || v.path == "" {
		m.notify("Could not fetch the attachment", true)
		return nil
	}
	for i := range m.mediaAtts {
		if m.mediaAtts[i].ID == v.id {
			m.mediaAtts[i].LocalPath = v.path
			m.mediaAtts[i].State = domain.AttachmentAvailable
			if w, h, ok := media.ImageSize(displayableOr(v.path)); ok {
				m.mediaAtts[i].Width, m.mediaAtts[i].Height = w, h
			}
		}
	}
	m.notify("Attachment ready", false)
	var record tea.Cmd
	if s, ok := m.store.(interface {
		SetAttachmentState(string, domain.AttachmentState, string) error
	}); ok {
		record = func() tea.Msg {
			_ = s.SetAttachmentState(v.id, domain.AttachmentAvailable, v.path)
			return nil
		}
	}
	if !wanted {
		return record
	}
	// v asked for this file. Record the download first, then suspend for the
	// viewer, so the write is not left racing a process that takes the terminal.
	a, _ := m.currentAttachment()
	a.LocalPath = v.path
	view := m.viewLocal(a)
	if record == nil {
		return view
	}
	return tea.Sequence(record, view)
}

// displayableOr is the drawable copy of path, or path itself when there is
// none, for callers that only read an image's size.
func displayableOr(path string) string {
	if p, ok := media.Displayable(path); ok {
		return p
	}
	return path
}

// viewLocal opens a downloaded part full-screen. A photo in a format the app
// cannot decode is shown from its converted copy, converting it first when
// that has not happened yet. When it cannot be shown at all the preview is
// shown instead, or the reason is given; the viewer is never started on a
// file it cannot draw, which used to leave only a flash as it exited at once.
func (m *Model) viewLocal(a domain.Attachment) tea.Cmd {
	if p, ok := media.Displayable(a.LocalPath); ok {
		return externalPreview(p)
	}
	if media.NeedsConversion(a.LocalPath) && !m.convertFailed[a.LocalPath] {
		m.notify("Converting photo…", false)
		return m.convertOne(a.ID, a.LocalPath, true)
	}
	return m.viewFallback(a)
}

// viewFallback shows the backend's preview of a part the app cannot draw.
func (m *Model) viewFallback(a domain.Attachment) tea.Cmd {
	if thumb := thumbFile(a); thumb != "" && media.Readable(thumb) {
		m.notify("Can't display this photo's format; showing the preview", true)
		return externalPreview(thumb)
	}
	// With nothing to draw, the viewer is where the file can still be opened
	// in another app or saved.
	if len(m.mediaAtts) > 0 {
		m.modal = "media"
	}
	m.notify("Can't display this photo's format; o opens it in another app", true)
	return nil
}

// attachmentConvertedMsg reports a photo converted to a format the app draws.
type attachmentConvertedMsg struct {
	id, path string
	err      error
	// view is set when v is waiting to show the result.
	view bool
}

// convertOne converts one downloaded part in the background.
func (m *Model) convertOne(id, path string, view bool) tea.Cmd {
	if m.converting == nil {
		m.converting = map[string]bool{}
	}
	m.converting[path] = true
	return func() tea.Msg {
		_, err := media.Convert(path)
		return attachmentConvertedMsg{id: id, path: path, err: err, view: view}
	}
}

// convertAttachments starts converting every downloaded part among msgs that
// the app cannot draw and has not already tried.
func (m *Model) convertAttachments(msgs []domain.Message) tea.Cmd {
	var cmds []tea.Cmd
	for _, msg := range msgs {
		for _, a := range msg.Attachments {
			p := localFile(a)
			if p == "" || m.converting[p] || m.convertFailed[p] || !media.NeedsConversion(p) {
				continue
			}
			cmds = append(cmds, m.convertOne(a.ID, p, false))
		}
	}
	return tea.Batch(cmds...)
}

// applyConverted redraws the conversation with a converted photo, and shows it
// if v was waiting on it.
func (m *Model) applyConverted(v attachmentConvertedMsg) tea.Cmd {
	delete(m.converting, v.path)
	if v.err != nil {
		if m.convertFailed == nil {
			m.convertFailed = map[string]bool{}
		}
		m.convertFailed[v.path] = true
		m.logError("convert attachment", v.err)
	}
	m.layoutConversation()
	if !v.view {
		return nil
	}
	a, _ := m.currentAttachment()
	if a.ID != v.id {
		a = domain.Attachment{ID: v.id, LocalPath: v.path}
	}
	if p, ok := media.Displayable(v.path); ok {
		m.notify("", false)
		return externalPreview(p)
	}
	return m.viewFallback(a)
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
		b.WriteString("v opens the image\n")
		return b.String()
	}
	// Say plainly that what the conversation is showing is the backend's small
	// preview, not the image, so the size on screen is not mistaken for the
	// size that was sent.
	if thumb := thumbFile(a); thumb != "" {
		meta := "preview only"
		if w, h, ok := media.ImageSize(thumb); ok {
			meta = "preview only · " + media.Dimensions(w, h)
		}
		fmt.Fprintf(&b, "%s\n", meta)
		b.WriteString("v downloads and opens the full image\n")
		return b.String()
	}
	b.WriteString("No local copy: press d to download\n")
	b.WriteString("v downloads and opens the full image\n")
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
