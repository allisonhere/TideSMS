package themes

import (
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/lipgloss"
	colorful "github.com/lucasb-eyer/go-colorful"
	"math"
)

// Bubble is the palette for one message surface. Fill paints the inside of the
// frame, Text is the body colour, and Frame colours the frame glyphs. An empty
// Name means the surface is derived from the conversation theme exactly as it
// always was, so nothing changes until a bubble theme is chosen.
type Bubble struct {
	Fill, Text, Frame lipgloss.Color
	Name              string
}

const (
	// outgoingTint is how far a received message's fill moves from the
	// background towards the accent. It is deliberately small: the foreground is
	// chosen for the background, so a saturated fill would cost legibility.
	outgoingTint = 0.22
	tintStep     = 0.04
	// minContrast is the WCAG AA ratio for body text. A theme whose own text sits
	// below it is not held to a standard it does not meet itself; the fill only
	// has to be no less legible than the background it replaces.
	minContrast = 4.5
	// contrastTolerance absorbs the rounding of the lightness-preserving blend.
	contrastTolerance = 0.1
)

// BubbleFor builds a bubble palette. With no name it returns the derived
// surface: sent messages sit on the theme's raised surface, received ones on the
// background tinted towards the accent. With a valid name it uses that theme's
// colours, falling back to the derived surface when the chosen fill is
// invisible against the pane or fails the contrast floor.
func BubbleFor(conv tideui.Theme, name string, outgoing bool) Bubble {
	if name == "" || !Valid(name) {
		return Bubble{Fill: derivedFill(conv, outgoing), Text: conv.Fg}
	}
	base := Base(name)
	fill := base.Bg
	if fill == "" || fill == conv.Bg {
		fill = ""
		for _, c := range []lipgloss.Color{base.Overlay, base.StatusBar, conv.Overlay} {
			if c != "" && c != conv.Bg {
				fill = c
				break
			}
		}
		if fill == "" {
			fill = derivedFill(conv, outgoing)
		}
	}
	text := base.Fg
	floor := math.Min(minContrast, contrastRatio(conv.Bg, conv.Fg)) - contrastTolerance
	if contrastRatio(fill, text) < floor {
		return Bubble{Name: name, Fill: derivedFill(conv, outgoing), Text: conv.Fg}
	}
	return Bubble{Name: name, Fill: fill, Text: text, Frame: base.BorderFocus}
}

// derivedFill reproduces the surfaces used before bubble themes existed.
func derivedFill(conv tideui.Theme, outgoing bool) lipgloss.Color {
	if !outgoing {
		if c, ok := readableTint(conv.Bg, conv.BorderFocus, conv.Fg); ok {
			return c
		}
	}
	for _, c := range []lipgloss.Color{conv.Overlay, conv.StatusBar, conv.Bg} {
		if c != "" {
			return c
		}
	}
	return conv.Bg
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
