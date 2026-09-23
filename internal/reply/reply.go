// Package reply is a small full-screen window for answering one conversation:
// its recent messages above a composer. It is what `tidesms reply ID` runs, so
// another program — TideDeck, when a message row is opened — can hand over the
// terminal for a reply and get it back when the reply is sent or abandoned.
//
// It shares the app's drafts, so a reply left unfinished here is waiting in
// TideSMS, and one started there is here; and it sends as the app does, with
// the same undo window.
package reply

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/allisonhere/tidesms/internal/api"
	"github.com/allisonhere/tidesms/internal/backend"
	"github.com/allisonhere/tidesms/internal/config"
	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tidesms/internal/keys"
	"github.com/allisonhere/tidesms/internal/storage"
	"github.com/allisonhere/tidesms/internal/themes"
	"github.com/allisonhere/tidesms/ui/composer"
	"github.com/allisonhere/tidesms/ui/conversation"
	"github.com/allisonhere/tideui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Store is what the window reads and writes.
type Store interface {
	api.Store
	Drafts() (map[string]storage.Draft, error)
	SaveDraft(key string, d storage.Draft) error
}

// history is how many recent messages are shown.
const history = 30

// Model is the reply window.
type Model struct {
	ctx     context.Context
	store   Store
	backend backend.MessagingBackend
	cfg     config.Config
	thread  domain.Thread
	msgs    []domain.Message
	view    conversation.Model
	editor  composer.Model
	draft   storage.Draft

	width, height int
	notice        string
	failed        bool
	// sending is set from Enter until the backend answers; holdUntil is the
	// end of the undo window before that.
	sending   bool
	holdUntil time.Time
	holdSeq   int
	done      bool
	// Sent reports that a message went, for the caller's exit status.
	Sent bool
}

type sentMsg struct{ err error }
type holdMsg struct{ seq int }
type closeMsg struct{}

// New opens the window on a conversation, marking it read as opening it in
// the app does.
func New(ctx context.Context, s Store, b backend.MessagingBackend, cfg config.Config, id string) (*Model, error) {
	t, err := api.FindThread(s, id)
	if err != nil {
		return nil, err
	}
	if len(domain.DedupeParticipants(t.Participants)) != 1 {
		return nil, fmt.Errorf("%s is a group; group conversations can't be replied to through KDE Connect", t.DisplayName)
	}
	ms, err := s.Messages(id, history)
	if err != nil {
		return nil, err
	}
	_ = api.MarkRead(s, id)
	m := &Model{ctx: ctx, store: s, backend: b, cfg: cfg, thread: t, msgs: ms, view: conversation.New(), editor: composer.New(cfg.Composer.Mode)}
	if ds, err := s.Drafts(); err == nil {
		m.draft = ds[id]
		m.editor.SetValue(m.draft.Body)
	}
	m.view.SetMessages(ms)
	m.editor.Focus(true)
	return m, nil
}

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) renderer() tideui.Renderer {
	return tideui.NewRenderer(themes.Resolve(m.cfg.General.Theme, "", ""), tideui.StyleOptions{PaneCorners: tideui.RoundCorners})
}

// editorRows is the composer's height.
const editorRows = 4

func (m *Model) layout() {
	w := max(1, m.width-2)
	// Context line, rule, composer, rule, hint and notice.
	h := max(1, m.height-4-1-editorRows-3)
	names := map[string]string{}
	for _, p := range m.thread.Participants {
		names[p.Number] = p.Name
	}
	c := m.cfg.Conversation
	m.view.Layout(m.renderer(), w, h, conversation.Options{Dates: c.ShowDateSeparators, MaxWidth: c.MaxWidth, Bubbles: c.Bubbles,
		Corners: c.Corners, Fill: c.FillBubbles, Names: names, Timestamps: c.Timestamps})
	m.view.Newest()
	m.editor.Size(w, editorRows)
}

// Update handles one event.
func (m *Model) Update(raw tea.Msg) (tea.Model, tea.Cmd) {
	msg := keys.Normalize(raw)
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = v.Width, v.Height
		m.layout()
		return m, nil
	case keys.Action:
		if v == keys.ActionNewline && !m.sending {
			m.editor.InsertString("\n")
		}
		return m, nil
	case holdMsg:
		if v.seq != m.holdSeq || !m.sending || m.holdUntil.IsZero() {
			return m, nil
		}
		if time.Until(m.holdUntil) > 0 {
			return m, m.tick()
		}
		return m, m.deliver()
	case sentMsg:
		m.sending = false
		if v.err != nil {
			// The reply stays in the composer, and in the app as a failed
			// message it can retry.
			m.editor.Focus(true)
			m.notice, m.failed = v.err.Error(), true
			return m, nil
		}
		m.Sent = true
		m.draft.Body = ""
		m.draft.Revision++
		_ = m.store.SaveDraft(m.thread.ID, m.draft)
		m.notice, m.failed = "✓ Submitted · delivery unverified", false
		return m, tea.Tick(700*time.Millisecond, func(time.Time) tea.Msg { return closeMsg{} })
	case closeMsg:
		m.done = true
		return m, tea.Quit
	case composer.CancelMsg:
		// Vim's :q, or a clean second Esc: leave, keeping the draft.
		m.saveDraft()
		m.done = true
		return m, tea.Quit
	case tea.KeyMsg:
		return m, m.key(v)
	}
	return m, nil
}

func (m *Model) key(k tea.KeyMsg) tea.Cmd {
	s := k.String()
	if m.sending {
		// Inside the undo window Esc takes it back; Enter skips the wait.
		if !m.holdUntil.IsZero() {
			switch s {
			case "esc", "ctrl+z":
				m.sending, m.holdUntil = false, time.Time{}
				m.holdSeq++
				m.editor.Focus(true)
				m.notice, m.failed = "Not sent · your reply is kept", false
			case "enter", "ctrl+enter", "f12":
				return m.deliver()
			}
		}
		return nil
	}
	switch {
	case s == "ctrl+enter" || s == "f12" || (s == "enter" && m.cfg.Composer.EnterSends) || (s == "alt+enter" && !m.cfg.Composer.EnterSends):
		return m.send()
	case s == "alt+enter":
		m.editor.InsertString("\n")
		m.saveDraft()
		return nil
	case s == "alt+esc" || (s == "esc" && m.cfg.Composer.Mode != "vim") || s == "ctrl+q":
		// Leaving keeps what was typed, as a draft the app shows too.
		m.saveDraft()
		m.done = true
		return tea.Quit
	}
	cmd, err := m.editor.Update(k)
	if err != nil {
		m.notice, m.failed = "Clipboard unavailable; check wl-clipboard or xclip", true
	}
	m.saveDraft()
	return cmd
}

func (m *Model) saveDraft() {
	if m.editor.Value() == m.draft.Body {
		return
	}
	m.draft.Body = m.editor.Value()
	m.draft.Revision++
	_ = m.store.SaveDraft(m.thread.ID, m.draft)
}

// send starts the undo window, or sends at once when it is off.
func (m *Model) send() tea.Cmd {
	if strings.TrimSpace(m.editor.Value()) == "" {
		m.notice, m.failed = "Write a message before sending", true
		return nil
	}
	m.saveDraft()
	m.sending = true
	m.editor.Focus(false)
	if secs := m.cfg.Composer.UndoSeconds; secs > 0 {
		m.holdSeq++
		m.holdUntil = time.Now().Add(time.Duration(secs) * time.Second)
		return m.tick()
	}
	return m.deliver()
}

func (m *Model) tick() tea.Cmd {
	left := time.Until(m.holdUntil)
	secs := int((left + time.Second - 1) / time.Second)
	m.notice, m.failed = fmt.Sprintf("Sending in %ds · Esc undo · Enter send now", secs), false
	seq := m.holdSeq
	return tea.Tick(left-time.Duration(secs-1)*time.Second, func(time.Time) tea.Msg { return holdMsg{seq} })
}

func (m *Model) deliver() tea.Cmd {
	m.holdUntil = time.Time{}
	m.holdSeq++
	m.notice, m.failed = "Sending…", false
	ctx, s, b, id, text := m.ctx, m.store, m.backend, m.thread.ID, m.editor.Value()
	return func() tea.Msg {
		_, err := api.Send(ctx, s, b, id, text, nil)
		return sentMsg{err}
	}
}

func (m *Model) numbers() []string {
	var out []string
	for _, p := range domain.DedupeParticipants(m.thread.Participants) {
		out = append(out, p.Number)
	}
	return out
}

// View draws the window.
func (m *Model) View() string {
	if m.done || m.width == 0 {
		return ""
	}
	r := m.renderer()
	w := max(1, m.width-2)
	// The pane's title names the person; the line under it says where the
	// reply goes.
	lines := []string{r.Styles.DetailMeta.Render("Replying to " + strings.Join(m.numbers(), ", "))}
	lines = append(lines, strings.Split(m.view.View(r, false), "\n")...)
	lines = append(lines, r.Styles.Badge.Render(ansi.Truncate("▸ Reply "+strings.Repeat("─", w), w, "")))
	ed := strings.Split(m.editor.View(r.Styles.Theme.BorderFocus), "\n")
	for len(ed) < editorRows {
		ed = append(ed, "")
	}
	lines = append(lines, ed[:editorRows]...)
	hint := "Enter send · Shift+Enter newline · Esc keep as draft and close"
	if !m.cfg.Composer.EnterSends {
		hint = "Ctrl+Enter/F12 send · Enter newline · Esc keep as draft and close"
	}
	notice := r.Styles.StatusSuccess.Render(m.notice)
	if m.failed {
		notice = r.Styles.StatusError.Render(m.notice)
	}
	lines = append(lines, r.Styles.DetailMeta.Render(strings.Repeat("─", w)), r.Styles.DetailMeta.Render(ansi.Truncate(hint, w, "…")), notice)
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], w, "")
	}
	pane := tideui.Pane{Title: "TideSMS · " + m.thread.DisplayName, Content: strings.Join(lines, "\n"), Focused: true}
	return r.Render(tideui.Layout{Width: m.width, Height: m.height, Mode: tideui.Tabbed, Panes: [3]tideui.Pane{pane}})
}
