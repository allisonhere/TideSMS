// Package conversation owns message selection, wrapping and viewport state.
package conversation

import (
	"fmt"
	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tidesms/internal/media"
	"github.com/allisonhere/tidesms/internal/themes"
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"maps"
	"os"
	"reflect"
	"strings"
	"time"
	"unicode"
)

type Options struct {
	Dates bool
	// MaxWidth caps how wide a message may grow; zero uses the pane. A reader
	// who prefers a narrow measure on a very wide terminal can set it.
	MaxWidth int
	// Bubbles draws a frame around each message instead of a single gutter bar.
	Bubbles bool
	// Corners is "round" or "square"; anything else is treated as round.
	Corners string
	// Fill paints the inside of a frame on a raised surface colour.
	Fill bool
	// Names resolves a sender's number to their display name, keyed by both the
	// normalized number and its national portion. Without it every incoming
	// message is labelled with a bare number even when the contact is known.
	Names map[string]string
	// Incoming and Outgoing are the surfaces for the two directions. A zero
	// value derives the surface from the renderer's theme.
	Incoming, Outgoing themes.Bubble
	// HighlightID marks a message the user jumped to, so it stands out briefly
	// from the messages around it. It styles only the sender line.
	HighlightID string
	// InlineMedia draws a local image inside the message instead of a text
	// block, as braille art unless Graphics is also set.
	InlineMedia bool
	// Graphics draws a local image with the terminal's own graphics protocol
	// rather than as text. The drawn cells are ordinary text, so the pane still
	// pads, truncates and scrolls them, but each frame must be accompanied by
	// the escapes Transmissions reports.
	Graphics bool
	// CellWidth and CellHeight are the terminal's cell size in pixels, which is
	// what gives a placed image its own proportions. Zero assumes a cell twice
	// as tall as it is wide.
	CellWidth, CellHeight int
	Timestamps, Query     string
	// Group names the sender of every incoming message. One-to-one, the
	// conversation's title already does, so those carry only the time.
	Group bool
}
type mediaStamp struct {
	path     string
	size     int64
	modified int64
}

type imageTransmission struct {
	message int
	data    string
}

type renderedMessage struct {
	message       domain.Message
	shape         shape
	lines         []string
	media         []mediaStamp
	transmissions []string
}

// shape is the part of a message's drawing that its neighbours decide: whether
// it opens a run with a sender line, carries its own status, and is followed by
// a gap. It is part of the cache key, since a new message can change all three.
type shape struct{ header, status, spacer bool }

// runGap is how close together two messages from the same sender must be for
// the second to continue the first's run without a header of its own.
const runGap = 5 * time.Minute

// continues reports whether msg joins prev's run: same sender and direction,
// same day, and sent soon after.
func continues(prev, msg domain.Message) bool {
	if prev.Direction != msg.Direction || prev.Sender != msg.Sender {
		return false
	}
	if prev.Timestamp.Local().Format("2006-01-02") != msg.Timestamp.Local().Format("2006-01-02") {
		return false
	}
	gap := msg.Timestamp.Sub(prev.Timestamp)
	return gap >= 0 && gap <= runGap
}

// settled statuses need not be repeated on every message: the newest outgoing
// message shows where things stand, and anything unsettled shows its own.
func settled(s domain.Status) bool {
	switch s {
	case domain.Sent, domain.Submitted, domain.Delivered, domain.Read:
		return true
	}
	return false
}

// shapes decides each message's shape from its neighbours.
func (m *Model) shapes() []shape {
	n := len(m.Messages)
	cont := make([]bool, n)
	lastOut := -1
	unread := false
	for i, msg := range m.Messages {
		// The unread boundary is drawn above the first unread message, and a
		// run never continues across it.
		boundary := msg.Unread && !unread
		unread = unread || msg.Unread
		cont[i] = i > 0 && !boundary && continues(m.Messages[i-1], msg)
		if msg.Direction == domain.Outgoing {
			lastOut = i
		}
	}
	out := make([]shape, n)
	for i, msg := range m.Messages {
		highlighted := m.Options.HighlightID != "" && msg.ID == m.Options.HighlightID
		out[i] = shape{
			header: !cont[i] || highlighted,
			status: msg.Direction == domain.Outgoing && (i == lastOut || !settled(msg.Status)),
			spacer: i == n-1 || !cont[i+1],
		}
	}
	return out
}

// drawable is the image to draw for a part: the part itself once fetched, or
// a converted copy of it when it is in a format the app cannot decode (a HEIC
// photo, say); otherwise the backend's preview. Falling back to the preview
// matters: a fetched photo the app cannot read used to replace the preview
// that was showing, so downloading it made the picture disappear.
func drawable(a domain.Attachment) string {
	if a.LocalPath != "" {
		if p, ok := media.Displayable(a.LocalPath); ok {
			return p
		}
	}
	if media.Readable(a.ThumbPath) {
		return a.ThumbPath
	}
	return ""
}

func attachmentStamps(msg domain.Message, inline bool) []mediaStamp {
	if !inline || len(msg.Attachments) == 0 {
		return nil
	}
	stamps := make([]mediaStamp, 0, len(msg.Attachments))
	for _, a := range msg.Attachments {
		path := drawable(a)
		stamp := mediaStamp{path: path}
		if info, err := os.Stat(path); err == nil {
			stamp.size = info.Size()
			stamp.modified = info.ModTime().UnixNano()
		}
		stamps = append(stamps, stamp)
	}
	return stamps
}

type Model struct {
	Messages                        []domain.Message
	Selected, Offset, Width, Height int
	Follow                          bool
	lines                           []string
	starts, ends                    []int
	transmissions                   []imageTransmission
	Options                         Options
	cache                           map[string]renderedMessage
	cacheRenderer                   tideui.Renderer
}

// Transmissions returns the graphics escapes for the images the last Layout
// placed. They carry no width and must be written outside the pane, ahead of
// the frame: a pane pads and truncates its content, and an image has to reach
// the terminal whole.
func (m *Model) Transmissions() string {
	var out strings.Builder
	for _, tx := range m.transmissions {
		if tx.message < len(m.starts) && m.ends[tx.message] > m.Offset && m.starts[tx.message] < m.Offset+m.Height {
			out.WriteString(tx.data)
		}
	}
	return out.String()
}

// Cells reserved around every bubble: the selection marker View prepends, the
// "│ " gutter, and a margin that keeps text off the pane border.
const (
	selectionWidth = 2
	gutterWidth    = 2
	marginWidth    = 1
	// A bubble never spans the full pane: the gap opposite it is what shows at a
	// glance which side a message came from.
	oppositeGap = 8
	// Below this the frame's four cells leave too little room for text, so the
	// plain gutter is used instead.
	minBubbleWidth = 24
	// An inline image is a thumbnail, not the message: it stays small enough
	// that the conversation around it is still readable, and v opens the full
	// image. The row cap only binds on an image taller than it is wide.
	inlineImageCols    = 24
	inlineImageRows    = 10
	inlineImageMaxRows = 20
)

// placeImage draws one image with the terminal's graphics protocol, returning
// the transmission escape and the cells that show it. It reports false for a
// file that is not a decodable image, so the caller falls back to text.
func placeImage(path string, bw int, opts Options) (string, []string, bool) {
	if path == "" {
		return "", nil, false
	}
	iw, ih, ok := media.ImageBounds(path)
	if !ok {
		return "", nil, false
	}
	// ViewerBox rather than PlacementBox: an image smaller than the thumbnail
	// footprint is drawn at its own size instead of being blown up to fill it.
	cols, rows := media.ViewerBox(iw, ih, min(bw, inlineImageCols), inlineImageMaxRows, opts.CellWidth, opts.CellHeight)
	if cols < 1 || rows < 1 {
		return "", nil, false
	}
	return media.InlineImage(path, cols, rows)
}

func New() Model { return Model{Follow: true} }
func (m *Model) SetMessages(ms []domain.Message) {
	id := ""
	if selected := m.Current(); selected != nil {
		id = selected.ID
	}
	m.Messages = ms
	m.Selected = max(0, len(ms)-1)
	if !m.Follow {
		for i, v := range ms {
			if v.ID == id {
				m.Selected = i
				break
			}
		}
	}
}
func (m Model) Current() *domain.Message {
	if m.Selected < 0 || m.Selected >= len(m.Messages) {
		return nil
	}
	v := m.Messages[m.Selected]
	return &v
}

// bubbleFor returns the configured palette for a message's direction, falling
// back to the derived surface when the caller set none.
func (o Options) bubbleFor(conv tideui.Theme, direction domain.Direction) themes.Bubble {
	b := o.Incoming
	if direction == domain.Outgoing {
		b = o.Outgoing
	}
	if b.Fill == "" {
		return themes.BubbleFor(conv, "", direction == domain.Outgoing)
	}
	return b
}

// senderName prefers the resolved contact name over the raw address.
func senderName(msg domain.Message, names map[string]string) string {
	if msg.Direction == domain.Outgoing {
		return "You"
	}
	if name := names[msg.Sender]; name != "" {
		return Safe(name)
	}
	if key := contacts.MatchKey(msg.Sender); key != "" {
		if name := names[key]; name != "" {
			return Safe(name)
		}
	}
	return Safe(msg.Sender)
}

func Safe(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}
func (m *Model) Layout(r tideui.Renderer, w, h int, opts Options) {
	cache := m.cache
	if m.Width != max(1, w) || !reflect.DeepEqual(m.Options, opts) || !reflect.DeepEqual(m.cacheRenderer, r) {
		cache = nil
	}
	// Replace rather than mutate: saved conversation views can share old caches.
	m.cache = make(map[string]renderedMessage, len(m.Messages))
	m.cacheRenderer = r
	m.Width = max(1, w)
	m.Height = max(1, h)
	m.Options = opts
	m.Options.Names = maps.Clone(opts.Names)
	m.lines = nil
	m.starts = nil
	m.ends = nil
	m.transmissions = nil
	lastDate := ""
	boundary := false
	shapes := m.shapes()
	for i, msg := range m.Messages {
		sh := shapes[i]
		start := len(m.lines)
		date := msg.Timestamp.Local().Format("2006-01-02")
		if opts.Dates && date != lastDate {
			label := msg.Timestamp.Local().Format("Mon, Jan 2 2006")
			if date == time.Now().Format("2006-01-02") {
				label = "Today"
			}
			m.lines = append(m.lines, r.Styles.DetailMeta.Render("── "+label+" ──"))
			lastDate = date
		}
		if msg.Unread && !boundary {
			m.lines = append(m.lines, r.Styles.Badge.Render("── Unread messages ──"))
			boundary = true
		}
		// Date lines and the unread boundary depend on neighbouring messages and
		// are drawn fresh; so does a message's shape, which the cache is keyed on.
		// File metadata invalidates image caches when downloads arrive or change.
		bodyStart := len(m.lines)
		transmissionStart := len(m.transmissions)
		stamps := attachmentStamps(msg, opts.InlineMedia)
		if cached, ok := cache[msg.ID]; ok && cached.shape == sh && reflect.DeepEqual(cached.media, stamps) && reflect.DeepEqual(cached.message, msg) {
			m.lines = append(m.lines, cached.lines...)
			for _, tx := range cached.transmissions {
				m.transmissions = append(m.transmissions, imageTransmission{message: len(m.starts), data: tx})
			}
			m.starts = append(m.starts, start)
			m.ends = append(m.ends, len(m.lines))
			m.cache[msg.ID] = cached
			continue
		}
		sender := senderName(msg, opts.Names)
		stamp := msg.Timestamp.Local().Format("15:04")
		if opts.Timestamps == "full" {
			stamp = msg.Timestamp.Local().Format("Jan 2, 2006 15:04:05")
		}
		label := sender + " · " + stamp
		// One-to-one, the pane title already names the other person, so their
		// messages carry only the time.
		if !opts.Group && msg.Direction == domain.Incoming {
			label = stamp
		}
		status := string(msg.Status)
		body := Safe(msg.Body)
		// A media-only message has no text; the attachment block is its
		// content, so the empty-body placeholder would be misleading.
		if body == "" && len(msg.Attachments) == 0 {
			body = "[Empty message]"
		}
		// Every bubble reserves the selection marker, its own gutter and a right
		// margin, so text never runs into either pane edge regardless of side.
		// A framed message spends two more cells than a plain gutter, so the text
		// inside stays the same width either way. A pane too narrow to close a
		// frame falls back to the gutter rather than drawing a broken one.
		frame := gutterWidth
		bubbles := opts.Bubbles && w >= minBubbleWidth
		if bubbles {
			frame = gutterWidth + 2
		}
		avail := max(1, w-selectionWidth-frame-marginWidth)
		// Leave a gap on the opposite side so the two directions stay visually
		// distinct, but only a small fixed one, and otherwise use the pane. A
		// fixed ceiling here made a wide terminal wrap early and leave a column
		// of the conversation permanently empty, whatever its size.
		bw := max(1, avail-min(oppositeGap, avail/4))
		if opts.MaxWidth > 0 {
			bw = max(1, min(bw, opts.MaxWidth))
		}
		var wrapped []string
		if body != "" {
			wrapped = strings.Split(ansi.Wrap(body, max(1, bw), ""), "\n")
		}
		// Attachments render as a compact block after the body, so media never
		// blocks the conversation and needs no local file to be listed.
		for _, a := range msg.Attachments {
			// Whichever image is on disk can be drawn as text right here: the
			// part once fetched, otherwise the backend's thumbnail, so a message
			// shows something without waiting on a download. Anything else keeps
			// the compact block.
			if opts.InlineMedia {
				image := drawable(a)
				// A terminal that speaks the graphics protocol draws the real
				// image; the rest get braille dots, which pack 2x4 sub-pixels
				// per cell and so keep fine detail in the same footprint. Either
				// way v opens the full-screen view for a closer look.
				if opts.Graphics {
					if transmit, lines, ok := placeImage(image, bw, opts); ok {
						m.transmissions = append(m.transmissions, imageTransmission{message: len(m.starts), data: transmit})
						wrapped = append(wrapped, lines...)
						continue
					}
				}
				if lines, ok := media.BrailleImage(image, min(bw, inlineImageCols), inlineImageRows); ok {
					wrapped = append(wrapped, lines...)
					continue
				}
			}
			kind, size := media.Describe(a.MIMEType, a.Filename, a.Size)
			name := a.Filename
			if name == "" {
				name = strings.ToLower(kind)
			}
			meta := ""
			if d := media.Dimensions(a.Width, a.Height); d != "" {
				meta = d
			}
			if a.Size > 0 {
				if meta != "" {
					meta += " · "
				}
				meta += size
			}
			if meta == "" {
				meta = "metadata only"
			}
			hint := "d download"
			if a.State == domain.AttachmentAvailable {
				hint = "v preview"
			}
			for _, line := range []string{fmt.Sprintf("[ %s: %s ]", strings.ToLower(kind), name), meta, hint} {
				wrapped = append(wrapped, ansi.Truncate(line, max(1, bw), "…"))
			}
		}
		content := 0
		for _, line := range wrapped {
			content = max(content, ansi.StringWidth(line))
		}
		width := content + frame
		if sh.header {
			width = max(width, ansi.StringWidth(label))
		}
		if sh.status {
			width = max(width, ansi.StringWidth(status))
		}
		indent, labelPad := 0, ""
		if msg.Direction == domain.Outgoing {
			indent = max(0, w-width-selectionWidth-marginWidth)
			// The sender and time sit against the bubble's right edge, as the
			// bubble itself does.
			labelPad = strings.Repeat(" ", max(0, width-ansi.StringWidth(label)))
		}
		pad := strings.Repeat(" ", indent)
		labelLine := pad + labelPad + r.Styles.DetailMeta.Render(ansi.Truncate(label, max(0, w-indent-selectionWidth), "…"))
		if opts.HighlightID != "" && msg.ID == opts.HighlightID {
			// A brief, unmissable cue that this is the message that was jumped to.
			labelLine = r.Styles.SearchMatch.Render("▐ " + ansi.Truncate(label, max(0, w-indent-selectionWidth-2), "…"))
		}
		if sh.header {
			m.lines = append(m.lines, labelLine)
		}
		tl, tr, bl, br := "╭", "╮", "╰", "╯"
		if opts.Corners == "square" {
			tl, tr, bl, br = "┌", "┐", "└", "┘"
		}
		// A filled frame sits on the theme's raised surface. The colour is
		// reopened after the inner styles' resets so the fill is unbroken, and
		// only the frame is painted — the space beside it keeps the pane's own
		// background.
		filling := bubbles && opts.Fill
		paint := func(s string) string { return s }
		bubble := opts.bubbleFor(r.Styles.Theme, msg.Direction)
		// The frame glyphs are drawn in their own style: the conversation's
		// accent, or the frame colour a chosen bubble theme names. Painting a
		// colour over glyphs already styled does not recolour them, since the
		// inner style sets its own foreground, so the choice is made here.
		edge := r.Styles.Badge
		if bubble.Frame != "" {
			edge = lipgloss.NewStyle().Foreground(bubble.Frame).Bold(true)
		}
		if filling {
			style := lipgloss.NewStyle().Background(bubble.Fill).Foreground(bubble.Text)
			paint = func(s string) string { return tideui.StyleOver(style, s) }
			edge = edge.Background(bubble.Fill)
		}
		framePaint := paint
		if bubbles {
			m.lines = append(m.lines, pad+framePaint(edge.Render(tl+strings.Repeat("─", content+2)+tr)))
		}
		for _, line := range wrapped {
			// The trailing gap is measured before highlighting, which adds
			// styling that carries no width of its own.
			gap := strings.Repeat(" ", max(0, content-ansi.StringWidth(line)))
			text := line
			if opts.Query != "" {
				text = highlight(r, text, opts.Query)
			}
			row := edge.Render("│ ") + text
			if bubbles {
				row += gap + edge.Render(" │")
			}
			m.lines = append(m.lines, pad+paint(row))
		}
		if bubbles {
			m.lines = append(m.lines, pad+framePaint(edge.Render(bl+strings.Repeat("─", content+2)+br)))
		}
		if sh.status {
			// The status sits against the same right edge as the sender and time,
			// so an outgoing message reads as one block rather than three.
			statusPad := strings.Repeat(" ", max(0, width-ansi.StringWidth(status)))
			m.lines = append(m.lines, pad+statusPad+r.Styles.DetailMeta.Render(status))
		}
		if sh.spacer {
			m.lines = append(m.lines, "")
		}
		if msg.ID != "" {
			entry := renderedMessage{message: msg, shape: sh, lines: append([]string(nil), m.lines[bodyStart:]...), media: stamps}
			entry.message.Attachments = append([]domain.Attachment(nil), msg.Attachments...)
			for _, tx := range m.transmissions[transmissionStart:] {
				entry.transmissions = append(entry.transmissions, tx.data)
			}
			m.cache[msg.ID] = entry
		}
		m.starts = append(m.starts, start)
		m.ends = append(m.ends, len(m.lines))
	}
	if len(m.Messages) == 0 {
		m.lines = []string{r.Styles.DetailMeta.Render("No cached messages yet."), r.Styles.DetailMeta.Render("Refresh to request history from your phone.")}
	}
	if m.Follow {
		m.Offset = max(0, len(m.lines)-m.Height)
	} else {
		m.Offset = max(0, min(m.Offset, max(0, len(m.lines)-m.Height)))
		m.ensureSelected()
	}
}
func highlight(r tideui.Renderer, line, query string) string {
	// Case-folded rune matching keeps Unicode offsets intact.
	a, q := []rune(line), []rune(query)
	if len(q) == 0 {
		return line
	}
	var b strings.Builder
	for i := 0; i < len(a); {
		if i+len(q) <= len(a) && strings.EqualFold(string(a[i:i+len(q)]), query) {
			b.WriteString(r.Styles.SearchMatch.Render(string(a[i : i+len(q)])))
			i += len(q)
		} else {
			b.WriteRune(a[i])
			i++
		}
	}
	return b.String()
}
func (m *Model) ensureSelected() {
	if m.Selected >= len(m.starts) || m.Selected < 0 {
		return
	}
	start, end := m.starts[m.Selected], m.ends[m.Selected]
	if start < m.Offset {
		m.Offset = start
	}
	if end > m.Offset+m.Height {
		if end-start >= m.Height {
			m.Offset = start
		} else {
			m.Offset = max(0, end-m.Height)
		}
	}
}
func (m *Model) Move(delta int) {
	m.Follow = false
	m.Selected = max(0, min(len(m.Messages)-1, m.Selected+delta))
	m.ensureSelected()
}
func (m *Model) Newest() {
	m.Follow = true
	m.Selected = max(0, len(m.Messages)-1)
	m.Offset = max(0, len(m.lines)-m.Height)
}
func (m *Model) Oldest() { m.Follow = false; m.Selected = 0; m.Offset = 0 }
func (m *Model) Scroll(delta int) {
	m.Follow = false
	m.Offset = max(0, min(max(0, len(m.lines)-m.Height), m.Offset+delta))
	for i := range m.starts {
		if m.ends[i] > m.Offset {
			m.Selected = i
			break
		}
	}
}
func (m Model) AtNewest() bool { return len(m.Messages) > 0 && m.Offset+m.Height >= len(m.lines) }
func (m Model) View(r tideui.Renderer, focused bool) string {
	out := make([]string, 0, m.Height)
	for i := m.Offset; i < min(len(m.lines), m.Offset+m.Height); i++ {
		line := m.lines[i]
		prefix := "  "
		if focused && m.Selected < len(m.starts) && i >= m.starts[m.Selected] && i < m.ends[m.Selected] {
			prefix = r.Styles.Badge.Render("▌ ")
		}
		out = append(out, ansi.Truncate(prefix+line, m.Width, ""))
	}
	for len(out) < m.Height {
		out = append(out, "")
	}
	return strings.Join(out, "\n")
}
func (m Model) Position() string {
	if len(m.Messages) == 0 {
		return "0 messages"
	}
	return fmt.Sprintf("%d/%d · PgUp/Dn scroll", m.Selected+1, len(m.Messages))
}
