package api

import (
	"fmt"
	"strings"
	"time"
)

// TideDeck panels are programs that print a document of rows. This builds that
// document from the conversation list, in the shape TideDeck's mail panel
// uses, so messages sit on a deck the way mail does: each row names who and
// how long ago, the message sits under it, unread rows are bright and read
// ones muted, and the unread total is the pane's badge. Every row carries the
// conversation id, which TideDeck hands back to `tidesms --open` and
// `tidesms reply` when a row is opened.

// DeckRow is one row of a TideDeck document.
type DeckRow struct {
	Type     string   `json:"type"`
	ID       string   `json:"id,omitempty"`
	Label    string   `json:"label,omitempty"`
	Value    string   `json:"value,omitempty"`
	Body     []string `json:"body,omitempty"`
	Tone     string   `json:"tone,omitempty"`
	BodyTone string   `json:"bodyTone,omitempty"`
}

// DeckBadge is the pane's badge.
type DeckBadge struct {
	Text string `json:"text"`
	Tone string `json:"tone"`
}

// DeckDoc is a TideDeck plugin document.
type DeckDoc struct {
	SchemaVersion int        `json:"schemaVersion"`
	Rows          []DeckRow  `json:"rows"`
	Badge         *DeckBadge `json:"badge,omitempty"`
}

// Deck builds the panel for a conversation list. unreadOnly says the list was
// filtered to unread conversations, which changes what an empty one means.
func Deck(ts Threads, now time.Time, unreadOnly bool) DeckDoc {
	doc := DeckDoc{SchemaVersion: 1, Rows: []DeckRow{}}
	if ts.Unread > 0 {
		doc.Badge = &DeckBadge{Text: fmt.Sprint(ts.Unread), Tone: "warning"}
	}
	if len(ts.Threads) == 0 {
		// An empty panel is the one a new reader sees, so it says why.
		value := "no conversations yet"
		if unreadOnly {
			value = "nothing new"
		}
		doc.Rows = append(doc.Rows, DeckRow{Type: "text", Label: "messages", Value: value, Tone: "muted"})
		return doc
	}
	for i, t := range ts.Threads {
		if i > 0 {
			doc.Rows = append(doc.Rows, DeckRow{Type: "spacer"})
		}
		label := t.Name + " · " + Age(now.Sub(t.LastTime))
		tone := "muted"
		if t.Unread > 0 {
			label += fmt.Sprintf(" · %d new", t.Unread)
			tone = "good"
		}
		body := strings.Join(strings.Fields(t.LastMessage), " ")
		if body == "" {
			body = "(no text)"
		}
		doc.Rows = append(doc.Rows, DeckRow{Type: "block", ID: t.ID, Label: label, Body: []string{body}, Tone: tone})
	}
	return doc
}

// DeckError is the panel when the list cannot be read. A panel never goes
// blank: it says what went wrong, so "not set up" reads differently from
// "broken".
func DeckError(what, detail string) DeckDoc {
	doc := DeckDoc{SchemaVersion: 1, Rows: []DeckRow{{Type: "text", Label: "messages", Value: what, Tone: "warning"}}}
	if detail != "" {
		doc.Rows = append(doc.Rows, DeckRow{Type: "block", Body: []string{detail}, BodyTone: "muted"})
	}
	return doc
}

// Age is how long ago, as briefly as a panel row needs.
func Age(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	return fmt.Sprintf("%dmo", int(d.Hours()/24/30))
}
