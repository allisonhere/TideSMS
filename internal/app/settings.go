package app

import (
	"reflect"
	"strconv"
	"strings"

	"github.com/allisonhere/tidesms/internal/ai"
	"github.com/allisonhere/tidesms/internal/config"
	"github.com/allisonhere/tidesms/internal/contacts"
	"github.com/allisonhere/tidesms/internal/themes"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The settings panel is a list of fields edited as one draft. Enter and the
// arrow keys change a row in place and the change previews at once, but nothing
// is written until Ctrl+S saves the whole panel; Esc closes it and puts back
// what was last saved.

type settingID int

const (
	settingTheme settingID = iota
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
	settingUndoSend
)

// undoSendChoices are the undo windows the settings row cycles through.
var undoSendChoices = []int{0, 3, 5, 10}

// aiSettings are the rows that configure the writing assistant. They are the
// only way to turn AI on from inside the app: the privacy pickers in the
// command palette narrow what an assistant may do, and cannot bring one into
// existence.
var aiSettings = []settingsField{
	{settingAIEnabled, "Enabled"},
	{settingAIProvider, "Provider"},
	{settingAIEndpoint, "Endpoint"},
	{settingAIKey, "API key"},
	{settingAIModel, "Model"},
	{settingAIPolicy, "Default policy"},
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

// settingsGroup is one titled run of rows. The panel is long enough that an
// undivided list makes the reader scan it; grouping says what each row is about
// before its label has to.
type settingsGroup struct {
	title  string
	fields []settingsField
}

// settingsGroups is the panel's shape. It holds app-wide defaults only: a
// conversation's own colours are set from its details screen (i or Ctrl+L),
// where there is never any doubt about whose they are.
func (m *Model) settingsGroups() []settingsGroup {
	appearance := []settingsField{{settingTheme, "Theme"}}
	appearance = append(appearance,
		settingsField{settingBubbles, "Message bubbles"},
		settingsField{settingCorners, "Bubble corners"},
		settingsField{settingFill, "Bubble fill"},
		settingsField{settingBubbleIn, "Incoming bubbles"},
		settingsField{settingBubbleOut, "Outgoing bubbles"},
	)
	return []settingsGroup{
		{"Appearance", appearance},
		{"Conversation", []settingsField{
			{settingComposer, "Composer"},
			{settingInlineMedia, "Inline images"},
		}},
		{"Assistant", aiSettings},
		{"Sending", []settingsField{
			{settingUndoSend, "Undo send"},
			{settingScheduler, "Background sending"},
		}},
	}
}

// settingsFields flattens the groups. Selection, navigation and every lookup
// address rows by their position in this list, so a heading can never be landed
// on: it is drawn, not selected.
func (m *Model) settingsFields() []settingsField {
	var out []settingsField
	for _, g := range m.settingsGroups() {
		out = append(out, g.fields...)
	}
	return out
}

// settingsLine is one drawn line of the panel: a heading, or a row together
// with its index among the fields.
type settingsLine struct {
	heading string
	field   settingsField
	index   int
}

// settingsLines is what the panel draws, headings included, so the renderer
// does not have to rebuild the grouping to lay it out.
func (m *Model) settingsLines() []settingsLine {
	var out []settingsLine
	index := 0
	for _, g := range m.settingsGroups() {
		if len(g.fields) == 0 {
			continue
		}
		out = append(out, settingsLine{heading: g.title})
		for _, f := range g.fields {
			out = append(out, settingsLine{field: f, index: index})
			index++
		}
	}
	return out
}

func (m *Model) selectedSetting() (settingsField, bool) {
	fields := m.settingsFields()
	if len(fields) == 0 {
		return settingsField{}, false
	}
	m.choice = max(0, min(m.choice, len(fields)-1))
	return fields[m.choice], true
}

func (m *Model) openSettings() tea.Cmd {
	m.modal = "settings"
	m.choice = 0
	m.themeCursor = themeIndex(m.cfg.General.Theme)
	m.bubbleInCursor = contactThemeIndex(m.cfg.Conversation.IncomingTheme)
	m.bubbleOutCursor = contactThemeIndex(m.cfg.Conversation.OutgoingTheme)
	m.aiProviderCursor = providerIndex(m.cfg.AI.Provider)
	policy := m.cfg.AI.DefaultPolicy
	if policy == "" {
		policy = "local"
	}
	m.aiPolicyCursor = policyIndex(policy)
	m.settingEdit = false
	m.settingsSaved = m.cfg
	return m.probeLocalProviders()
}

// stageConfig makes c the panel's draft: it takes effect everywhere at once so
// it can be judged, but nothing is written until Ctrl+S.
func (m *Model) stageConfig(c config.Config) tea.Cmd {
	m.cfg = c
	m.applyConfig()
	m.layoutConversation()
	// A key typed for a hosted provider is only useful with a model, so go
	// straight on to choosing one, as saving the key used to.
	if m.lookupAfterKey {
		m.lookupAfterKey = false
		m.selectSettingRow(settingAIModel)
		return m.chooseAIModel()
	}
	return nil
}

// settingsDirty reports whether the panel holds anything Ctrl+S would write.
func (m *Model) settingsDirty() bool {
	return !reflect.DeepEqual(m.cfg, m.settingsSaved)
}

// saveSettings writes the draft. The panel stays open, so a run of changes can
// be saved and then carried on from.
func (m *Model) saveSettings() tea.Cmd {
	if !m.settingsDirty() {
		m.notify("No changes to save", false)
		return nil
	}
	if provider := ai.Provider(m.cfg.AI.Provider); provider != ai.Provider(m.settingsSaved.AI.Provider) && m.localProviderUnavailable(provider) {
		m.notify(string(provider)+" is unavailable; choose another provider", true)
		return nil
	}
	cmd := m.saveConfig(m.cfg)
	if cmd == nil {
		return nil
	}
	m.settingsSaved = m.cfg
	m.notify("Settings saved", false)
	return cmd
}

// discardSettings closes the panel and puts back what was saved, so a draft
// that was only being tried leaves nothing behind.
func (m *Model) discardSettings() {
	dirty := m.settingsDirty()
	m.cfg = m.settingsSaved
	m.applyConfig()
	m.layoutConversation()
	m.modal = ""
	if dirty {
		m.notify("Settings changes discarded", false)
	}
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
	case settingUndoSend:
		if m.cfg.Composer.UndoSeconds == 0 {
			return "off"
		}
		return strconv.Itoa(m.cfg.Composer.UndoSeconds) + "s"
	case settingAIEnabled:
		return onOff(m.cfg.AI.Enabled)
	case settingAIProvider:
		if selected {
			return m.providerLabel(ai.Providers[m.aiProviderCursor])
		}
		return m.providerLabel(ai.Provider(m.cfg.AI.Provider))
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

// bubbleSwatch draws a bubble row's value on the fill and text that bubble would
// use, so cycling the row shows the colour rather than only its name. It asks
// BubbleFor, as the conversation does, so "automatic" shows the derived surface
// and a theme too faint against the pane shows the fallback it would really get.
func (m *Model) bubbleSwatch(value string, outgoing bool) string {
	name := value
	if name == "automatic" {
		name = ""
	}
	b := themes.BubbleFor(themes.Base(m.cfg.General.Theme), name, outgoing)
	if b.Fill == "" {
		return value
	}
	return lipgloss.NewStyle().Background(b.Fill).Foreground(b.Text).Render(" " + value + " ")
}

// settingsAdjust handles ←/→: it changes the row's value in the draft, which
// previews at once. Nothing is written until Ctrl+S. The contact theme is kept
// as a cursor rather than staged, because it lives on the contact and not in
// the configuration.
func (m *Model) settingsAdjust(delta int) tea.Cmd {
	f, ok := m.selectedSetting()
	if !ok {
		return nil
	}
	c := m.cfg
	switch f.id {
	case settingTheme:
		m.themeCursor = cycleIndex(m.themeCursor, delta, len(themes.Names))
		c.General.Theme = themes.Names[m.themeCursor]
	case settingBubbleIn:
		m.bubbleInCursor = cycleIndex(m.bubbleInCursor, delta, len(contactThemeNames()))
		c.Conversation.IncomingTheme = globalBubbleTheme(contactThemeNames()[m.bubbleInCursor])
	case settingBubbleOut:
		m.bubbleOutCursor = cycleIndex(m.bubbleOutCursor, delta, len(contactThemeNames()))
		c.Conversation.OutgoingTheme = globalBubbleTheme(contactThemeNames()[m.bubbleOutCursor])
	case settingAIProvider:
		// An unavailable provider may be passed through while cycling; the
		// panel says so, and Ctrl+S refuses to save it.
		m.aiProviderCursor = cycleIndex(m.aiProviderCursor, delta, len(ai.Providers))
		c = c.SwitchAIProvider(string(ai.Providers[m.aiProviderCursor]))
		m.modelListFailed = ""
		if ai.Provider(c.AI.Provider) == ai.ProviderDisabled {
			c.AI.Enabled = false
		}
	case settingAIPolicy:
		m.aiPolicyCursor = cycleIndex(m.aiPolicyCursor, delta, len(globalPolicyChoices))
		c.AI.DefaultPolicy = globalPolicyChoices[m.aiPolicyCursor]
	case settingComposer:
		if c.Composer.Mode == "vim" {
			c.Composer.Mode = "normal"
		} else {
			c.Composer.Mode = "vim"
		}
	case settingBubbles:
		c.Conversation.Bubbles = !c.Conversation.Bubbles
	case settingCorners:
		if c.Conversation.Corners == "square" {
			c.Conversation.Corners = "round"
		} else {
			c.Conversation.Corners = "square"
		}
	case settingFill:
		c.Conversation.FillBubbles = !c.Conversation.FillBubbles
	case settingInlineMedia:
		c.Conversation.InlineMedia = !c.Conversation.InlineMedia
	case settingScheduler:
		c.Scheduler.Enabled = !c.Scheduler.Enabled
	case settingUndoSend:
		// A value typed into the file that is not a choice starts from off.
		i := 0
		for j, v := range undoSendChoices {
			if v == c.Composer.UndoSeconds {
				i = j
			}
		}
		c.Composer.UndoSeconds = undoSendChoices[cycleIndex(i, delta, len(undoSendChoices))]
	case settingAIEnabled:
		c.AI.Enabled = !c.AI.Enabled
	default:
		return nil
	}
	return m.stageConfig(c)
}

// settingsActivate handles Enter/Space. A row with a value changes it as →
// does; the typed rows open their editor, and the model row its picker. Enter
// never saves: that is Ctrl+S, for the whole panel at once.
func (m *Model) settingsActivate() tea.Cmd {
	f, ok := m.selectedSetting()
	if !ok {
		return nil
	}
	switch f.id {
	case settingAIModel:
		// Offer what the provider actually serves; typing stays available
		// behind the picker's own entry, and as the fallback when the provider
		// cannot be reached.
		return m.chooseAIModel()
	case settingAIEndpoint, settingAIKey:
		return m.beginSettingEdit(f.id)
	}
	return m.settingsAdjust(1)
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
	return func() tea.Msg { return mutationMsg{contact: &local, err: m.store.SaveContact(local), restyle: true} }
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

// pendingContactTheme returns the contact whose theme is being chosen right
// now and the palette highlighted for it, from either surface that offers the
// choice: the settings panel's row, or the picker t opens. An "automatic"
// choice yields an empty name, meaning the contact keeps no override.
//
// It is deliberately not scoped to the open conversation. A live preview has
// to reach everywhere that contact is drawn — their sidebar row as much as
// their conversation — and only each caller knows which of those it is
// painting.
func (m *Model) pendingContactTheme() (contacts.Contact, string, bool) {
	switch m.modal {
	case "themes":
		if m.choice < 0 || m.choice >= len(m.choices) {
			return contacts.Contact{}, "", false
		}
		return m.editing, blankAutomatic(m.choices[m.choice]), true
	case "details":
		// A group's colours are its thread's, not any member's.
		if m.details.group {
			return contacts.Contact{}, "", false
		}
		name, _ := m.detailsPreview(detailsTheme)
		return m.recipient, name, true
	}
	return contacts.Contact{}, "", false
}

// blankAutomatic maps the picker's "automatic" onto the empty name the rest of
// the code uses for "no override".
func blankAutomatic(name string) string {
	if name == "automatic" {
		return ""
	}
	return name
}

// sameContact reports whether two records name the same person. Numbers
// written differently still match, which is what decides whose conversation is
// open everywhere else.
func sameContact(a, b contacts.Contact) bool {
	if a.ID != "" && a.ID == b.ID {
		return true
	}
	if a.PhoneNumber == "" || b.PhoneNumber == "" {
		return false
	}
	if a.PhoneNumber == b.PhoneNumber {
		return true
	}
	key := contacts.MatchKey(a.PhoneNumber)
	return key != "" && key == contacts.MatchKey(b.PhoneNumber)
}

// contactRows returns the sidebar's contacts with any live theme preview
// applied, so choosing a palette recolours the row at the same moment it
// recolours the conversation rather than only once it is committed.
func (m *Model) contactRows() []contacts.Contact {
	list := m.filtered()
	target, name, ok := m.pendingContactTheme()
	if !ok {
		return list
	}
	for i := range list {
		if sameContact(list[i], target) {
			list[i].Theme = name
		}
	}
	return list
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
	// An empty, unchanged field is a cancelled edit, not a validation trap.
	if id == settingAIModel && value == "" && m.cfg.AI.Model == "" {
		m.cancelSettingEdit()
		return nil
	}
	m.settingEdit = false
	c := m.cfg
	switch id {
	case settingAIEndpoint:
		c.AI.Endpoint = value
		if c.AI.Provider == string(ai.ProviderOllama) {
			c.AI.OllamaEndpoint = value
		}
	case settingAIModel:
		c.AI.Model = value
	case settingAIKey:
		c.AI.APIKey = value
		m.modelListFailed = ""
		m.lookupAfterKey = value != "" && !ai.Provider(c.AI.Provider).Local() && ai.Provider(c.AI.Provider) != ai.ProviderDisabled
	default:
		return nil
	}
	return m.stageConfig(c)
}

func (m *Model) cancelSettingEdit() {
	m.settingEdit = false
}

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
	case "tab", "shift+tab", "up", "down":
		cmd := m.commitSettingEdit()
		delta := 1
		if k.String() == "up" || k.String() == "shift+tab" {
			delta = -1
		}
		m.choice = cycleIndex(m.choice, delta, len(m.settingsFields()))
		return cmd
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
			return "Enter done · Esc cancel · the key is never shown"
		}
		return "Enter done · Tab/↑↓ leave field · Esc cancel"
	}
	esc := "Esc close"
	if m.settingsDirty() {
		esc = "Esc discard"
	}
	if f, ok := m.selectedSetting(); ok && textSetting(f.id) {
		return "↑↓ move · Enter edit · Ctrl+S save · " + esc
	}
	return "↑↓ move · ←→/Enter change · Ctrl+S save · " + esc
}

// aiSettingsNotice explains a configuration that cannot work, at the moment it
// is visible. The AI commands otherwise fail later with a message about the
// privacy policy, which is not the setting at fault.
func (m *Model) aiSettingsNotice() string {
	provider := ai.Provider(m.cfg.AI.Provider)
	if !m.cfg.AI.Enabled {
		return "AI is off"
	}
	if provider == ai.ProviderDisabled {
		return "AI is on: choose a provider"
	}
	if m.localProviderUnavailable(provider) {
		return "AI is on: " + string(provider) + " is unavailable; choose another provider"
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
