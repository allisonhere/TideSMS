package components

import (
	"fmt"
	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"time"
)

// Threads renders the sidebar list. themeFor gives a thread's palette name,
// keyed by thread id, so a row shows the colour its conversation will open in;
// a thread absent from the map has no theme of its own.
func Threads(r tideui.Renderer, ts []domain.Thread, selected string, w, h int, themeFor map[string]string) string {
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
		if t.UnreadCount > 0 {
			mark = "● "
			suffix = fmt.Sprint(t.UnreadCount)
		}
		// Leave room for the marker, the suffix and a gap, so a long participant
		// list is elided rather than butting up against the timestamp.
		name := ansi.Truncate(contacts.SafeLabel(t.DisplayName), max(1, w-len(mark)-ansi.StringWidth(suffix)-2), "…")
		rows = append(rows, r.RenderRow(tideui.Row{Prefix: mark, Text: tinted(name, themeFor[t.ID], t.ID == selected), Suffix: suffix, Selected: t.ID == selected}, w), r.Styles.DetailMeta.Render(ansi.Truncate("  "+contacts.SafeLabel(t.LastMessage), w, "…")), "")
	}
	for len(rows) < h-1 {
		rows = append(rows, "")
	}
	rows = append(rows, r.Styles.DetailMeta.Render(ansi.Truncate(fmt.Sprintf("%d threads · Enter open · c contacts", len(ts)), w, "…")))
	return strings.Join(rows, "\n")
}
