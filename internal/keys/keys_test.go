package keys

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"testing"
)

type csi []byte

func (c csi) String() string { return fmt.Sprintf("?CSI%+v?", []byte(c)) }
func TestModifiedKeys(t *testing.T) {
	for _, tc := range []struct{ seq, want string }{{"13;5u", "f12"}, {"27;5;13~", "f12"}, {"13;3u", "alt+enter"}, {"27;3;13~", "alt+enter"}, {"27;3u", "alt+esc"}, {"13u", "enter"}, {"112;5u", "ctrl+p"}, {"27;5;99~", "ctrl+c"}, {"9;2u", "shift+tab"}} {
		got := Normalize(csi(tc.seq))
		k, ok := got.(tea.KeyMsg)
		if !ok || k.String() != tc.want {
			t.Errorf("%s: %v", tc.seq, got)
		}
	}
}
func TestCtrlShiftEnterIsSchedule(t *testing.T) {
	if got := Normalize(csi("13;6u")); got != ActionSchedule {
		t.Fatalf("ctrl+shift+enter = %v, want schedule", got)
	}
	// Ctrl+Enter alone is still the submit fallback.
	got := Normalize(csi("13;5u")).(tea.KeyMsg)
	if got.Type != tea.KeyF12 {
		t.Fatalf("ctrl+enter = %v", got)
	}
}

func TestPasteIsNeverACommand(t *testing.T) {
	k := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\x1b[13;5u"), Paste: true}
	got := Normalize(k).(tea.KeyMsg)
	if !got.Paste || got.Type != tea.KeyRunes {
		t.Fatal("paste became command")
	}
}
