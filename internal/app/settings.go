package app

import (
	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/themes"
	tea "github.com/charmbracelet/bubbletea"
)

// The static settings panel is a single non-scrolling list of fields. Each row
// shows its current value and is changed in place: Enter toggles or cycles a
// value, and the two theme rows cycle with the arrow keys so the palette
// previews live before it is committed with Enter.

type settingID int

const (
	settingTheme settingID = iota
	settingContactTheme
	settingComposer
	settingBubbles
	settingCorners
	settingFill
	settingBubbleIn
	settingBubbleOut
	settingScheduler
)

type settingsField struct {
	id    settingID
	label string
}

// settingsFields is the row list for the current context. Contact theme only
// appears when a contact or open thread gives it a target.
func (m *Model) settingsFields() []settingsField {
	fields := []settingsField{{settingTheme, "Theme"}}
	if _, ok := m.settingsContact(); ok {
		fields = append(fields, settingsField{settingContactTheme, "Contact theme"})
	}
	return append(fields,
		settingsField{settingComposer, "Composer"},
		settingsField{settingBubbles, "Message bubbles"},
		settingsField{settingCorners, "Bubble corners"},
		settingsField{settingFill, "Bubble fill"},
		settingsField{settingBubbleIn, "Incoming bubbles"},
		settingsField{settingBubbleOut, "Outgoing bubbles"},
		settingsField{settingScheduler, "Background sending"},
	)
}

// settingsContact resolves the contact whose theme the panel edits, mirroring
// the palette's Change contact theme command.
func (m *Model) settingsContact() (contacts.Contact, bool) {
	if m.focus && (m.recipient.ID != "" || m.recipient.Synced) {
		return m.recipient, true
	}
	return m.selectedContact()
}

func (m *Model) selectedSetting() (settingsField, bool) {
	fields := m.settingsFields()
	if len(fields) == 0 {
		return settingsField{}, false
	}
	m.choice = max(0, min(m.choice, len(fields)-1))
	return fields[m.choice], true
}

func (m *Model) openSettings() {
	m.modal = "settings"
	m.choice = 0
	m.themeCursor = themeIndex(m.cfg.General.Theme)
	m.contactCursor = 0
	if c, ok := m.settingsContact(); ok {
		m.contactCursor = contactThemeIndex(c.Theme)
	}
	m.bubbleInCursor = contactThemeIndex(m.cfg.Conversation.IncomingTheme)
	m.bubbleOutCursor = contactThemeIndex(m.cfg.Conversation.OutgoingTheme)
}

func themeIndex(name string) int {
	for i, n := range themes.Names {
		if n == name {
			return i
		}
	}
	return 0
}

func contactThemeNames() []string { return append([]string{"automatic"}, themes.Names...) }

func contactThemeIndex(name string) int {
	if name == "" {
		return 0
	}
	for i, n := range contactThemeNames() {
		if n == name {
			return i
		}
	}
	return 0
}

func cycleIndex(v, delta, n int) int {
	if n <= 0 {
		return 0
	}
	return ((v+delta)%n + n) % n
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// settingsValue formats a row's right-hand value. The selected theme row shows
// the cursor so cycling previews before Enter commits.
func (m *Model) settingsValue(id settingID, selected bool) string {
	switch id {
	case settingTheme:
		if selected {
			return themes.Names[m.themeCursor]
		}
		return m.cfg.General.Theme
	case settingContactTheme:
		if selected {
			return contactThemeNames()[m.contactCursor]
		}
		c, _ := m.settingsContact()
		if c.Theme == "" {
			return "automatic"
		}
		return c.Theme
	case settingComposer:
		return m.cfg.Composer.Mode
	case settingBubbles:
		return onOff(m.cfg.Conversation.Bubbles)
	case settingCorners:
		return m.cfg.Conversation.Corners
	case settingFill:
		return onOff(m.cfg.Conversation.FillBubbles)
	case settingBubbleIn:
		if selected {
			return contactThemeNames()[m.bubbleInCursor]
		}
		return bubbleName(m.cfg.Conversation.IncomingTheme)
	case settingBubbleOut:
		if selected {
			return contactThemeNames()[m.bubbleOutCursor]
		}
		return bubbleName(m.cfg.Conversation.OutgoingTheme)
	case settingScheduler:
		return onOff(m.cfg.Scheduler.Enabled)
	}
	return ""
}

// bubbleName labels an unset bubble theme as "automatic", matching the picker.
func bubbleName(name string) string {
	if name == "" {
		return "automatic"
	}
	return name
}

// settingsAdjust handles ←/→: only the theme rows have a value worth cycling
// without committing.
func (m *Model) settingsAdjust(delta int) {
	f, ok := m.selectedSetting()
	if !ok {
		return
	}
	switch f.id {
	case settingTheme:
		m.themeCursor = cycleIndex(m.themeCursor, delta, len(themes.Names))
	case settingContactTheme:
		m.contactCursor = cycleIndex(m.contactCursor, delta, len(contactThemeNames()))
	case settingBubbleIn:
		m.bubbleInCursor = cycleIndex(m.bubbleInCursor, delta, len(contactThemeNames()))
	case settingBubbleOut:
		m.bubbleOutCursor = cycleIndex(m.bubbleOutCursor, delta, len(contactThemeNames()))
	}
}

// settingsActivate handles Enter/Space: commit a theme, toggle a boolean, or
// cycle a two-valued enum, always saving immediately except for themes which
// are already previewed.
func (m *Model) settingsActivate() tea.Cmd {
	f, ok := m.selectedSetting()
	if !ok {
		return nil
	}
	switch f.id {
	case settingTheme:
		c := m.cfg
		c.General.Theme = themes.Names[m.themeCursor]
		return m.saveConfig(c)
	case settingContactTheme:
		return m.commitContactTheme(contactThemeNames()[m.contactCursor])
	case settingComposer:
		c := m.cfg
		if c.Composer.Mode == "vim" {
			c.Composer.Mode = "normal"
		} else {
			c.Composer.Mode = "vim"
		}
		return m.saveConfig(c)
	case settingBubbles:
		c := m.cfg
		c.Conversation.Bubbles = !c.Conversation.Bubbles
		return m.saveConfig(c)
	case settingCorners:
		c := m.cfg
		if c.Conversation.Corners == "square" {
			c.Conversation.Corners = "round"
		} else {
			c.Conversation.Corners = "square"
		}
		return m.saveConfig(c)
	case settingFill:
		c := m.cfg
		c.Conversation.FillBubbles = !c.Conversation.FillBubbles
		return m.saveConfig(c)
	case settingBubbleIn:
		c := m.cfg
		c.Conversation.IncomingTheme = globalBubbleTheme(contactThemeNames()[m.bubbleInCursor])
		return m.saveConfig(c)
	case settingBubbleOut:
		c := m.cfg
		c.Conversation.OutgoingTheme = globalBubbleTheme(contactThemeNames()[m.bubbleOutCursor])
		return m.saveConfig(c)
	case settingScheduler:
		c := m.cfg
		c.Scheduler.Enabled = !c.Scheduler.Enabled
		return m.saveConfig(c)
	}
	return nil
}

// globalBubbleTheme maps the picker's "automatic" to an empty config value.
func globalBubbleTheme(name string) string {
	if name == "automatic" {
		return ""
	}
	return name
}

// openBubblePicker opens the themed value list for one direction. Contact scope
// offers "automatic" (the derived surface); thread scope offers "inherit" (fall
// back to the contact and then the global default).
func (m *Model) openBubblePicker(scope string, outgoing bool) {
	m.bubbleScope = scope
	if outgoing {
		m.bubbleDir = "out"
	} else {
		m.bubbleDir = "in"
	}
	m.modal = "bubble-themes"
	if scope == "thread" {
		m.choices = append([]string{"inherit"}, themes.Names...)
	} else {
		m.choices = contactThemeNames()
	}
	current := ""
	if scope == "thread" {
		if t := m.history.active; t != nil {
			if outgoing {
				current = t.ThemeOut
			} else {
				current = t.ThemeIn
			}
		}
	} else {
		if outgoing {
			current = m.editing.ThemeOut
		} else {
			current = m.editing.ThemeIn
		}
	}
	m.choice = contactThemeIndex(current)
}

// commitBubbleTheme saves the picker's choice for the current scope and
// direction. "automatic" and "inherit" both mean no override.
func (m *Model) commitBubbleTheme(name string) tea.Cmd {
	if name == "automatic" || name == "inherit" {
		name = ""
	}
	m.modal = ""
	if m.bubbleScope == "thread" {
		if m.history.active == nil {
			return nil
		}
		thread := m.history.active.ID
		in, out := m.history.active.ThemeIn, m.history.active.ThemeOut
		if m.bubbleDir == "out" {
			out = name
		} else {
			in = name
		}
		m.history.active.ThemeIn, m.history.active.ThemeOut = in, out
		m.layoutConversation()
		s := m.history.store
		return func() tea.Msg { return historySavedMsg{s.ThreadBubbleThemes(thread, in, out)} }
	}
	local, err := ensureLocal(m.editing)
	if err != nil {
		m.notify("Could not create contact ID", true)
		return nil
	}
	if m.bubbleDir == "out" {
		local.ThemeOut = name
	} else {
		local.ThemeIn = name
	}
	m.busy = true
	return func() tea.Msg { return mutationMsg{contact: &local, err: m.store.SaveContact(local)} }
}

// commitContactTheme applies a contact accent, promoting a phone entry to a
// local contact first, exactly as the palette picker does.
func (m *Model) commitContactTheme(name string) tea.Cmd {
	c, ok := m.settingsContact()
	if !ok {
		m.notify("Select a saved contact first", true)
		return nil
	}
	local, err := ensureLocal(c)
	if err != nil {
		m.notify("Could not create contact ID", true)
		return nil
	}
	if name == "automatic" {
		local.Theme = ""
	} else {
		local.Theme = name
	}
	m.busy = true
	return func() tea.Msg { return mutationMsg{contact: &local, err: m.store.SaveContact(local)} }
}

// settingsThemePreview returns the global theme highlighted in the panel for
// the shell renderer.
func (m *Model) settingsThemePreview() (string, bool) {
	if m.modal != "settings" {
		return "", false
	}
	f, ok := m.selectedSetting()
	if !ok || f.id != settingTheme {
		return "", false
	}
	return themes.Names[m.themeCursor], true
}

// settingsBubblePreview returns the bubble theme highlighted in the panel for
// one direction. "automatic" resolves to an empty name, meaning the derived
// surface.
func (m *Model) settingsBubblePreview(outgoing bool) (string, bool) {
	if m.modal != "settings" {
		return "", false
	}
	f, ok := m.selectedSetting()
	if !ok {
		return "", false
	}
	want := settingBubbleIn
	cursor := m.bubbleInCursor
	if outgoing {
		want, cursor = settingBubbleOut, m.bubbleOutCursor
	}
	if f.id != want {
		return "", false
	}
	return globalBubbleTheme(contactThemeNames()[cursor]), true
}

// settingsContactPreview returns the contact theme highlighted in the panel,
// with "automatic" resolving to no override.
func (m *Model) settingsContactPreview() (string, bool) {
	if m.modal != "settings" {
		return "", false
	}
	f, ok := m.selectedSetting()
	if !ok || f.id != settingContactTheme {
		return "", false
	}
	name := contactThemeNames()[m.contactCursor]
	if name == "automatic" {
		return "", true
	}
	return name, true
}
