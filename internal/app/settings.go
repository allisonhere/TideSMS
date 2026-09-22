package app

import (
	"strconv"
	"strings"

	"github.com/allisonhere/tidesms/internal/ai"
	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/themes"
	"github.com/charmbracelet/bubbles/textinput"
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
	settingInlineMedia
	settingAIEnabled
	settingAIProvider
	settingAIEndpoint
	settingAIModel
	settingAIKey
	settingAIPolicy
	settingScheduler
)

// aiSettings are the rows that configure the writing assistant. They are the
// only way to turn AI on from inside the app: the privacy pickers in the
// command palette narrow what an assistant may do, and cannot bring one into
// existence.
var aiSettings = []settingsField{
	{settingAIEnabled, "AI enabled"},
	{settingAIProvider, "AI provider"},
	{settingAIEndpoint, "AI endpoint"},
	{settingAIModel, "AI model"},
	{settingAIKey, "AI API key"},
	{settingAIPolicy, "AI default policy"},
}

// textSetting reports whether a row is edited by typing rather than cycling.
func textSetting(id settingID) bool {
	return id == settingAIEndpoint || id == settingAIModel || id == settingAIKey
}

// globalPolicyChoices are the values the default policy may take. Unlike the
// per-contact picker there is no "inherit": this is what everything inherits.
var globalPolicyChoices = []string{"local", "cloud", "disabled"}

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
	fields = append(fields,
		settingsField{settingComposer, "Composer"},
		settingsField{settingBubbles, "Message bubbles"},
		settingsField{settingCorners, "Bubble corners"},
		settingsField{settingFill, "Bubble fill"},
		settingsField{settingBubbleIn, "Incoming bubbles"},
		settingsField{settingBubbleOut, "Outgoing bubbles"},
		settingsField{settingInlineMedia, "Inline images"},
	)
	fields = append(fields, aiSettings...)
	return append(fields, settingsField{settingScheduler, "Background sending"})
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
	m.aiProviderCursor = providerIndex(m.cfg.AI.Provider)
	policy := m.cfg.AI.DefaultPolicy
	if policy == "" {
		policy = "local"
	}
	m.aiPolicyCursor = policyIndex(policy)
	m.settingEdit = false
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

// orDash shows an unset value as a dash rather than as nothing, so an empty
// row is visibly empty instead of looking broken.
func orDash(v string) string {
	if v == "" {
		return "—"
	}
	return v
}

// maskedKey describes a stored API key without printing it. The length is
// shown because it is the one property worth confirming at a glance, and the
// key itself is never rendered: the panel sits on a screen that may be shared.
func maskedKey(key string) string {
	if key == "" {
		return "not set"
	}
	return strings.Repeat("•", min(len(key), 12)) + " (" + strconv.Itoa(len(key)) + " chars)"
}

func providerIndex(name string) int {
	for i, p := range ai.Providers {
		if string(p) == name {
			return i
		}
	}
	return 0
}

func policyIndex(name string) int {
	for i, p := range globalPolicyChoices {
		if p == name {
			return i
		}
	}
	return 0
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
	case settingInlineMedia:
		return onOff(m.cfg.Conversation.InlineMedia)
	case settingAIEnabled:
		return onOff(m.cfg.AI.Enabled)
	case settingAIProvider:
		if selected {
			return string(ai.Providers[m.aiProviderCursor])
		}
		return orDash(m.cfg.AI.Provider)
	case settingAIEndpoint:
		if m.cfg.AI.Endpoint == "" {
			// An empty endpoint is not unset so much as "whatever this provider
			// uses", which is worth showing rather than leaving blank.
			if d := ai.Provider(m.cfg.AI.Provider).DefaultEndpoint(); d != "" {
				return d + " (default)"
			}
			return "—"
		}
		return m.cfg.AI.Endpoint
	case settingAIModel:
		return orDash(m.cfg.AI.Model)
	case settingAIKey:
		return maskedKey(m.cfg.AI.APIKey)
	case settingAIPolicy:
		if selected {
			return globalPolicyChoices[m.aiPolicyCursor]
		}
		if m.cfg.AI.DefaultPolicy == "" {
			return "local"
		}
		return m.cfg.AI.DefaultPolicy
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
	case settingAIProvider:
		m.aiProviderCursor = cycleIndex(m.aiProviderCursor, delta, len(ai.Providers))
	case settingAIPolicy:
		m.aiPolicyCursor = cycleIndex(m.aiPolicyCursor, delta, len(globalPolicyChoices))
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
	case settingInlineMedia:
		c := m.cfg
		c.Conversation.InlineMedia = !c.Conversation.InlineMedia
		return m.saveConfig(c)
	case settingScheduler:
		c := m.cfg
		c.Scheduler.Enabled = !c.Scheduler.Enabled
		return m.saveConfig(c)
	case settingAIEnabled:
		c := m.cfg
		if c.AI.Enabled {
			c.AI.Enabled = false
			return m.saveConfig(c)
		}
		// Turning AI on with nothing behind it would leave every AI command
		// failing. Pick the local provider, which needs no key and no network.
		if ai.Provider(c.AI.Provider) == ai.ProviderDisabled {
			c.AI.Provider = string(ai.ProviderOllama)
			m.aiProviderCursor = providerIndex(c.AI.Provider)
		}
		// An enabled provider with no model is a configuration the loader
		// rejects, so saving one here would lock the app out of its own config
		// on the next start. Ask for the model instead of writing that.
		if c.AI.Model == "" {
			m.notify("Name the model first — an enabled provider needs one", true)
			m.selectSettingRow(settingAIModel)
			return m.beginSettingEdit(settingAIModel)
		}
		c.AI.Enabled = true
		return m.saveConfig(c)
	case settingAIProvider:
		c := m.cfg
		c.AI.Provider = string(ai.Providers[m.aiProviderCursor])
		// An endpoint belongs to the provider that was chosen with it. Keeping
		// one across a switch would point the new provider at the old one's
		// address, so a default is restored instead.
		c.AI.Endpoint = ""
		if ai.Provider(c.AI.Provider) == ai.ProviderDisabled {
			c.AI.Enabled = false
		}
		return m.saveConfig(c)
	case settingAIPolicy:
		c := m.cfg
		c.AI.DefaultPolicy = globalPolicyChoices[m.aiPolicyCursor]
		return m.saveConfig(c)
	case settingAIEndpoint, settingAIModel, settingAIKey:
		return m.beginSettingEdit(f.id)
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
//
// It previews onto the open conversation only when the contact being edited is
// that conversation's own. The panel edits whichever contact is selected, which
// need not be the one on screen: cycling one person's theme used to repaint a
// different person's conversation, showing a colour that would never be applied
// to it.
func (m *Model) settingsContactPreview() (string, bool) {
	if m.modal != "settings" {
		return "", false
	}
	f, ok := m.selectedSetting()
	if !ok || f.id != settingContactTheme {
		return "", false
	}
	if !m.previewTargetsConversation() {
		return "", false
	}
	name := contactThemeNames()[m.contactCursor]
	if name == "automatic" {
		return "", true
	}
	return name, true
}

// previewTargetsConversation reports whether the contact the settings panel is
// editing is the one whose conversation is open.
func (m *Model) previewTargetsConversation() bool {
	c, ok := m.settingsContact()
	if !ok {
		return false
	}
	if c.ID != "" && c.ID == m.recipient.ID {
		return true
	}
	if c.PhoneNumber == "" || m.recipient.PhoneNumber == "" {
		return false
	}
	if c.PhoneNumber == m.recipient.PhoneNumber {
		return true
	}
	// Numbers written differently can still be the same person, which is what
	// decides whose conversation is open everywhere else.
	key := contacts.MatchKey(c.PhoneNumber)
	return key != "" && key == contacts.MatchKey(m.recipient.PhoneNumber)
}

// beginSettingEdit opens the inline editor for a typed row. The API key starts
// empty rather than prefilled: it is shown masked everywhere else, and
// prefilling it would put the secret back on screen the moment it is edited.
func (m *Model) beginSettingEdit(id settingID) tea.Cmd {
	in := textinput.New()
	in.CharLimit = 300
	switch id {
	case settingAIEndpoint:
		in.SetValue(m.cfg.AI.Endpoint)
		in.Placeholder = ai.Provider(m.cfg.AI.Provider).DefaultEndpoint()
	case settingAIModel:
		in.SetValue(m.cfg.AI.Model)
		in.Placeholder = "model name"
	case settingAIKey:
		in.EchoMode = textinput.EchoPassword
		in.EchoCharacter = '•'
		in.Placeholder = "paste the key; empty clears it"
	}
	in.CursorEnd()
	m.settingInput = in
	m.settingEditing = id
	m.settingEdit = true
	// An unfocused textinput ignores every key, so the row would look editable
	// and silently swallow what was typed.
	return m.settingInput.Focus()
}

// commitSettingEdit stores the typed value. Whitespace is trimmed because a
// pasted key or endpoint commonly carries a trailing newline, which would
// otherwise be sent to the provider verbatim.
func (m *Model) commitSettingEdit() tea.Cmd {
	if !m.settingEdit {
		return nil
	}
	value := strings.TrimSpace(m.settingInput.Value())
	id := m.settingEditing
	m.settingEdit = false
	c := m.cfg
	switch id {
	case settingAIEndpoint:
		c.AI.Endpoint = value
	case settingAIModel:
		c.AI.Model = value
		// The loader refuses an enabled provider with no model, so clearing the
		// model switches AI off rather than leaving a config that will not load.
		if value == "" && c.AI.Enabled {
			c.AI.Enabled = false
			m.notify("AI switched off: an enabled provider needs a model", false)
		}
	case settingAIKey:
		c.AI.APIKey = value
	default:
		return nil
	}
	return m.saveConfig(c)
}

func (m *Model) cancelSettingEdit() { m.settingEdit = false }

// selectSettingRow moves the panel's cursor to a row by id, so a setting that
// depends on another can send the reader straight to it.
func (m *Model) selectSettingRow(id settingID) {
	for i, f := range m.settingsFields() {
		if f.id == id {
			m.choice = i
			return
		}
	}
}

// settingsEditKey drives the inline editor. Every key that is not Enter or Esc
// is text, which is why h/j/k/l must not reach the panel's navigation.
func (m *Model) settingsEditKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "enter":
		return m.commitSettingEdit()
	case "esc":
		m.cancelSettingEdit()
		return nil
	}
	var cmd tea.Cmd
	m.settingInput, cmd = m.settingInput.Update(k)
	return cmd
}

// settingsHint describes what the selected row responds to, so a typed row
// does not look unresponsive to the arrow keys that cycle every other one.
func (m *Model) settingsHint() string {
	if m.settingEdit {
		if m.settingEditing == settingAIKey {
			return "Enter save · Esc cancel · the key is never shown"
		}
		return "Enter save · Esc cancel"
	}
	if f, ok := m.selectedSetting(); ok && textSetting(f.id) {
		return "↑↓ move · Enter edit · Esc close"
	}
	return "↑↓ move · ←→ change · Enter toggle · Esc close"
}

// aiSettingsNotice explains a configuration that cannot work, at the moment it
// is visible. The AI commands otherwise fail later with a message about the
// privacy policy, which is not the setting at fault.
func (m *Model) aiSettingsNotice() string {
	provider := ai.Provider(m.cfg.AI.Provider)
	if !m.cfg.AI.Enabled || provider == ai.ProviderDisabled {
		return "AI is off: set AI enabled and a provider"
	}
	policy := ai.Policy(m.cfg.AI.DefaultPolicy)
	if policy == "" {
		policy = ai.PolicyLocal
	}
	if !ai.Allowed(policy, provider) {
		return "Default policy " + string(policy) + " forbids " + string(provider) + "; use ollama or lmstudio, or set the policy to cloud"
	}
	if !provider.Local() && m.cfg.AI.APIKey == "" {
		return string(provider) + " needs an API key"
	}
	if m.cfg.AI.Model == "" {
		return "No model set for " + string(provider)
	}
	return ""
}
