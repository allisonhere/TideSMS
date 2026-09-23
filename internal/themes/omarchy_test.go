package themes

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// useOmarchy points the reader at a colors.toml in a temporary directory and
// forgets anything cached from another test.
func useOmarchy(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "colors.toml")
	if contents != "" {
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	previous := omarchyPath
	omarchyPath = func() string { return path }
	resetOmarchyCache()
	t.Cleanup(func() {
		omarchyPath = previous
		resetOmarchyCache()
	})
	return path
}

func resetOmarchyCache() {
	omarchyCache.Lock()
	omarchyCache.path, omarchyCache.info, omarchyCache.ok = "", nil, false
	omarchyCache.checked = time.Time{}
	omarchyCache.Unlock()
}

const azureGlow = `
accent = "#00aaff"
background = "#0a0f1a"
foreground = "#a8dfff"
color0 = "#0a0f1a"
color1 = "#0099cc"
color2 = "#00e0b8"
color8 = "#123247"
`

// The Omarchy theme is the desktop's own palette.
func TestOmarchyThemeFollowsTheDesktopPalette(t *testing.T) {
	useOmarchy(t, azureGlow)
	if !Valid(Omarchy) || !OmarchyAvailable() {
		t.Fatal("omarchy not recognised")
	}
	got := Base(Omarchy)
	want := map[string][2]lipgloss.Color{
		"Bg":          {got.Bg, "#0a0f1a"},
		"Fg":          {got.Fg, "#a8dfff"},
		"BorderFocus": {got.BorderFocus, "#00aaff"},
		"Unread":      {got.Unread, "#00e0b8"},
		"Error":       {got.Error, "#0099cc"},
		// color0 is the background here, so the bar uses color8 to stay visible.
		"StatusBar": {got.StatusBar, "#123247"},
	}
	for field, pair := range want {
		if pair[0] != pair[1] {
			t.Errorf("%s = %q, want %q", field, pair[0], pair[1])
		}
	}
	if Accent(Omarchy) != "#00aaff" {
		t.Errorf("accent = %q", Accent(Omarchy))
	}
	// A conversation or bubble given the Omarchy theme uses it whole.
	if Resolve("nord", Omarchy, "+15550001").Bg != "#0a0f1a" {
		t.Error("an omarchy override did not apply")
	}
}

// Changing the desktop theme changes the app's, without choosing again.
func TestOmarchyThemeNoticesADesktopChange(t *testing.T) {
	path := useOmarchy(t, azureGlow)
	if Base(Omarchy).Bg != "#0a0f1a" {
		t.Fatal("first palette not read")
	}
	// Replace the file the way `omarchy theme set` does: a new file moved into
	// place, not an edit in place.
	next := filepath.Join(filepath.Dir(path), "next.toml")
	if err := os.WriteFile(next, []byte(`background = "#fdf6e3"
foreground = "#586e75"
accent = "#268bd2"`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(next, path); err != nil {
		t.Fatal(err)
	}
	omarchyCache.Lock()
	omarchyCache.checked = time.Time{} // skip the one-second recheck window
	omarchyCache.Unlock()
	if got := Base(Omarchy).Bg; got != "#fdf6e3" {
		t.Errorf("desktop change not picked up: %q", got)
	}
}

// Without Omarchy the name still loads, on the default palette, so a config
// copied from an Omarchy machine is not rejected; it is just not offered.
func TestOmarchyMissingFallsBack(t *testing.T) {
	useOmarchy(t, "")
	if OmarchyAvailable() {
		t.Fatal("reported available with no file")
	}
	if !Valid(Omarchy) {
		t.Error("a config naming omarchy would be rejected")
	}
	if Base(Omarchy).Bg == "" {
		t.Error("no fallback palette")
	}
	for _, n := range builtinNames() {
		if n == Omarchy {
			t.Error("offered in pickers without Omarchy")
		}
	}
}

// A malformed file is treated as absent rather than producing a blank palette.
func TestOmarchyMalformedFileIsIgnored(t *testing.T) {
	useOmarchy(t, `background = "not a colour"`)
	if OmarchyAvailable() {
		t.Error("a palette with no usable background was accepted")
	}
}
