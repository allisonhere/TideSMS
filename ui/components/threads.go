package components

import (
	"fmt"
	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tidesms/internal/themes"
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"time"
)

// Threads renders the sidebar list. themeFor gives a thread's palette name,
// keyed by thread id, so a row shows the colour its conversation will open in;
// a thread absent from the map has no theme of its own. drafts holds the
// unsent text of threads that have any, which the row shows in place of the
// last message so a half-written reply is not forgotten.
func Threads(r tideui.Renderer, ts []domain.Thread, selected string, w, h int, themeFor, drafts map[string]string) string {
	if len(ts) == 0 {
		return r.Styles.DetailMeta.Render("No cached threads yet.\nRefresh when your phone is online.")
	}
	idx := 0
	for i, t := range ts {
		if t.ID == selected {
			idx = i
		}
	}
	count := max(1, (h-1)/3)
	start := max(0, idx-count+1)
	rows := []string{}
	for i := start; i < min(len(ts), start+count); i++ {
		t := ts[i]
		mark := "  "
		suffix := t.LastTimestamp.Local().Format("15:04")
		if t.LastTimestamp.Local().Format("2006-01-02") != time.Now().Format("2006-01-02") {
			suffix = t.LastTimestamp.Local().Format("Jan 2")
		}
		// Unread threads keep their time: a count alone does not say whether
		// the messages are from a minute ago or last week.
		if t.UnreadCount > 0 {
			mark = "● "
			suffix = fmt.Sprintf("%d · %s", t.UnreadCount, suffix)
		}
		preview := contacts.SafeLabel(t.LastMessage)
		// Media stored before previews named it arrives with no text at all.
		if preview == "" {
			preview = "Attachment"
		}
		const draftLabel = "Draft: "
		// Collapse line breaks to spaces before control characters are stripped,
		// or a two-line draft runs its words together.
		draft := contacts.SafeLabel(strings.Join(strings.Fields(drafts[t.ID]), " "))
		if draft != "" {
			preview = draftLabel + draft
		}
		// The draft label is drawn in the accent so it reads as a state of the
		// thread rather than as something the other person wrote.
		styled := func(s string) string {
			if draft != "" && strings.HasPrefix(s, draftLabel) {
				return r.Styles.Badge.Render(draftLabel) + r.Styles.DetailMeta.Render(strings.TrimPrefix(s, draftLabel))
			}
			return r.Styles.DetailMeta.Render(s)
		}
		// Leave room for the marker, the suffix and a gap, so a long participant
		// list is elided rather than butting up against the timestamp.
		theme := themeFor[t.ID]
		if theme == "" || !themes.Valid(theme) {
			name := ansi.Truncate(contacts.SafeLabel(t.DisplayName), max(1, w-len(mark)-ansi.StringWidth(suffix)-2), "…")
			rows = append(rows, r.RenderRow(tideui.Row{Prefix: mark, Text: name, Suffix: suffix, Selected: t.ID == selected}, w), "  "+styled(ansi.Truncate(preview, max(1, w-2), "…")), "")
			continue
		}
		// A themed thread is two bands the width of the pane: the name and
		// time on the accent, the preview on the palette's own background, so
		// the whole entry reads as that person's conversation. The bands hide
		// the selection colour, so the selected entry is marked in the first
		// column instead, beside the unread dot.
		lead := " "
		if t.ID == selected {
			lead = "▸"
		}
		unread := " "
		if t.UnreadCount > 0 {
			unread = "●"
		}
		head := lead + unread + " "
		room := max(1, w-ansi.StringWidth(head)-ansi.StringWidth(suffix)-2)
		name := ansi.Truncate(contacts.SafeLabel(t.DisplayName), room, "…")
		gap := strings.Repeat(" ", max(1, w-ansi.StringWidth(head+name+suffix)-1))
		body := lead + "  " + ansi.Truncate(preview, max(1, w-4), "…")
		rows = append(rows, band(head+name+gap+suffix, theme, true, w), band(body, theme, false, w), "")
	}
	for len(rows) < h-1 {
		rows = append(rows, "")
	}
	rows = append(rows, r.Styles.DetailMeta.Render(ansi.Truncate(fmt.Sprintf("%d threads · Enter open · i details · c contacts", len(ts)), w, "…")))
	return strings.Join(rows, "\n")
}
