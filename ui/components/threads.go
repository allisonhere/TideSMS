package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/x/ansi"
)

// ThreadView is what the thread list shows.
type ThreadView struct {
	Threads  []domain.Thread
	Selected string // thread id
	Width    int
	Height   int
	// Themes gives a thread's palette name, keyed by thread id, so a row shows
	// the colour its conversation will open in; a thread absent from the map
	// has no theme of its own.
	Themes map[string]string
	// Drafts holds the unsent text of threads that have any, which the row
	// shows in place of the last message so a half-written reply is not
	// forgotten.
	Drafts  map[string]string
	Focused bool
	// Compact drops the blank line between entries so more threads fit.
	Compact bool
}

// Threads renders the sidebar's conversations, pinned ones under their own
// heading, each as a two-line entry with a blank line after it unless the list
// is compact.
func Threads(r tideui.Renderer, v ThreadView) string {
	w, h := v.Width, v.Height
	if len(v.Threads) == 0 {
		return " " + r.Styles.DetailMeta.Render("No conversations cached yet.") + "\n " +
			r.Styles.DetailMeta.Render("They appear once your phone syncs.")
	}
	now := time.Now()
	pinned, unread := 0, 0
	for _, t := range v.Threads {
		if t.Pinned {
			pinned++
		}
		if t.UnreadCount > 0 {
			unread++
		}
	}
	var rows []string
	first, last := 0, 0
	for i, t := range v.Threads {
		// Headings only when there is something to divide: a list with no
		// pinned threads, or only pinned ones, is a single section.
		if pinned > 0 && pinned < len(v.Threads) {
			switch i {
			case 0:
				rows = append(rows, Heading(r, "Pinned", w, true))
			case pinned:
				rows = append(rows, Heading(r, "Recent", w, true))
			}
		}
		selected := t.ID == v.Selected
		if selected {
			first = len(rows)
		}
		rows = append(rows, threadEntry(r, t, v, now, selected)...)
		if selected {
			last = len(rows) - 1
		}
		if !v.Compact {
			rows = append(rows, "")
		}
	}
	summary := fmt.Sprintf("%d threads", len(v.Threads))
	if unread > 0 {
		summary += fmt.Sprintf(" · %d unread", unread)
	}
	// A blank line keeps the footer from reading as part of the last entry.
	body := max(1, h-2)
	out := append(fill(window(rows, first, last, body), body), "", Footer(r, w, summary, "↵ open · i info"))
	return strings.Join(out, "\n")
}

// threadEntry is a thread's two lines: who and when, then what was said last
// and how much of it is unread.
func threadEntry(r tideui.Renderer, t domain.Thread, v ThreadView, now time.Time, selected bool) []string {
	w := v.Width
	p := paintFor(r, selected, v.Focused)
	theme := v.Themes[t.ID]
	label := contacts.SafeLabel(t.DisplayName)
	lead, indent := p.lead(r, w, label, theme)

	// Unread threads keep their time: a count alone does not say whether the
	// messages are from a minute ago or last week.
	stamp := p.dim.Render(when(t.LastTimestamp, now))
	if t.UnreadCount > 0 {
		stamp = p.accent.Render(when(t.LastTimestamp, now))
	}
	stamp += p.base.Render(" ")
	name := p.name(selected, v.Focused, t.UnreadCount > 0, theme).Render(label)
	if t.IsGroup && len(t.Participants) > 0 {
		name += p.dim.Render(fmt.Sprintf(" · %d", len(t.Participants)))
	}
	room := w - ansi.StringWidth(lead) - ansi.StringWidth(stamp) - 1
	if ansi.StringWidth(label) > room {
		name = p.name(selected, v.Focused, t.UnreadCount > 0, theme).Render(ansi.Truncate(label, max(1, room), "…"))
	}
	head := p.line(w, lead+name, stamp)

	preview := contacts.SafeLabel(t.LastMessage)
	// Media stored before previews named it arrives with no text at all.
	if preview == "" {
		preview = "Attachment"
	}
	// Collapse line breaks to spaces before control characters are stripped,
	// or a two-line draft runs its words together.
	draft := contacts.SafeLabel(strings.Join(strings.Fields(v.Drafts[t.ID]), " "))
	badge := ""
	if t.UnreadCount > 0 {
		count := fmt.Sprint(t.UnreadCount)
		if t.UnreadCount > 99 {
			count = "99+"
		}
		badge = p.badge.Render(" "+count+" ") + p.base.Render(" ")
	}
	room = w - ansi.StringWidth(indent) - ansi.StringWidth(badge) - 1
	var text string
	if draft != "" {
		// The draft label is drawn in the accent so it reads as a state of the
		// thread rather than as something the other person wrote.
		const label = "Draft: "
		text = p.accent.Render(label) + p.dim.Render(ansi.Truncate(draft, max(1, room-len(label)), "…"))
	} else {
		style := p.dim
		if t.UnreadCount > 0 {
			style = p.base
		}
		text = style.Render(ansi.Truncate(preview, max(1, room), "…"))
	}
	return []string{head, p.line(w, indent+text, badge)}
}
