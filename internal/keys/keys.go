// Package keys bridges modified keys that Bubble Tea v1 reports as unknown CSI.
package keys

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"strconv"
	"strings"
)

// Normalize handles Kitty CSI-u and xterm modifyOtherKeys without reinterpreting
// bracketed paste, which Bubble Tea delivers as an ordinary KeyMsg.
func Normalize(msg tea.Msg) tea.Msg {
	if _, ok := msg.(tea.KeyMsg); ok {
		return msg
	}
	s, ok := msg.(fmt.Stringer)
	if !ok {
		return msg
	}
	printed := s.String()
	if !strings.HasPrefix(printed, "?CSI[") || !strings.HasSuffix(printed, "]?") {
		return msg
	}
	var seq []byte
	for _, part := range strings.Fields(strings.TrimSuffix(strings.TrimPrefix(printed, "?CSI["), "]?")) {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || n > 255 {
			return msg
		}
		seq = append(seq, byte(n))
	}
	if len(seq) == 0 {
		return msg
	}
	final := seq[len(seq)-1]
	params := strings.Split(string(seq[:len(seq)-1]), ";")
	code, mod := 0, 1
	number := func(s string) int { v, _ := strconv.Atoi(strings.Split(s, ":")[0]); return v }
	if final == 'u' {
		code = number(params[0])
		if len(params) > 1 {
			mod = number(params[1])
		}
	}
	if final == '~' && len(params) == 3 && params[0] == "27" {
		code = number(params[2])
		mod = number(params[1])
	}
	if mod < 1 {
		return msg
	}
	bits := mod - 1
	alt := bits&2 != 0
	ctrl := bits&4 != 0
	if code == 13 && ctrl {
		return tea.KeyMsg{Type: tea.KeyF12}
	}
	switch code {
	case 27:
		return tea.KeyMsg{Type: tea.KeyEsc, Alt: alt}
	case 13:
		return tea.KeyMsg{Type: tea.KeyEnter, Alt: alt}
	case 9:
		if bits&1 != 0 {
			return tea.KeyMsg{Type: tea.KeyShiftTab}
		}
		return tea.KeyMsg{Type: tea.KeyTab, Alt: alt}
	case 127:
		return tea.KeyMsg{Type: tea.KeyBackspace, Alt: alt}
	}
	if ctrl {
		if code >= 'A' && code <= 'Z' {
			code += 32
		}
		if code >= 'a' && code <= 'z' {
			return tea.KeyMsg{Type: tea.KeyType(code - 'a' + 1), Alt: alt}
		}
	}
	if code >= 32 && code <= 0x10ffff && !ctrl {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{rune(code)}, Alt: alt}
	}
	return msg
}
