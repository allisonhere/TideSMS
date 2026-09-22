// Package sms estimates how a message will be split into SMS segments. The
// encoding matters: a single character outside the GSM 7-bit alphabet, or any
// emoji, forces the whole message to UCS-2 and roughly halves the capacity.
package sms

// Estimate is a message's length in one encoding.
type Estimate struct {
	// Units is the encoding units used: septets for GSM-7, UTF-16 code units
	// for UCS-2. An extended GSM character counts as two septets, and a rune
	// outside the BMP counts as two UTF-16 units.
	Units int
	// Segments is how many SMS parts the message will take.
	Segments int
	// Unicode reports whether UCS-2 encoding is required.
	Unicode bool
	// Remaining is how many units fit in the last segment before it splits
	// again.
	Remaining int
}

// Count classifies the text and returns its segment estimate. Empty text is
// one segment with full capacity remaining.
func Count(text string) Estimate {
	unicode := false
	for _, r := range text {
		if !gsm7(r) {
			unicode = true
			break
		}
	}
	if unicode {
		return estimate(text, 70, 67, true)
	}
	units := 0
	for _, r := range text {
		if gsm7Extended(r) {
			units += 2
		} else {
			units++
		}
	}
	return finish(units, 160, 153, false)
}

func estimate(text string, single, multi int, unicode bool) Estimate {
	units := 0
	for _, r := range text {
		if r > 0xFFFF {
			units += 2
		} else {
			units++
		}
	}
	return finish(units, single, multi, unicode)
}

func finish(units, single, multi int, unicode bool) Estimate {
	segments := 1
	capacity := single
	if units > single {
		capacity = multi
		segments = (units + multi - 1) / multi
	}
	remaining := capacity
	if segments > 0 {
		used := units - (segments-1)*capacity
		remaining = capacity - used
	}
	return Estimate{Units: units, Segments: segments, Unicode: unicode, Remaining: remaining}
}

// gsm7 reports whether r is in the GSM 03.38 basic alphabet. Newlines and
// carriage returns are valid.
func gsm7(r rune) bool {
	if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
		return true
	}
	if _, ok := gsm7Basic[r]; ok {
		return true
	}
	return gsm7Extended(r)
}

func gsm7Extended(r rune) bool {
	_, ok := gsm7Ext[r]
	return ok
}

var gsm7Basic = map[rune]bool{
	'@': true, '£': true, '$': true, '¥': true, 'è': true, 'é': true, 'ù': true, 'ì': true,
	'ò': true, 'Ç': true, '\n': true, 'Ø': true, 'ø': true, '\r': true, 'Å': true, 'å': true,
	'Δ': true, '_': true, 'Φ': true, 'Γ': true, 'Λ': true, 'Ω': true, 'Π': true, 'Ψ': true,
	'Σ': true, 'Θ': true, 'Ξ': true, 'Æ': true, 'æ': true, 'ß': true, 'É': true, ' ': true,
	'!': true, '"': true, '#': true, '¤': true, '%': true, '&': true, '\'': true, '(': true,
	')': true, '*': true, '+': true, ',': true, '-': true, '.': true, '/': true, ':': true,
	';': true, '<': true, '=': true, '>': true, '?': true, '¡': true, 'Ä': true, 'Ö': true,
	'Ñ': true, 'Ü': true, '§': true, '¿': true, 'ä': true, 'ö': true, 'ñ': true, 'ü': true,
	'à': true,
}

var gsm7Ext = map[rune]bool{
	'^': true, '{': true, '}': true, '\\': true, '[': true, '~': true, ']': true, '|': true, '€': true,
}
