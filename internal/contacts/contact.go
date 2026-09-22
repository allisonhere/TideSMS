package contacts

import (
	"fmt"
	"strings"
	"unicode"
)

// A Contact with Synced set came from the phone's address book. It is an
// overlay entry: it has no local identity until the user edits or themes it.
type Contact struct {
	ID, Name, PhoneNumber, Theme string
	// ThemeIn and ThemeOut are the bubble palettes for this contact's received
	// and sent messages. Empty keeps the derived surface.
	ThemeIn, ThemeOut string
	Synced            bool
}

// Source records where a contact entry came from, so a local alias can be shown
// while the imported name and number are retained for reference and merging.
type Source struct {
	ContactID, Source, SourceID string
	DisplayName, PhoneNumber    string
}

const (
	SourceLocal = "local"
	SourcePhone = "phone"
)

// Normalize preserves international prefixes without guessing a country code.
func Normalize(s string) (string, error) {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' && b.Len() == 0:
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '(' || r == ')' || r == '.':
		default:
			return "", fmt.Errorf("use a phone number with digits and an optional leading +")
		}
	}
	n := strings.TrimPrefix(b.String(), "+")
	if len(n) < 3 || len(n) > 15 {
		return "", fmt.Errorf("phone number must contain 3–15 digits")
	}
	return b.String(), nil
}

// SafeLabel prevents names from injecting terminal controls.
func SafeLabel(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

// MatchKey is the trailing national-number portion used to compare numbers that
// were written in different formats, such as a contact saved as 8165550182 and
// an SMS address delivered as +18165550182. It is deliberately only a lookup
// hint: a match is accepted solely when exactly one name shares the key, so two
// unrelated numbers can never be merged just because they look similar.
func MatchKey(number string) string {
	var digits strings.Builder
	for _, r := range number {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	s := digits.String()
	if len(s) < 10 {
		return ""
	}
	return s[len(s)-10:]
}
