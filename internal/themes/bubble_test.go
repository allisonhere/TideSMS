package themes

import (
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/lipgloss"
	"math"
	"testing"
)

// The derived surfaces must still tell the two directions apart, and the
// received fill must be no harder to read than the background it replaces.
func TestDerivedSurfacesMatchDirection(t *testing.T) {
	for _, th := range tideui.BuiltinThemes {
		in := BubbleFor(th, "", false)
		out := BubbleFor(th, "", true)
		if in.Fill == out.Fill {
			t.Errorf("%s: both directions fill with %s", th.Name, in.Fill)
		}
		if out.Fill != th.Overlay && th.Overlay != "" {
			t.Errorf("%s: sent messages should use the raised surface, got %s", th.Name, out.Fill)
		}
		if out.Text != th.Fg || in.Text != th.Fg {
			t.Errorf("%s: derived text should be the theme foreground", th.Name)
		}
		baseline := contrastRatio(th.Bg, th.Fg)
		floor := math.Min(minContrast, baseline) - contrastTolerance
		if got := contrastRatio(in.Fill, th.Fg); got < floor {
			t.Errorf("%s: received fill contrast %.2f below %.2f (theme's own %.2f)", th.Name, got, floor, baseline)
		}
	}
}

// An explicit theme supplies the bubble colours and is labelled with its name.
func TestBubbleForExplicitTheme(t *testing.T) {
	conv := Base("catppuccin-mocha")
	b := BubbleFor(conv, "dracula", true)
	if b.Name != "dracula" {
		t.Fatalf("name = %q", b.Name)
	}
	base := Base("dracula")
	if b.Frame != base.BorderFocus {
		t.Fatalf("frame = %q, want %q", b.Frame, base.BorderFocus)
	}
	if b.Fill == "" || b.Text == "" {
		t.Fatalf("palette incomplete: %+v", b)
	}
}

// Choosing the same theme as the pane would paint bubbles the pane background,
// so the raised surface is substituted instead.
func TestBubbleForAvoidsInvisibleFill(t *testing.T) {
	conv := Base("catppuccin-mocha")
	b := BubbleFor(conv, "catppuccin-mocha", true)
	if b.Fill == conv.Bg {
		t.Fatal("bubble fill matches the pane background")
	}
}

// An unknown name is ignored and the derived surface is used.
func TestBubbleForIgnoresUnknownName(t *testing.T) {
	conv := Base("nord")
	b := BubbleFor(conv, "not-a-theme", true)
	if b.Name != "" {
		t.Fatalf("unknown name kept: %q", b.Name)
	}
	if want := BubbleFor(conv, "", true); b.Fill != want.Fill {
		t.Fatalf("fill = %q, want derived %q", b.Fill, want.Fill)
	}
}

// A colour that cannot be parsed leaves the background untouched rather than
// rendering something arbitrary.
func TestTintRejectsUnparseableColours(t *testing.T) {
	for _, tc := range [][2]lipgloss.Color{{"", "#ffffff"}, {"#1e1e2e", ""}, {"9", "#ffffff"}} {
		if c, ok := tint(tc[0], tc[1], 0.2); ok || c != tc[0] {
			t.Errorf("tint(%q,%q) = %q,%v", tc[0], tc[1], c, ok)
		}
	}
}
