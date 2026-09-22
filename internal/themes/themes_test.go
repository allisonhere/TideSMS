package themes

import (
	"fmt"
	"github.com/allisonhere/tideui"
	"testing"
)

// Every palette TideUI ships is offered, so a contact can be given any of them.
func TestNamesCoverTideUIThemes(t *testing.T) {
	if len(Names) != len(tideui.BuiltinThemes) || len(Names) < 10 {
		t.Fatalf("offering %d of %d themes", len(Names), len(tideui.BuiltinThemes))
	}
	for _, name := range Names {
		if !Valid(name) {
			t.Errorf("%q is offered but rejected", name)
		}
		if got := Resolve("catppuccin-mocha", name, "+15551234567"); got.Name != name {
			t.Errorf("%q resolved to %q", name, got.Name)
		}
	}
	if Valid("not-a-theme") {
		t.Error("an unknown theme was accepted")
	}
}

// An explicit theme is used whole, not reduced to an accent.
func TestExplicitThemeReplacesTheWholePalette(t *testing.T) {
	base := Resolve("catppuccin-mocha", "", "")
	got := Resolve("catppuccin-mocha", "gruvbox-dark", "+15551234567")
	want, _ := tideui.ThemeByName("gruvbox-dark")
	if got.Bg != want.Bg || got.Fg != want.Fg || got.BorderFocus != want.BorderFocus {
		t.Fatalf("contact theme not applied whole: %+v", got)
	}
	if got.Bg == base.Bg {
		t.Error("background unchanged by a different theme")
	}
}

// Without a theme of their own, people differ by accent while the surrounding
// palette stays put, and the accent never changes between runs.
func TestAutomaticAccentsKeepThePalette(t *testing.T) {
	base := Resolve("nord", "", "")
	seen := map[string]bool{}
	for i := 0; i < 40; i++ {
		id := fmt.Sprintf("+1555000%04d", i)
		got := Resolve("nord", "", id)
		if got.Bg != base.Bg || got.Fg != base.Fg {
			t.Fatalf("an automatic accent changed the palette for %s", id)
		}
		if again := Resolve("nord", "", id); again.BorderFocus != got.BorderFocus {
			t.Fatalf("accent is not stable for %s", id)
		}
		seen[string(got.BorderFocus)] = true
	}
	// Accents collide by design on a small palette; what matters is that they
	// spread rather than collapsing onto one colour.
	if len(seen) != len(accents) {
		t.Errorf("used %d of %d accents", len(seen), len(accents))
	}
}

// Themes written before TideUI's palettes were adopted keep working.
func TestLegacyAccentNamesStillResolve(t *testing.T) {
	for _, name := range []string{"tide", "rose", "ocean", "violet", "amber", "mint", "mono"} {
		if !Valid(name) {
			t.Errorf("legacy %q rejected", name)
		}
		got := Resolve("mint", name, "+15551234567")
		if got.Name != name {
			t.Errorf("legacy %q resolved to %q", name, got.Name)
		}
		if got.BorderFocus != legacy[name] {
			t.Errorf("legacy %q lost its accent", name)
		}
	}
	// A legacy global keeps TideUI's default palette underneath it.
	mocha, _ := tideui.ThemeByName("catppuccin-mocha")
	if got := Resolve("mint", "", ""); got.Bg != mocha.Bg || got.BorderFocus != legacy["mint"] {
		t.Fatalf("legacy global theme: %+v", got)
	}
}
