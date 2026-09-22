// Package search parses the small query language used by global message
// search and renders it for SQLite's full-text index. It performs no I/O.
package search

import (
	"strings"
	"time"
)

// Query is a parsed global search: free text plus optional filters.
type Query struct {
	// Text is the full-text portion. An empty Text means "match any body".
	Text   string
	From   string
	Before time.Time
	After  time.Time
}

// Empty reports whether the query asks for nothing at all.
func (q Query) Empty() bool {
	return q.Text == "" && q.From == "" && q.Before.IsZero() && q.After.IsZero()
}

// Parse reads a query string. Recognised filters are from:, before: and after:;
// everything else is free text. Quoted phrases are kept together so
// `"dentist appointment"` searches as one phrase.
func Parse(raw string) Query {
	var q Query
	for _, tok := range tokenize(raw) {
		lower := strings.ToLower(tok)
		switch {
		case strings.HasPrefix(lower, "from:") && len(tok) > len("from:"):
			q.From = strings.TrimSpace(tok[len("from:"):])
		case strings.HasPrefix(lower, "before:"):
			if t, ok := parseDate(tok[len("before:"):]); ok {
				q.Before = t
			}
		case strings.HasPrefix(lower, "after:"):
			if t, ok := parseDate(tok[len("after:"):]); ok {
				q.After = t
			}
		default:
			if q.Text == "" {
				q.Text = tok
			} else {
				q.Text += " " + tok
			}
		}
	}
	return q
}

// FilterOnly reports whether the query has no text terms but valid filters.
func (q Query) FilterOnly() bool { return q.Text == "" && !q.Empty() }

// MatchExpression turns the free text into an FTS5 MATCH expression. Each term
// is quoted so punctuation cannot be read as FTS syntax. It returns "" when
// there is no text to match.
func MatchExpression(text string) string {
	terms := tokenize(text)
	if len(terms) == 0 {
		return ""
	}
	parts := make([]string, 0, len(terms))
	for _, t := range terms {
		// A token may arrive either bare or wrapped in quotes from the original
		// input; either way it becomes one quoted FTS term, so spaces inside a
		// phrase are kept and punctuation cannot act as FTS syntax.
		inner := strings.Trim(t, `"`)
		parts = append(parts, `"`+strings.ReplaceAll(inner, `"`, `""`)+`"`)
	}
	return strings.Join(parts, " ")
}

// tokenize splits on whitespace but keeps a double-quoted span together,
// including its quotes, so the phrase boundary survives into MatchExpression.
// An unterminated quote runs to the end of the input.
func tokenize(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == '"':
			if inQuote {
				cur.WriteRune(r)
				flush()
				inQuote = false
				continue
			}
			flush()
			inQuote = true
			cur.WriteRune(r)
		case isSpace(r) && !inQuote:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

func isSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r'
}

// parseDate accepts a plain calendar date in local time. before: is exclusive
// of midnight, after: is inclusive, which matches how people read the words.
func parseDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// Result is one matching message with enough thread context to open it.
type Result struct {
	MessageID  string
	DeviceID   string
	ThreadID   string
	Sender     string
	Body       string
	ThreadName string
	Timestamp  time.Time
}
