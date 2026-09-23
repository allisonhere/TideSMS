package themes

import (
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/lipgloss"
	"hash/fnv"
)

// Names lists the themes offered in pickers: TideUI's own palettes, which carry
// a full set of colors rather than an accent alone.
var Names = builtinNames()

// "omarchy" leads the list on a machine running Omarchy, where matching the
// desktop is the likeliest wish; elsewhere it is not offered, though a config
// that names it still loads.
func builtinNames() []string {
	out := make([]string, 0, len(tideui.BuiltinThemes)+1)
	if OmarchyAvailable() {
		out = append(out, Omarchy)
	}
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

// byName finds a full palette: the live Omarchy one, or one of TideUI's. An
// "omarchy" that cannot be read falls back to the default palette, so it is
// still reported as found.
func byName(name string) (tideui.Theme, bool) {
	if name == Omarchy {
		if t, ok := omarchyTheme(); ok {
			return t, true
		}
		t, _ := tideui.ThemeByName("catppuccin-mocha")
		t.Name = Omarchy
		return t, true
	}
	return tideui.ThemeByName(name)
}

func Valid(name string) bool {
	if _, ok := legacy[name]; ok {
		return true
	}
	_, ok := byName(name)
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
	if t, ok := byName(name); ok {
		return t
	}
	fallback, _ := byName("catppuccin-mocha")
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
		if full, ok := byName(override); ok {
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

// Accent returns the highlight colour a named theme paints with, which is what
// a list uses to show at a glance which palette a contact carries. An empty or
// unrecognised name returns "", meaning the row has no accent of its own and
// should be left in the surrounding palette: Base falls back to a default
// theme, and tinting every row with that would say a contact has a theme when
// it has none.
func Accent(name string) lipgloss.Color {
	if name == "" || !Valid(name) {
		return ""
	}
	return Base(name).BorderFocus
}

// First returns the first of the given names that is a real theme, so a caller
// can express a fallback order in one expression. It is what lets a row say
// "this conversation's palette, or failing that its bubble palette" without
// repeating the validity check at each step.
func First(names ...string) string {
	for _, name := range names {
		if name != "" && Valid(name) {
			return name
		}
	}
	return ""
}
