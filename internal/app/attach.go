package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tidesms/internal/media"
	tea "github.com/charmbracelet/bubbletea"
)

// Pictures are attached to a conversation's draft, as its text is: switching
// threads keeps each one's pictures with it. They are held in memory, not
// saved with the draft, so a restart forgets them; the prepared copies stay in
// the cache.

// maxAttachments keeps a message within what MMS carries comfortably.
const maxAttachments = 5

// attachments are the pictures waiting in the open conversation's draft.
func (m *Model) attachments() []media.Outgoing { return m.attached[m.draftKey()] }

// attachedMsg reports a picture prepared for the draft it was chosen for.
type attachedMsg struct {
	key string
	out media.Outgoing
	err error
}

// pasteCheckMsg reports whether a Ctrl+V should attach a picture; when it
// should not, the key goes on to the editor as an ordinary paste.
type pasteCheckMsg struct {
	epoch   uint64
	key     tea.KeyMsg
	picture bool
}

// recentPicturesMsg delivers the picker's starting list.
type recentPicturesMsg struct {
	seq   int
	found []media.Candidate
}

// canAttach says why a picture cannot be attached here, or "" when it can.
func (m *Model) canAttach() string {
	if m.recipient.PhoneNumber == "" && m.history.active == nil {
		return "Choose a conversation before attaching a picture"
	}
	if t := m.history.active; t != nil && t.IsGroup {
		return "Group chats are read-only here, so pictures can't be sent to them"
	}
	if d := m.currentDevice(); d != nil && d.SMSCapability == "available" && !d.Capabilities.SendMedia {
		return "This phone can't be sent pictures through KDE Connect"
	}
	if len(m.attachments()) >= maxAttachments {
		return fmt.Sprintf("A message can carry %d pictures at most", maxAttachments)
	}
	return ""
}

// attachFile prepares a picture off the render path and attaches it to the
// draft that was open when it was chosen.
func (m *Model) attachFile(path string) tea.Cmd {
	if why := m.canAttach(); why != "" {
		m.notify(why, true)
		return nil
	}
	key := m.draftKey()
	m.notify("Preparing "+filepath.Base(path)+"…", false)
	return func() tea.Msg {
		out, err := media.Prepare(path)
		return attachedMsg{key: key, out: out, err: err}
	}
}

// attachClipboard attaches the picture on the clipboard.
func (m *Model) attachClipboard() tea.Cmd {
	if why := m.canAttach(); why != "" {
		m.notify(why, true)
		return nil
	}
	key := m.draftKey()
	ctx := m.ctx
	return func() tea.Msg {
		out, err := media.ClipboardPicture(ctx)
		return attachedMsg{key: key, out: out, err: err}
	}
}

// checkPaste decides, off the render path, whether Ctrl+V is a picture.
func (m *Model) checkPaste(k tea.KeyMsg) tea.Cmd {
	epoch, ctx := m.editorEpoch, m.ctx
	return func() tea.Msg {
		return pasteCheckMsg{epoch: epoch, key: k, picture: media.ClipboardHasOnlyPicture(ctx)}
	}
}

func (m *Model) handleAttached(v attachedMsg) tea.Cmd {
	if v.err != nil {
		m.notify(sentence(v.err.Error()), true)
		return nil
	}
	list := m.attached[v.key]
	for _, a := range list {
		if a.Path == v.out.Path {
			m.notify(v.out.Name+" is already attached", false)
			return nil
		}
	}
	if len(list) >= maxAttachments {
		m.notify(fmt.Sprintf("A message can carry %d pictures at most", maxAttachments), true)
		return nil
	}
	m.attached[v.key] = append(list, v.out)
	m.notify("Attached "+v.out.Name+" · "+media.HumanSize(v.out.Size), false)
	m.layoutConversation()
	return nil
}

// removeAttachment drops the newest picture from the open draft.
func (m *Model) removeAttachment() bool {
	key := m.draftKey()
	list := m.attached[key]
	if len(list) == 0 {
		return false
	}
	last := list[len(list)-1]
	m.attached[key] = list[:len(list)-1]
	m.notify("Removed "+last.Name, false)
	m.layoutConversation()
	return true
}

// restoreAttachments puts a failed message's pictures back in the draft for a
// retry, as its text is.
func (m *Model) restoreAttachments(msg domain.Message) {
	var list []media.Outgoing
	for _, a := range msg.Attachments {
		if a.LocalPath == "" {
			continue
		}
		if _, err := os.Stat(a.LocalPath); err != nil {
			continue
		}
		list = append(list, media.Outgoing{Path: a.LocalPath, Name: a.Filename, MIME: a.MIMEType, Size: a.Size, Width: a.Width, Height: a.Height})
	}
	if len(list) > 0 {
		m.attached[m.draftKey()] = list
		m.layoutConversation()
	}
}

// outgoingParts turns the draft's pictures into the parts of the message
// shown while it sends, drawn from the prepared copies.
func outgoingParts(id string, list []media.Outgoing) []domain.Attachment {
	out := make([]domain.Attachment, 0, len(list))
	for i, a := range list {
		out = append(out, domain.Attachment{ID: fmt.Sprintf("%s-%d", id, i), MIMEType: a.MIME, Filename: a.Name, Size: a.Size,
			LocalPath: a.Path, Width: a.Width, Height: a.Height, State: domain.AttachmentAvailable})
	}
	return out
}

// sameAttachments reports whether the draft still holds exactly the pictures
// that were sent, so they are cleared only when nothing was added meanwhile.
func sameAttachments(list []media.Outgoing, sent []domain.Attachment) bool {
	if len(list) != len(sent) {
		return false
	}
	for i := range list {
		if list[i].Path != sent[i].LocalPath {
			return false
		}
	}
	return true
}

// attachLine is the row above the composer naming the attached pictures, or
// "" when there are none.
func (m *Model) attachLine() string {
	list := m.attachments()
	if len(list) == 0 {
		return ""
	}
	names := make([]string, 0, len(list))
	for _, a := range list {
		names = append(names, a.Name+" ("+media.HumanSize(a.Size)+")")
	}
	return "Attached: " + strings.Join(names, ", ") + " · Backspace removes"
}

func attachLines(m *Model) int {
	if m.attachLine() == "" {
		return 0
	}
	return 1
}

// The picker lists recent pictures, filtered by what is typed; a typed path
// (starting with / or ~) lists that folder instead, and Tab completes it.

func (m *Model) openAttachPicker() tea.Cmd {
	if why := m.canAttach(); why != "" {
		m.notify(why, true)
		return nil
	}
	m.modal = "attach"
	m.choice = 0
	m.filter.SetValue("")
	m.attachRecent = nil
	m.attachCands = nil
	m.choices = nil
	m.attachSeq++
	seq := m.attachSeq
	home, _ := os.UserHomeDir()
	return tea.Batch(m.filter.Focus(), func() tea.Msg {
		return recentPicturesMsg{seq: seq, found: media.RecentPictures(home, 300)}
	})
}

// refreshAttach rebuilds the picker's list for what has been typed.
func (m *Model) refreshAttach() {
	query := strings.TrimSpace(m.filter.Value())
	home, _ := os.UserHomeDir()
	if strings.HasPrefix(query, "/") || strings.HasPrefix(query, "~") || strings.HasPrefix(query, "./") {
		m.attachCands = media.Complete(query, home)
	} else {
		m.attachCands = nil
		q := strings.ToLower(query)
		for _, c := range m.attachRecent {
			if q == "" || strings.Contains(strings.ToLower(c.Path), q) {
				m.attachCands = append(m.attachCands, c)
			}
		}
	}
	m.choices = make([]string, 0, len(m.attachCands))
	for _, c := range m.attachCands {
		m.choices = append(m.choices, candidateLabel(c, home))
	}
	m.choice = min(m.choice, max(0, len(m.choices)-1))
}

func candidateLabel(c media.Candidate, home string) string {
	if strings.HasSuffix(c.Path, string(filepath.Separator)) {
		return filepath.Base(c.Path) + "/"
	}
	dir := filepath.Dir(c.Path)
	if home != "" && strings.HasPrefix(dir, home) {
		dir = "~" + strings.TrimPrefix(dir, home)
	}
	return filepath.Base(c.Path) + " · " + dir + " · " + media.HumanSize(c.Size) + " · " + age(c.Modified)
}

// age is how long ago, briefly.
func age(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
	return t.Format("Jan 2006")
}

// attachKey handles a key in the picker.
func (m *Model) attachKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		m.modal = ""
		m.filter.Blur()
		return nil
	case "up", "ctrl+k":
		m.choice = max(0, m.choice-1)
		return nil
	case "down", "ctrl+j":
		m.choice = min(max(0, len(m.choices)-1), m.choice+1)
		return nil
	case "tab":
		// Complete to the highlighted entry: a folder opens, a picture fills in.
		if m.choice < len(m.attachCands) {
			m.filter.SetValue(m.homeRelative(m.attachCands[m.choice].Path))
			m.filter.CursorEnd()
			m.choice = 0
			m.refreshAttach()
		}
		return nil
	case "enter":
		if m.choice >= len(m.attachCands) {
			// A typed path that lists nothing may still be a picture.
			if p := m.expandPath(strings.TrimSpace(m.filter.Value())); p != "" {
				m.modal = ""
				return m.attachFile(p)
			}
			return nil
		}
		c := m.attachCands[m.choice]
		if strings.HasSuffix(c.Path, string(filepath.Separator)) {
			m.filter.SetValue(m.homeRelative(c.Path))
			m.filter.CursorEnd()
			m.choice = 0
			m.refreshAttach()
			return nil
		}
		m.modal = ""
		m.filter.Blur()
		return m.attachFile(c.Path)
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(k)
	m.choice = 0
	m.refreshAttach()
	return cmd
}

func (m *Model) homeRelative(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

func (m *Model) expandPath(p string) string {
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		p = home + p[1:]
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return ""
	}
	return abs
}

// attachPreview is a small drawing of the highlighted picture, cached by path
// because decoding a photo on every key press would make the picker sluggish.
func (m *Model) attachPreview(cols, rows int) []string {
	if m.choice >= len(m.attachCands) {
		return nil
	}
	path := m.attachCands[m.choice].Path
	if strings.HasSuffix(path, string(filepath.Separator)) {
		return nil
	}
	key := fmt.Sprintf("%s|%dx%d", path, cols, rows)
	if lines, ok := m.attachPreviews[key]; ok {
		return lines
	}
	lines, _ := media.BrailleImage(path, cols, rows)
	if len(m.attachPreviews) > 64 {
		m.attachPreviews = map[string][]string{}
	}
	m.attachPreviews[key] = lines
	return lines
}

// sentence capitalises an error for display.
func sentence(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
