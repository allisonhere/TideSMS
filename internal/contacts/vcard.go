package contacts

import (
	"strconv"
	"strings"
)

// Synced is one telephone number imported from a phone's address book. It is
// deliberately not a Contact: synced entries are a read-only overlay that never
// replaces or edits what the user created locally.
type Synced struct{ UID, Name, PhoneNumber, RawNumber string }

// ParseVCards extracts the display name and every telephone number from one or
// more vCards. Android's contacts provider emits vCard 2.1, which folds long
// values with a trailing "=" and encodes non-ASCII text as quoted-printable;
// desktop exporters emit 3.0 or 4.0 with indented folding. Both are accepted,
// as are grouped properties such as "item1.TEL". Anything unrecognised is
// skipped rather than guessed at, and a card with no usable number yields
// nothing.
func ParseVCards(uid string, data []byte) []Synced {
	var out []Synced
	for _, card := range splitCards(string(data)) {
		name, cardUID := "", uid
		var numbers []string
		for _, line := range unfold(card) {
			property, params, value := splitLine(line)
			if property == "" {
				continue
			}
			if hasParam(params, "ENCODING", "QUOTED-PRINTABLE") {
				value = decodeQuotedPrintable(value)
			}
			switch property {
			case "FN":
				if v := SafeLabel(strings.TrimSpace(value)); v != "" {
					name = v
				}
			case "N":
				if name == "" {
					name = structuredName(value)
				}
			case "ORG":
				if name == "" {
					name = SafeLabel(strings.TrimSpace(strings.ReplaceAll(value, ";", " ")))
				}
			case "TEL":
				numbers = append(numbers, strings.TrimSpace(value))
			case "UID":
				if v := strings.TrimSpace(value); v != "" {
					cardUID = v
				}
			}
		}
		seen := map[string]bool{}
		for _, raw := range numbers {
			number, err := Normalize(raw)
			if err != nil || seen[number] {
				continue
			}
			seen[number] = true
			out = append(out, Synced{UID: cardUID, Name: name, PhoneNumber: number, RawNumber: raw})
		}
	}
	return out
}

func splitCards(s string) []string {
	var cards []string
	for _, part := range strings.Split(s, "BEGIN:VCARD") {
		if strings.TrimSpace(part) != "" {
			cards = append(cards, part)
		}
	}
	return cards
}

// unfold joins continuation lines. vCard 3.0 marks them by leading whitespace;
// vCard 2.1 quoted-printable values instead end the line with "=".
func unfold(card string) []string {
	var lines []string
	quoted := false
	for _, raw := range strings.Split(strings.ReplaceAll(card, "\r\n", "\n"), "\n") {
		raw = strings.TrimSuffix(raw, "\r")
		switch {
		case len(lines) > 0 && quoted && strings.HasSuffix(lines[len(lines)-1], "="):
			lines[len(lines)-1] = strings.TrimSuffix(lines[len(lines)-1], "=") + strings.TrimLeft(raw, " \t")
		case len(lines) > 0 && (strings.HasPrefix(raw, " ") || strings.HasPrefix(raw, "\t")):
			lines[len(lines)-1] += raw[1:]
		default:
			lines = append(lines, raw)
			property, params, _ := splitLine(raw)
			quoted = property != "" && hasParam(params, "ENCODING", "QUOTED-PRINTABLE")
		}
	}
	return lines
}

// splitLine returns the upper-case property name without any group prefix, its
// parameters, and the raw value.
func splitLine(line string) (string, []string, string) {
	colon := strings.Index(line, ":")
	if colon < 0 {
		return "", nil, ""
	}
	fields := strings.Split(line[:colon], ";")
	property := strings.ToUpper(strings.TrimSpace(fields[0]))
	if dot := strings.LastIndex(property, "."); dot >= 0 {
		property = property[dot+1:]
	}
	return property, fields[1:], line[colon+1:]
}
func hasParam(params []string, key, value string) bool {
	for _, p := range params {
		p = strings.ToUpper(strings.TrimSpace(p))
		if p == value || p == key+"="+value {
			return true
		}
	}
	return false
}

// structuredName renders N:Family;Given;Middle;Prefix;Suffix as a display name.
func structuredName(value string) string {
	parts := strings.Split(value, ";")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	order := []int{3, 1, 2, 0, 4}
	var words []string
	for _, i := range order {
		if i < len(parts) && parts[i] != "" {
			words = append(words, parts[i])
		}
	}
	return SafeLabel(strings.Join(words, " "))
}

// decodeQuotedPrintable decodes =XX escapes, assuming the UTF-8 charset that
// KDE Connect's Android client uses. Malformed escapes are left as written.
func decodeQuotedPrintable(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '=' || i+2 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		n, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
		if err != nil {
			b.WriteByte(s[i])
			continue
		}
		b.WriteByte(byte(n))
		i += 2
	}
	return b.String()
}
