package conversation

import (
	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tidesms/internal/themes"
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"regexp"
	"strings"
	"testing"
	"time"
)

func render(t *testing.T, bodies []domain.Message, w, h int) []string {
	t.Helper()
	r := tideui.NewRenderer(themes.Resolve("tide", "", ""), tideui.StyleOptions{})
	m := New()
	m.SetMessages(bodies)
	m.Layout(r, w, h, Options{Dates: true, Timestamps: "smart"})
	return strings.Split(m.View(r, true), "\n")
}

func message(dir domain.Direction, body string) domain.Message {
	return domain.Message{ID: body + string(dir), Sender: "+15551234567", Body: body,
		Timestamp: time.Date(2026, 9, 22, 10, 41, 0, 0, time.UTC), Direction: dir, Status: domain.Sent}
}

// No rendered line may exceed the pane, at any width, for any content.
func TestNoLineExceedsTheWidth(t *testing.T) {
	msgs := []domain.Message{
		message(domain.Incoming, "Are we still meeting around seven?"),
		message(domain.Outgoing, "Yep, I'll be there."),
		message(domain.Incoming, "Bring dessert 😄 and maybe some 🍰🍰🍰 too"),
		message(domain.Outgoing, "https://example.com/a/very/long/path/that/cannot/be/broken/at/spaces/at/all/ever"),
		message(domain.Incoming, "日本語のテキストは全角文字で幅が二倍になります。折り返しに注意。"),
		message(domain.Outgoing, strings.Repeat("supercalifragilistic ", 12)),
		message(domain.Incoming, "line one\nline two is quite a lot longer than the first one\n\nline four"),
		message(domain.Outgoing, "é́́ combining marks and a ZWJ family 👨‍👩‍👧‍👦 here"),
	}
	for _, w := range []int{120, 100, 80, 60, 40, 30, 20, 12, 5, 1} {
		for _, line := range render(t, msgs, w, 40) {
			if got := ansi.StringWidth(line); got > w {
				t.Errorf("width %d: line of width %d: %q", w, got, ansi.Strip(line))
			}
		}
	}
}

// Wrapping must not lose or reorder the text of a message.
func TestWrappingPreservesContent(t *testing.T) {
	body := "The quick brown fox jumps over the lazy dog near the river bank at dawn"
	for _, w := range []int{100, 60, 40, 24} {
		lines := render(t, []domain.Message{message(domain.Incoming, body)}, w, 40)
		var parts []string
		for _, l := range lines {
			text := ansi.Strip(l)
			if i := strings.Index(text, "│ "); i >= 0 {
				parts = append(parts, strings.TrimSpace(text[i+len("│ "):]))
			}
		}
		flat := strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
		if !strings.Contains(flat, strings.Join(strings.Fields(body), " ")) {
			t.Errorf("width %d lost text:\n%s", w, flat)
		}
	}
}

// A bubble must use the pane it is given. Capping it at a fifth below the
// available width left a quarter of the conversation permanently empty and made
// messages wrap far earlier than they needed to.
func TestBubblesUseTheAvailableWidth(t *testing.T) {
	// One unbreakable token, so the break lands exactly on the wrap width rather
	// than on the last word boundary before it.
	long := strings.Repeat("x", 600)
	for _, tc := range []struct{ w, want int }{
		{81, 81 - selectionWidth - gutterWidth - marginWidth - oppositeGap},
		{60, 60 - selectionWidth - gutterWidth - marginWidth - oppositeGap},
		{200, 200 - selectionWidth - gutterWidth - marginWidth - oppositeGap},
	} {
		for _, dir := range []domain.Direction{domain.Incoming, domain.Outgoing} {
			widest := 0
			for _, line := range render(t, []domain.Message{message(dir, long)}, tc.w, 40) {
				text := ansi.Strip(line)
				if i := strings.Index(text, "│ "); i >= 0 {
					widest = max(widest, ansi.StringWidth(text[i+len("│ "):]))
				}
			}
			if widest != tc.want {
				t.Errorf("width %d %s: wrapped at %d, want %d", tc.w, dir, widest, tc.want)
			}
		}
	}
}

// The sender, the body and the status of an outgoing message share one right
// edge, so it reads as a single block.
func TestOutgoingBlockSharesARightEdge(t *testing.T) {
	lines := render(t, []domain.Message{message(domain.Outgoing, "it works thatway now")}, 81, 40)
	var edges []int
	for _, line := range lines {
		text := strings.TrimRight(ansi.Strip(line), " ")
		if strings.TrimSpace(text) == "" || strings.Contains(text, "──") {
			continue // The date separator spans its own line.
		}
		edges = append(edges, ansi.StringWidth(text))
	}
	if len(edges) < 3 {
		t.Fatalf("expected label, body and status: %v", edges)
	}
	if edges[0] != edges[1] || edges[1] != edges[2] {
		t.Errorf("ragged right edge: label %d, body %d, status %d", edges[0], edges[1], edges[2])
	}
}

// An incoming message shows who sent it. The thread title resolved the contact
// but each message printed the bare number, so a conversation with a known
// person was labelled with their address on every line.
func TestIncomingMessagesShowTheSenderName(t *testing.T) {
	names := map[string]string{"+13145178351": "David Queen", "8165550182": "Rina"}
	for _, tc := range []struct{ sender, want string }{
		{"+13145178351", "David Queen"},  // exact number
		{"+18165550182", "Rina"},         // known without a country code
		{"+15550000000", "+15550000000"}, // genuinely unknown, shown as dialled
	} {
		got := senderName(domain.Message{Sender: tc.sender, Direction: domain.Incoming}, names)
		if got != tc.want {
			t.Errorf("sender %q rendered as %q, want %q", tc.sender, got, tc.want)
		}
	}
	if got := senderName(domain.Message{Sender: "+13145178351", Direction: domain.Outgoing}, names); got != "You" {
		t.Errorf("outgoing rendered as %q", got)
	}
	// A name from a phone may not carry terminal control sequences.
	evil := map[string]string{"+15551112222": "Ev\x1b[31mil"}
	if got := senderName(domain.Message{Sender: "+15551112222", Direction: domain.Incoming}, evil); strings.Contains(got, "\x1b") {
		t.Errorf("control sequence survived: %q", got)
	}
}

func renderOpts(t *testing.T, ms []domain.Message, w, h int, o Options) []string {
	t.Helper()
	r := tideui.NewRenderer(themes.Resolve("tide", "", ""), tideui.StyleOptions{})
	m := New()
	m.SetMessages(ms)
	m.Layout(r, w, h, o)
	return strings.Split(m.View(r, true), "\n")
}

// A framed message is closed on all four sides, and its frame costs width that
// the text inside gives up, so the message still fits the pane.
func TestBubblesEncloseTheMessage(t *testing.T) {
	msgs := []domain.Message{
		message(domain.Incoming, "Are we still meeting around seven?"),
		message(domain.Outgoing, "Yep, I'll be there. 😄 日本語もね"),
		message(domain.Incoming, strings.Repeat("long ", 40)),
		message(domain.Outgoing, "https://example.com/a/path/that/cannot/break/at/spaces/anywhere/at/all"),
	}
	for _, w := range []int{120, 100, 76, 60, 40, 24, 12, 5, 1} {
		var tops, bottoms, bodies int
		for _, line := range renderOpts(t, msgs, w, 60, Options{Dates: true, Timestamps: "smart", Bubbles: true}) {
			text := ansi.Strip(line)
			if got := ansi.StringWidth(text); got > w {
				t.Fatalf("width %d: framed line of width %d: %q", w, got, text)
			}
			switch {
			case strings.Contains(text, "╭"):
				tops++
			case strings.Contains(text, "╰"):
				bottoms++
			case strings.Contains(text, "│ "):
				bodies++
				// Frames are only drawn where they fit; below that the plain
				// gutter is used, which has nothing to close.
				if w >= minBubbleWidth && !strings.HasSuffix(strings.TrimRight(text, " "), "│") {
					t.Errorf("width %d: unclosed frame: %q", w, text)
				}
			}
		}
		if w < minBubbleWidth && (tops > 0 || bottoms > 0) {
			t.Errorf("width %d drew a frame it cannot close", w)
		}
		if w >= minBubbleWidth {
			if tops != len(msgs) || bottoms != len(msgs) {
				t.Errorf("width %d: %d tops and %d bottoms for %d messages", w, tops, bottoms, len(msgs))
			}
			if bodies < len(msgs) {
				t.Errorf("width %d: only %d body lines", w, bodies)
			}
		}
	}
}

// Turning frames off must not change what the message says, only its chrome.
func TestBubblesAreOnlyChrome(t *testing.T) {
	body := "The quick brown fox jumps over the lazy dog near the river bank"
	read := func(on bool) string {
		var parts []string
		for _, line := range renderOpts(t, []domain.Message{message(domain.Incoming, body)}, 60, 30,
			Options{Timestamps: "smart", Bubbles: on}) {
			text := ansi.Strip(line)
			i := strings.Index(text, "│ ")
			if i < 0 || strings.Contains(text, "╭") || strings.Contains(text, "╰") {
				continue
			}
			parts = append(parts, strings.TrimSpace(strings.TrimSuffix(strings.TrimRight(text[i+len("│ "):], " "), "│")))
		}
		return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
	}
	if framed, plain := read(true), read(false); framed != plain {
		t.Errorf("frames changed the text:\n framed %q\n plain  %q", framed, plain)
	} else if framed != body {
		t.Errorf("text altered: %q", framed)
	}
}

// Corners are a choice; either style closes the frame and neither changes its size.
func TestBubbleCornerStyles(t *testing.T) {
	msgs := []domain.Message{message(domain.Incoming, "Are we still meeting around seven?")}
	widths := map[string]int{}
	for style, want := range map[string]string{"round": "╭╮╰╯", "square": "┌┐└┘"} {
		other := "┌┐└┘"
		if style == "square" {
			other = "╭╮╰╯"
		}
		var found string
		for _, line := range renderOpts(t, msgs, 60, 20, Options{Timestamps: "smart", Bubbles: true, Corners: style}) {
			text := ansi.Strip(line)
			for _, r := range want {
				if strings.ContainsRune(text, r) {
					found += string(r)
				}
			}
			for _, r := range other {
				if strings.ContainsRune(text, r) {
					t.Errorf("%s frame used %q", style, r)
				}
			}
			widths[style] = max(widths[style], ansi.StringWidth(text))
		}
		for _, r := range want {
			if !strings.ContainsRune(found, r) {
				t.Errorf("%s frame is missing %q", style, r)
			}
		}
	}
	if widths["round"] != widths["square"] {
		t.Errorf("corner style changed the frame size: %d vs %d", widths["round"], widths["square"])
	}
	// An unset or unknown value stays rounded rather than drawing nothing.
	var corners string
	for _, line := range renderOpts(t, msgs, 60, 20, Options{Timestamps: "smart", Bubbles: true}) {
		if strings.ContainsRune(ansi.Strip(line), '╭') {
			corners = "round"
		}
	}
	if corners != "round" {
		t.Error("an unset corner style did not fall back to round")
	}
}

// background returns the true-colour sequence a colour renders as, derived from
// lipgloss rather than computed by hand: it round-trips values slightly.
func background(c lipgloss.Color) string {
	return regexp.MustCompile(`48;2;\d+;\d+;\d+`).FindString(lipgloss.NewStyle().Background(c).Render("x"))
}

// Filling paints the frame and its inside on the theme's raised surface, and
// nothing else: the sender, the status and the space beside a bubble keep the
// pane's own background.
func TestBubbleFillPaintsOnlyTheFrame(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	theme := themes.Resolve("tide", "", "")
	received := background(themes.BubbleFor(theme, "", false).Fill)
	sent := background(themes.BubbleFor(theme, "", true).Fill)
	if received == "" || sent == "" || received == sent {
		t.Fatalf("need two distinct fills, got %q and %q", received, sent)
	}
	msgs := []domain.Message{
		message(domain.Incoming, "Are we still meeting around seven?"),
		message(domain.Outgoing, "Yep, I'll be there."),
	}
	framed, bare := 0, 0
	// The outgoing bubble is indented, so its lines are the ones carrying a pad.
	for _, line := range renderOpts(t, msgs, 60, 20, Options{Timestamps: "smart", Bubbles: true, Fill: true}) {
		text := ansi.Strip(line)
		if !strings.ContainsAny(text, "╭╰") && !strings.Contains(text, "│ ") {
			if strings.TrimSpace(text) != "" {
				if strings.Contains(line, received) || strings.Contains(line, sent) {
					t.Errorf("fill escaped the frame: %q", text)
				}
				bare++
			}
			continue
		}
		framed++
		outgoing := strings.HasPrefix(strings.TrimPrefix(text, "▌"), "    ")
		want, other := received, sent
		if outgoing {
			want, other = sent, received
		}
		if !strings.Contains(line, want) {
			t.Errorf("frame line missing its own fill: %q", text)
		}
		if strings.Contains(line, other) {
			t.Errorf("frame line carries the other direction's fill: %q", text)
		}
	}
	if framed < 6 || bare < 2 {
		t.Fatalf("expected frames and plain lines, got %d and %d", framed, bare)
	}

	for _, o := range []Options{
		{Timestamps: "smart", Bubbles: true, Fill: false},
		{Timestamps: "smart", Bubbles: false, Fill: true}, // nothing to fill
	} {
		for _, line := range renderOpts(t, msgs, 60, 20, o) {
			if strings.Contains(line, received) || strings.Contains(line, sent) {
				t.Errorf("bubbles=%v fill=%v painted a surface: %q", o.Bubbles, o.Fill, ansi.Strip(line))
			}
		}
	}
}

// A chosen bubble palette paints the fill and body it names, and the frame
// glyphs carry the palette's frame colour.
func TestExplicitBubbleThemePaintsItsPalette(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	conv := themes.Resolve("tide", "", "")
	in := themes.BubbleFor(conv, "dracula", false)
	out := themes.BubbleFor(conv, "gruvbox-light", true)
	if in.Fill == "" || out.Fill == "" || in.Name != "dracula" || out.Name != "gruvbox-light" {
		t.Fatalf("palette not built: in=%+v out=%+v", in, out)
	}
	msgs := []domain.Message{
		message(domain.Incoming, "Are we still meeting around seven?"),
		message(domain.Outgoing, "Yep, I'll be there."),
	}
	lines := renderOpts(t, msgs, 60, 20, Options{Timestamps: "smart", Bubbles: true, Fill: true, Incoming: in, Outgoing: out})
	inFill, outFill := background(in.Fill), background(out.Fill)
	if inFill == outFill {
		t.Fatal("explicit fills should differ")
	}
	sawIn, sawOut := false, false
	for _, line := range lines {
		if strings.Contains(line, inFill) {
			sawIn = true
		}
		if strings.Contains(line, outFill) {
			sawOut = true
		}
	}
	if !sawIn || !sawOut {
		t.Fatalf("explicit bubble fills not drawn: in=%v out=%v", sawIn, sawOut)
	}
}

// A jumped-to message is marked so it stands out from the messages around it.
func TestHighlightMarksTheJumpedMessage(t *testing.T) {
	msgs := []domain.Message{message(domain.Incoming, "older one"), message(domain.Incoming, "newest one")}
	lines := renderOpts(t, msgs, 60, 20, Options{Timestamps: "smart", HighlightID: msgs[0].ID})
	joined := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "▐ ") {
		t.Fatal("no highlight marker for the jumped-to message")
	}
	if n := strings.Count(joined, "▐ "); n != 1 {
		t.Fatalf("highlight marker count = %d, want 1", n)
	}
}

// An attachment renders as a compact block after the body, with its kind,
// dimensions, size and a preview hint, without needing a local file.
func TestAttachmentBlockRenders(t *testing.T) {
	msg := message(domain.Incoming, "look at this")
	msg.Attachments = []domain.Attachment{{
		ID: "a1", MessageID: msg.ID, MIMEType: "image/jpeg", Filename: "dinner.jpg",
		Size: 1_800_000, Width: 1920, Height: 1080, State: domain.AttachmentMetadata,
	}}
	joined := ansi.Strip(strings.Join(renderOpts(t, []domain.Message{msg}, 60, 20, Options{Timestamps: "smart"}), "\n"))
	for _, want := range []string{"[ image: dinner.jpg ]", "1920×1080 · 1.8 MB", "Enter to preview"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in:\n%s", want, joined)
		}
	}
}
