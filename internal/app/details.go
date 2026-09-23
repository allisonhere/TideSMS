package app

import (
	"fmt"
	"strings"

	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/domain"
	"github.com/allisonhere/tidesms/internal/storage"
	"github.com/allisonhere/tidesms/internal/themes"
	"github.com/allisonhere/tideui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The details screen is a conversation's own settings, as Google Messages has
// them: its colours, notifications and privacy, reached from the chat rather
// than from the global panel. It always describes the open conversation, so the
// preview, the AI policy picker and the mute toggle all have one target.
//
// A one-to-one chat saves to the person, so their colours follow them to every
// list; a group has no single person, so it saves to the thread. Like the
// settings panel it edits a draft: changes preview at once, Ctrl+S saves, and
// Esc puts back what was saved.

type detailsRow int

const (
	detailsTheme detailsRow = iota
	detailsIn
	detailsOut
	detailsNotify
	detailsAI
	detailsRename
	detailsCopy
)

// detailsState is the draft. Theme and bubble values are indexes into
// contactThemeNames(), whose first entry is "automatic": no override.
type detailsState struct {
	group          bool
	theme, in, out int
	muted          bool
	saved          struct {
		theme, in, out int
		muted          bool
	}
}

type detailsField struct {
	id    detailsRow
	label string
}

// openDetails brings up the screen for the conversation the reader means. From
// the thread list that is the highlighted thread and from the contact list the
// selected person, so either is opened first; elsewhere it is the one already
// open.
func (m *Model) openDetails() tea.Cmd {
	var cmd tea.Cmd
	if m.history.enabled {
		switch m.history.pane {
		case paneThreads:
			if t, ok := m.highlightedThread(); ok && (m.history.active == nil || m.history.active.ID != t.ID) {
				cmd = m.openThread(t)
			}
		case paneContacts:
			if c, ok := m.selectedContact(); ok {
				cmd = m.pickRecipient(c)
			}
		}
	} else if !m.focus {
		if c, ok := m.selectedContact(); ok && !sameContact(c, m.recipient) {
			m.choose(c)
		}
	}
	if m.recipient.PhoneNumber == "" && m.history.active == nil {
		m.notify("Open a conversation first", true)
		return cmd
	}
	d := detailsState{}
	if t := m.history.active; t != nil && (t.IsGroup || len(t.Participants) > 1) {
		d.group = true
		d.theme = contactThemeIndex(t.ThemeID)
		d.in = contactThemeIndex(t.ThemeIn)
		d.out = contactThemeIndex(t.ThemeOut)
	} else {
		// A one-to-one thread may still carry an override of its own from the
		// palette's thread commands. That is what the reader sees, so it is what
		// the screen starts from; saving moves it onto the person.
		c := m.recipient
		d.theme = contactThemeIndex(firstSet(m.activeThreadTheme(), c.Theme))
		d.in = contactThemeIndex(firstSet(m.activeThreadIn(), c.ThemeIn))
		d.out = contactThemeIndex(firstSet(m.activeThreadOut(), c.ThemeOut))
	}
	d.muted = m.detailsMuted(d.group)
	d.saved.theme, d.saved.in, d.saved.out, d.saved.muted = d.theme, d.in, d.out, d.muted
	m.details = d
	m.modal = "details"
	m.choice = 0
	return cmd
}

// highlightedThread is the thread list's highlighted row, which is its first
// until a thread has been chosen.
func (m *Model) highlightedThread() (domain.Thread, bool) {
	ts := m.history.threads
	if len(ts) == 0 {
		return domain.Thread{}, false
	}
	for _, t := range ts {
		if t.ID == m.history.threadSelected {
			return t, true
		}
	}
	return ts[0], true
}

func firstSet(names ...string) string {
	for _, n := range names {
		if n != "" {
			return n
		}
	}
	return ""
}

func (m *Model) activeThreadTheme() string {
	if t := m.history.active; t != nil {
		return t.ThemeID
	}
	return ""
}

func (m *Model) activeThreadIn() string {
	if t := m.history.active; t != nil {
		return t.ThemeIn
	}
	return ""
}

func (m *Model) activeThreadOut() string {
	if t := m.history.active; t != nil {
		return t.ThemeOut
	}
	return ""
}

// detailsMuted reads the stored notification override for the conversation.
func (m *Model) detailsMuted(group bool) bool {
	if m.store == nil {
		return false
	}
	scope, key := m.detailsNotifyKey(group)
	if key == "" {
		return false
	}
	mode, ok, err := m.store.NotificationMode(scope, key)
	return err == nil && ok && mode == notifMuted
}

func (m *Model) detailsNotifyKey(group bool) (string, string) {
	if group {
		if t := m.history.active; t != nil {
			return storage.ScopeThread, t.ID
		}
		return storage.ScopeThread, ""
	}
	return storage.ScopeContact, m.recipient.PhoneNumber
}

func (m *Model) detailsFields() []detailsField {
	fields := []detailsField{
		{detailsTheme, "Theme"},
		{detailsIn, "Incoming bubbles"},
		{detailsOut, "Outgoing bubbles"},
		{detailsNotify, "Notifications"},
		{detailsAI, "AI policy"},
	}
	if !m.details.group {
		label := "Rename"
		if m.recipient.ID == "" {
			label = "Save as contact"
		}
		fields = append(fields, detailsField{detailsRename, label})
	}
	label := "Copy number"
	if m.details.group {
		label = "Copy numbers"
	}
	return append(fields, detailsField{detailsCopy, label})
}

func (m *Model) selectedDetail() (detailsField, bool) {
	fields := m.detailsFields()
	if len(fields) == 0 {
		return detailsField{}, false
	}
	m.choice = max(0, min(m.choice, len(fields)-1))
	return fields[m.choice], true
}

func (m *Model) detailsDirty() bool {
	d := m.details
	return d.theme != d.saved.theme || d.in != d.saved.in || d.out != d.saved.out || d.muted != d.saved.muted
}

// detailsPreview reports the draft's value for one of the colour rows while
// the screen is open. An empty name is "automatic".
func (m *Model) detailsPreview(row detailsRow) (string, bool) {
	if m.modal != "details" {
		return "", false
	}
	names := contactThemeNames()
	switch row {
	case detailsTheme:
		return blankAutomatic(names[m.details.theme]), true
	case detailsIn:
		return blankAutomatic(names[m.details.in]), true
	case detailsOut:
		return blankAutomatic(names[m.details.out]), true
	}
	return "", false
}

func (m *Model) detailsValue(id detailsRow) string {
	names := contactThemeNames()
	switch id {
	case detailsTheme:
		return names[m.details.theme]
	case detailsIn:
		return names[m.details.in]
	case detailsOut:
		return names[m.details.out]
	case detailsNotify:
		if m.details.muted {
			return "muted"
		}
		return "on"
	case detailsAI:
		scope, key := storage.ScopeContact, m.aiContactKey()
		if m.details.group {
			scope, key = storage.ScopeThread, m.aiScopeID(storage.ScopeThread)
		}
		if m.store != nil && key != "" {
			if p, _, _, ok, err := m.store.AIPolicy(scope, key); err == nil && ok {
				return p + " ›"
			}
		}
		return "inherit ›"
	case detailsRename:
		return "›"
	}
	return ""
}

// detailsAdjust changes a row in the draft.
func (m *Model) detailsAdjust(delta int) {
	f, ok := m.selectedDetail()
	if !ok {
		return
	}
	n := len(contactThemeNames())
	switch f.id {
	case detailsTheme:
		m.details.theme = cycleIndex(m.details.theme, delta, n)
	case detailsIn:
		m.details.in = cycleIndex(m.details.in, delta, n)
	case detailsOut:
		m.details.out = cycleIndex(m.details.out, delta, n)
	case detailsNotify:
		m.details.muted = !m.details.muted
	default:
		return
	}
	m.layoutConversation()
}

// detailsKey drives the screen.
func (m *Model) detailsKey(k tea.KeyMsg) tea.Cmd {
	fields := m.detailsFields()
	switch k.String() {
	case "esc", "q":
		m.discardDetails()
		return nil
	case "ctrl+s":
		return m.saveDetails()
	case "up", "k", "ctrl+k":
		m.choice = max(0, m.choice-1)
	case "down", "j", "ctrl+j":
		m.choice = min(len(fields)-1, m.choice+1)
	case "home", "g":
		m.choice = 0
	case "end", "G":
		m.choice = max(0, len(fields)-1)
	case "left", "h":
		m.detailsAdjust(-1)
	case "right", "l":
		m.detailsAdjust(1)
	case "enter", " ":
		return m.detailsActivate()
	}
	return nil
}

// detailsActivate changes a value row as → does and runs an action row. The
// actions open screens of their own, so they wait for the draft to be saved or
// discarded rather than silently dropping it.
func (m *Model) detailsActivate() tea.Cmd {
	f, ok := m.selectedDetail()
	if !ok {
		return nil
	}
	switch f.id {
	case detailsAI, detailsRename:
		if m.detailsDirty() {
			m.notify("Ctrl+S to save these changes first, or Esc to discard them", true)
			return nil
		}
	}
	switch f.id {
	case detailsAI:
		if m.details.group {
			m.openAIPolicyPicker(storage.ScopeThread)
		} else {
			m.openAIPolicyPicker(storage.ScopeContact)
		}
		return nil
	case detailsRename:
		m.editing = m.recipient
		m.openForm("edit", m.recipient)
		return nil
	case detailsCopy:
		if m.details.group && m.history.active != nil {
			numbers := []string{}
			for _, p := range m.history.active.Participants {
				numbers = append(numbers, p.RawNumber)
			}
			return m.copyText(strings.Join(numbers, ", "))
		}
		return m.copyText(m.recipient.PhoneNumber)
	}
	m.detailsAdjust(1)
	return nil
}

// discardDetails closes the screen. The draft only ever lived in the preview,
// so closing is enough to put everything back.
func (m *Model) discardDetails() {
	dirty := m.detailsDirty()
	m.modal = ""
	m.layoutConversation()
	if dirty {
		m.notify("Changes discarded", false)
	}
}

// saveDetails writes what changed and leaves the screen open.
func (m *Model) saveDetails() tea.Cmd {
	if !m.detailsDirty() {
		m.notify("No changes to save", false)
		return nil
	}
	d := m.details
	names := contactThemeNames()
	theme, in, out := blankAutomatic(names[d.theme]), blankAutomatic(names[d.in]), blankAutomatic(names[d.out])
	var cmds []tea.Cmd
	coloursChanged := d.theme != d.saved.theme || d.in != d.saved.in || d.out != d.saved.out
	if coloursChanged {
		if d.group {
			cmd := m.saveThreadColours(theme, in, out)
			if cmd == nil {
				return nil
			}
			cmds = append(cmds, cmd)
		} else {
			local, err := ensureLocal(m.recipient)
			if err != nil {
				m.notify("Could not create contact ID", true)
				return nil
			}
			local.Theme, local.ThemeIn, local.ThemeOut = theme, in, out
			m.busy = true
			store := m.store
			cmds = append(cmds, func() tea.Msg { return mutationMsg{contact: &local, err: store.SaveContact(local), restyle: true} })
			// The screen started from whatever was in effect, a thread override
			// included. Now that the person carries the colours, the override
			// would hide them, so it is cleared.
			if t := m.history.active; t != nil && (t.ThemeID != "" || t.ThemeIn != "" || t.ThemeOut != "") {
				if cmd := m.saveThreadColours("", "", ""); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}
	}
	if d.muted != d.saved.muted && m.store != nil {
		scope, key := m.detailsNotifyKey(d.group)
		muted, store := d.muted, m.store
		cmds = append(cmds, func() tea.Msg {
			if muted {
				return historySavedMsg{store.SetNotificationMode(scope, key, notifMuted)}
			}
			return historySavedMsg{store.ClearNotificationMode(scope, key)}
		})
	}
	m.details.saved.theme, m.details.saved.in, m.details.saved.out, m.details.saved.muted = d.theme, d.in, d.out, d.muted
	m.notify("Saved", false)
	return tea.Batch(cmds...)
}

// saveThreadColours stores a thread's theme and bubble overrides, updating the
// open thread at once as the palette's thread pickers do.
func (m *Model) saveThreadColours(theme, in, out string) tea.Cmd {
	t := m.history.active
	if t == nil || m.history.store == nil {
		return nil
	}
	t.ThemeID, t.ThemeIn, t.ThemeOut = theme, in, out
	m.layoutConversation()
	id, s := t.ID, m.history.store
	return func() tea.Msg {
		if err := s.ThreadTheme(id, theme); err != nil {
			return historySavedMsg{err}
		}
		return historySavedMsg{s.ThreadBubbleThemes(id, in, out)}
	}
}

// detailsTitle names the conversation the screen describes.
func (m *Model) detailsTitle() string {
	name := contacts.SafeLabel(m.recipient.Name)
	if t := m.history.active; t != nil && (m.details.group || name == "") {
		name = contacts.SafeLabel(t.DisplayName)
	}
	title := "Details · " + name
	if m.detailsDirty() {
		title += " · unsaved"
	}
	return title
}

// detailsHeader is the summary above the rows.
func (m *Model) detailsHeader() string {
	if m.details.group && m.history.active != nil {
		return fmt.Sprintf("Group · %d people", len(m.history.active.Participants))
	}
	return m.recipient.PhoneNumber
}

// detailsBody renders the rows the way the settings panel does, with the colour
// rows drawn on the colours they choose.
func (m *Model) detailsBody(r tideui.Renderer, w int) string {
	idx, _ := m.selectedDetail()
	rows := []string{r.Styles.DetailMeta.Render(m.detailsHeader()), ""}
	for i, f := range m.detailsFields() {
		value := m.detailsValue(f.id)
		switch f.id {
		case detailsTheme:
			value = themeSwatch(value)
		case detailsIn, detailsOut:
			value = m.bubbleSwatch(value, f.id == detailsOut)
		}
		rows = append(rows, r.RenderSoftRow(tideui.SoftRow{Text: f.label, Suffix: value, Selected: f.id == idx.id && i == m.choice}, w))
	}
	return strings.Join(rows, "\n")
}

// detailsHint describes the keys.
func (m *Model) detailsHint() string {
	esc := "Esc close"
	if m.detailsDirty() {
		esc = "Esc discard"
	}
	return "↑↓ move · ←→/Enter change · Ctrl+S save · " + esc
}

// themeSwatch draws a theme's name on its accent, as a thread row draws the
// person's name, so the value shows the colour it picks. "automatic" has no
// palette of its own and is left plain.
func themeSwatch(value string) string {
	if value == "automatic" || !themes.Valid(value) {
		return value
	}
	base := themes.Base(value)
	return lipgloss.NewStyle().Background(base.BorderFocus).Foreground(base.Bg).Bold(true).Render(" " + value + " ")
}
