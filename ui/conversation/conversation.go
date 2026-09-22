// Package conversation owns message selection, wrapping and viewport state.
package conversation

import (
	"fmt"
	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	colorful "github.com/lucasb-eyer/go-colorful"
	"math"
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
	Names             map[string]string
	Timestamps, Query string
}
type Model struct {
	Messages                        []domain.Message
	Selected, Offset, Width, Height int
	Follow                          bool
	lines                           []string
	starts, ends                    []int
	Options                         Options
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
)

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

const (
	// outgoingTint is how far a sent message's fill moves from the background
	// towards the accent. It is deliberately small: the foreground is chosen for
	// the background, so a saturated fill would cost legibility.
	outgoingTint = 0.22
	tintStep     = 0.04
	// minContrast is the WCAG AA ratio for body text. A theme whose own text sits
	// below it is not held to a standard it does not meet itself; the fill only
	// has to be no less legible than the background it replaces.
	minContrast = 4.5
	// contrastTolerance absorbs the rounding of the lightness-preserving blend.
	contrastTolerance = 0.1
)

// fillColor gives the two directions different surfaces. Received messages sit
// on the theme's raised surface; sent ones sit on the background tinted towards
// the accent, which nearly every theme leaves identical to its status bar, so
// there is no second surface to borrow.
func fillColor(t tideui.Theme, direction domain.Direction) lipgloss.Color {
	if direction == domain.Outgoing {
		if c, ok := readableTint(t.Bg, t.BorderFocus, t.Fg); ok {
			return c
		}
	}
	for _, c := range []lipgloss.Color{t.Overlay, t.StatusBar, t.Bg} {
		if c != "" {
			return c
		}
	}
	return t.Bg
}

// readableTint blends as far towards the accent as the theme allows while
// keeping text on the result legible, falling back to the gentlest tint when no
// amount reaches the threshold.
func readableTint(bg, accent, fg lipgloss.Color) (lipgloss.Color, bool) {
	floor := math.Min(minContrast, contrastRatio(bg, fg)) - contrastTolerance
	var last lipgloss.Color
	for amount := outgoingTint; amount >= tintStep; amount -= tintStep {
		c, ok := tint(bg, accent, amount)
		if !ok {
			return bg, false
		}
		if contrastRatio(c, fg) >= floor {
			return c, true
		}
		last = c
	}
	if last == "" {
		return bg, false
	}
	return last, true
}

// contrastRatio is the WCAG relative-luminance ratio between two colours.
func contrastRatio(a, b lipgloss.Color) float64 {
	x, err := colorful.Hex(string(a))
	if err != nil {
		return 0
	}
	y, err := colorful.Hex(string(b))
	if err != nil {
		return 0
	}
	lighter, darker := relativeLuminance(x), relativeLuminance(y)
	if lighter < darker {
		lighter, darker = darker, lighter
	}
	return (lighter + 0.05) / (darker + 0.05)
}

func relativeLuminance(c colorful.Color) float64 {
	channel := func(v float64) float64 {
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(c.R) + 0.7152*channel(c.G) + 0.0722*channel(c.B)
}

// tint shifts base towards mix in hue and chroma while keeping base's
// lightness, so the result is visibly different without costing the contrast the
// theme chose for its text. It reports false when either colour is not hex.
func tint(base, mix lipgloss.Color, amount float64) (lipgloss.Color, bool) {
	from, err := colorful.Hex(string(base))
	if err != nil {
		return base, false
	}
	to, err := colorful.Hex(string(mix))
	if err != nil {
		return base, false
	}
	_, a, b := from.BlendLab(to, amount).Lab()
	lightness, _, _ := from.Lab()
	return lipgloss.Color(colorful.Lab(lightness, a, b).Clamped().Hex()), true
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
	m.Width = max(1, w)
	m.Height = max(1, h)
	m.Options = opts
	m.lines = nil
	m.starts = nil
	m.ends = nil
	lastDate := ""
	boundary := false
	for _, msg := range m.Messages {
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
		sender := senderName(msg, opts.Names)
		stamp := msg.Timestamp.Local().Format("15:04")
		if opts.Timestamps == "full" {
			stamp = msg.Timestamp.Local().Format("Jan 2, 2006 15:04:05")
		}
		label := sender + " · " + stamp
		body := Safe(msg.Body)
		if body == "" {
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
		wrapped := strings.Split(ansi.Wrap(body, max(1, bw), ""), "\n")
		content := 0
		for _, line := range wrapped {
			content = max(content, ansi.StringWidth(line))
		}
		width := max(ansi.StringWidth(label), content+frame)
		indent, labelPad := 0, ""
		if msg.Direction == domain.Outgoing {
			indent = max(0, w-width-selectionWidth-marginWidth)
			// The sender and time sit against the bubble's right edge, as the
			// bubble itself does.
			labelPad = strings.Repeat(" ", max(0, width-ansi.StringWidth(label)))
		}
		pad := strings.Repeat(" ", indent)
		m.lines = append(m.lines, pad+labelPad+r.Styles.DetailMeta.Render(ansi.Truncate(label, max(0, w-indent-selectionWidth), "…")))
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
		if filling {
			style := lipgloss.NewStyle().
				Background(fillColor(r.Styles.Theme, msg.Direction)).
				Foreground(r.Styles.Theme.Fg)
			paint = func(s string) string { return tideui.StyleOver(style, s) }
		}
		if bubbles {
			m.lines = append(m.lines, pad+paint(r.Styles.Badge.Render(tl+strings.Repeat("─", content+2)+tr)))
		}
		for _, line := range wrapped {
			// The trailing gap is measured before highlighting, which adds
			// styling that carries no width of its own.
			gap := strings.Repeat(" ", max(0, content-ansi.StringWidth(line)))
			text := line
			if opts.Query != "" {
				text = highlight(r, text, opts.Query)
			}
			row := r.Styles.Badge.Render("│ ") + text
			if bubbles {
				row += gap + r.Styles.Badge.Render(" │")
			}
			m.lines = append(m.lines, pad+paint(row))
		}
		if bubbles {
			m.lines = append(m.lines, pad+paint(r.Styles.Badge.Render(bl+strings.Repeat("─", content+2)+br)))
		}
		if msg.Direction == domain.Outgoing {
			// The status sits against the same right edge as the sender and time,
			// so an outgoing message reads as one block rather than three.
			statusPad := strings.Repeat(" ", max(0, width-ansi.StringWidth(string(msg.Status))))
			m.lines = append(m.lines, pad+statusPad+r.Styles.DetailMeta.Render(string(msg.Status)))
		}
		m.lines = append(m.lines, "")
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
