package themes

import (
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/lipgloss"
	"hash/fnv"
)

// Names lists the themes offered in pickers: TideUI's own palettes, which carry
// a full set of colors rather than an accent alone.
var Names = builtinNames()

func builtinNames() []string {
	out := make([]string, 0, len(tideui.BuiltinThemes))
	for _, t := range tideui.BuiltinThemes {
		out = append(out, t.Name)
	}
	return out
}

// legacy holds the accent names TideSMS used before it adopted TideUI's themes.
// Configurations and contacts written then keep working: the name is applied as
// an accent over the base palette instead of being rejected.
var legacy = map[string]lipgloss.Color{
	"tide": "#89dceb", "rose": "#f5a3bb", "ocean": "#89b4fa", "violet": "#cba6f7",
	"amber": "#f9c97b", "mint": "#a6e3bd", "mono": "#cdd6f4",
}

// accents distinguish people who have no theme of their own, without changing
// the palette around them.
var accents = []lipgloss.Color{"#89dceb", "#f5a3bb", "#89b4fa", "#cba6f7", "#f9c97b", "#a6e3bd"}

func Valid(name string) bool {
	if _, ok := legacy[name]; ok {
		return true
	}
	_, ok := tideui.ThemeByName(name)
	return ok
}

// accented applies one color everywhere an accent is used, keeping the rest of
// the palette intact.
func accented(base tideui.Theme, name string, c lipgloss.Color) tideui.Theme {
	out := tideui.ThemeOverrides{Accent: c}.Apply(base)
	out.Unread = c
	if name != "" {
		out.Name = name
	}
	return out
}

// Base returns the palette a theme name selects, treating a legacy accent name
// as an accent over TideUI's default.
func Base(name string) tideui.Theme {
	if t, ok := tideui.ThemeByName(name); ok {
		return t
	}
	fallback, _ := tideui.ThemeByName("catppuccin-mocha")
	if c, ok := legacy[name]; ok {
		return accented(fallback, name, c)
	}
	return fallback
}

// Resolve picks the palette for a conversation. An explicit theme, whether on a
// contact or a thread, wins and is used whole. Without one the global palette is
// kept and only its accent is derived from the identity, so people stay
// distinguishable without the interface changing colour around them.
func Resolve(global, override, identity string) tideui.Theme {
	base := Base(global)
	if override != "" {
		if full, ok := tideui.ThemeByName(override); ok {
			return full
		}
		if c, ok := legacy[override]; ok {
			return accented(base, override, c)
		}
	}
	if identity != "" {
		h := fnv.New32a()
		_, _ = h.Write([]byte(identity))
		return accented(base, "", accents[int(h.Sum32())%len(accents)])
	}
	return base
}
