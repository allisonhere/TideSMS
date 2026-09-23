package themes

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/lipgloss"
)

// Omarchy is the theme that follows the Omarchy desktop: whatever palette
// `omarchy theme set` last applied. It can be chosen anywhere a theme can, the
// global theme, a contact, a thread or a bubble, and it changes with the desktop
// rather than being fixed when it was picked.
const Omarchy = "omarchy"

// omarchyColors is the part of an Omarchy theme's colors.toml a palette needs.
type omarchyColors struct {
	Accent     string `toml:"accent"`
	Background string `toml:"background"`
	Foreground string `toml:"foreground"`
	Color0     string `toml:"color0"`
	Color1     string `toml:"color1"`
	Color2     string `toml:"color2"`
	Color4     string `toml:"color4"`
	Color8     string `toml:"color8"`
}

// omarchyPath is where Omarchy keeps the applied theme. It is a variable so
// tests can point it at a fixture.
var omarchyPath = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "omarchy", "current", "theme", "colors.toml")
}

// omarchyRecheck bounds how often the file is looked at. Themes are resolved
// many times a frame, and a desktop theme change only needs noticing within
// the app's own refresh.
const omarchyRecheck = time.Second

var omarchyCache struct {
	sync.Mutex
	path    string
	checked time.Time
	info    os.FileInfo
	theme   tideui.Theme
	ok      bool
}

// OmarchyAvailable reports whether this machine has an applied Omarchy theme.
func OmarchyAvailable() bool {
	_, ok := omarchyTheme()
	return ok
}

// omarchyTheme reads the applied Omarchy theme, re-reading it only when the
// file has changed. A missing or unreadable file reports false, and the caller
// falls back to the default palette so a config naming "omarchy" still loads
// on a machine without it.
func omarchyTheme() (tideui.Theme, bool) {
	c := &omarchyCache
	c.Lock()
	defer c.Unlock()
	path := omarchyPath()
	now := time.Now()
	if path == c.path && now.Sub(c.checked) < omarchyRecheck {
		return c.theme, c.ok
	}
	c.path, c.checked = path, now
	info, err := os.Stat(path)
	if err != nil {
		c.ok = false
		return c.theme, false
	}
	// Omarchy switches theme by moving a whole directory into place, so the
	// file's identity changes as well as, or instead of, its timestamp.
	if c.ok && c.info != nil && os.SameFile(info, c.info) && info.ModTime().Equal(c.info.ModTime()) {
		return c.theme, true
	}
	var colors omarchyColors
	if _, err := toml.DecodeFile(path, &colors); err != nil || !hex(colors.Background) || !hex(colors.Foreground) {
		c.ok = false
		return c.theme, false
	}
	c.theme, c.info, c.ok = omarchyPalette(colors), info, true
	return c.theme, true
}

// omarchyPalette maps an Omarchy palette onto TideUI's. The accent drives
// focus and selection as it does across the desktop, color8 (the terminal's
// bright black) is the quiet border and muted text, and TideUI raises any
// pairing that falls short of legible contrast on its own.
func omarchyPalette(c omarchyColors) tideui.Theme {
	pick := func(values ...string) lipgloss.Color {
		for _, v := range values {
			if hex(v) {
				return lipgloss.Color(v)
			}
		}
		return ""
	}
	accent := pick(c.Accent, c.Color4, c.Foreground)
	// The status bar sits on color0 when that differs from the background;
	// most themes make them equal, which would hide the bar, so color8 then.
	raised := pick(c.Color8)
	if hex(c.Color0) && !strings.EqualFold(c.Color0, c.Background) {
		raised = lipgloss.Color(c.Color0)
	}
	return tideui.Theme{
		Name:          Omarchy,
		Bg:            lipgloss.Color(c.Background),
		Fg:            lipgloss.Color(c.Foreground),
		Border:        pick(c.Color8, c.Foreground),
		BorderFocus:   accent,
		Selected:      accent,
		Unread:        pick(c.Color2, c.Accent),
		Dimmed:        pick(c.Color8, c.Foreground),
		StatusBar:     raised,
		StatusFg:      lipgloss.Color(c.Foreground),
		Error:         pick(c.Color1, c.Accent),
		OverlayBorder: accent,
	}
}

func hex(v string) bool {
	if len(v) != 7 || v[0] != '#' {
		return false
	}
	for _, r := range v[1:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}
