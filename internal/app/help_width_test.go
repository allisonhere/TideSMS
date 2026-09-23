package app

import (
	"testing"

	"github.com/allisonhere/tidesms/internal/themes"
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/x/ansi"
)

// The shortcut list is only useful if it can be read. The modal caps at 66
// columns, so a line written wider than that is silently cut mid-word.
func TestHelpLinesFitTheModal(t *testing.T) {
	r := tideui.NewRenderer(themes.Base("catppuccin-mocha"), tideui.StyleOptions{})
	const width = 66 - 4
	for _, history := range []bool{false, true} {
		for _, line := range helpSections(r, history, width) {
			if w := ansi.StringWidth(line); w > width {
				t.Errorf("history=%v: %d columns, room for %d: %q", history, w, width, ansi.Strip(line))
			}
		}
	}
}

// Every group is titled, and no group is empty.
func TestHelpGroupsAreTitled(t *testing.T) {
	for _, groups := range [][]helpGroup{composeHelp(), historyHelp()} {
		for _, g := range groups {
			if g.title == "" {
				t.Error("an untitled group")
			}
			if len(g.lines) == 0 {
				t.Errorf("group %q has no lines", g.title)
			}
		}
	}
}
