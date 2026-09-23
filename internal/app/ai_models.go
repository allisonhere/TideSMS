package app

import (
	"errors"
	"strings"

	"github.com/allisonhere/tidesms/internal/ai"
	"github.com/allisonhere/tidesms/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

// typeModelChoice is the picker's escape hatch, for a model the provider does
// not list — one not yet pulled locally, or newer than the catalogue.
const typeModelChoice = "Type a name…"

// aiModelsMsg carries a finished model listing back to the event loop.
type aiModelsMsg struct {
	models []string
	err    error
}

// chooseAIModel is what Enter on the model row does. It asks the provider what
// it serves and offers the answer, rather than expecting the name to be typed
// from memory — a model that is misremembered fails later, at the first
// request, with an error that does not say the name was wrong.
//
// The order matters: a cloud provider cannot list anything without its key, so
// a missing key sends the reader to that row first instead of producing an
// authentication failure they would have to interpret.
func (m *Model) chooseAIModel() tea.Cmd {
	provider := ai.Provider(m.cfg.AI.Provider)
	// A listing that has already failed for this exact configuration will fail
	// the same way again, and re-attempting it on every Enter is what turned
	// an unreachable provider into a loop between the row and its editor.
	if m.modelListFailed != "" && m.modelListFailed == m.modelListKey() {
		return m.beginSettingEdit(settingAIModel)
	}
	if provider == ai.ProviderDisabled {
		m.notify("Choose a provider first", true)
		m.selectSettingRow(settingAIProvider)
		return nil
	}
	if !provider.Local() && m.cfg.AI.APIKey == "" {
		m.notify("Set the API key first — "+string(provider)+" cannot list its models without one", true)
		m.selectSettingRow(settingAIKey)
		return m.beginSettingEdit(settingAIKey)
	}
	// Listing sends the key to the provider, which is exactly what the privacy
	// policy governs. A policy that forbids the provider forbids this too.
	policy := ai.Policy(m.cfg.AI.DefaultPolicy)
	if policy == "" {
		policy = ai.PolicyLocal
	}
	if !ai.Allowed(policy, provider) {
		m.notify("The "+string(policy)+" policy does not allow contacting "+string(provider), true)
		m.selectSettingRow(settingAIPolicy)
		return nil
	}

	m.notify("Asking "+string(provider)+" for its models…", false)
	m.modelListFailed = ""
	ctx := m.ctx
	rt := ai.Runtime{
		Provider: provider,
		Endpoint: m.cfg.AI.Endpoint,
		APIKey:   m.cfg.AI.APIKey,
	}
	return func() tea.Msg {
		models, err := ai.ListModels(ctx, rt)
		return aiModelsMsg{models: models, err: err}
	}
}

// applyAIModels opens the picker on what came back. A provider that cannot be
// reached is not a dead end: the row falls back to being typed, with the reason
// stated so the failure is not mistaken for the model being unavailable.
func (m *Model) applyAIModels(v aiModelsMsg) tea.Cmd {
	if m.modal != "settings" {
		// The reader moved on while the request was in flight.
		return nil
	}
	if v.err != nil || len(v.models) == 0 {
		m.modelListFailed = m.modelListKey()
		m.notify(m.modelListReason(v.err), true)
		m.selectSettingRow(settingAIModel)
		return m.beginSettingEdit(settingAIModel)
	}
	m.modelListFailed = ""
	// The picker borrows m.choice, which is also the panel's cursor, so the
	// panel's own row is remembered and put back when the picker closes.
	// Without that, leaving the picker drops the reader on whatever row the
	// picker's cursor happened to line up with.
	m.settingsRow = m.choice
	m.modal = "ai-models"
	m.choices = append(append([]string{}, v.models...), typeModelChoice)
	m.choice = 0
	for i, name := range v.models {
		if name == m.cfg.AI.Model {
			m.choice = i
		}
	}
	return nil
}

// closeModelPicker returns to the panel with its cursor where it was left.
func (m *Model) closeModelPicker() {
	m.modal = "settings"
	m.choice = m.settingsRow
}

// cancelModelPicker abandons the choice. An enable waiting on the model is
// abandoned with it: leaving the picker means no model was chosen, and
// switching AI on without one writes a configuration the loader refuses.
func (m *Model) cancelModelPicker() {
	m.closeModelPicker()
	m.selectSettingRow(settingAIModel)
	if m.enablingAI {
		m.enablingAI = false
		m.notify("No model chosen, so AI stayed off", false)
	}
}

// modelListKey identifies the configuration a listing was attempted with, so a
// failure is forgotten the moment any of it changes and the provider is given
// another chance.
func (m *Model) modelListKey() string {
	return m.cfg.AI.Provider + "\x00" + m.cfg.AI.Endpoint + "\x00" + m.cfg.AI.APIKey
}

// modelListReason says why the catalogue could not be had, in terms of the
// thing to do about it. A local provider that is not running is the common
// case and deserves better than a dial error: the endpoint is on this machine,
// so "not running" is both the diagnosis and the fix.
func (m *Model) modelListReason(err error) string {
	provider := ai.Provider(m.cfg.AI.Provider)
	endpoint := m.cfg.AI.Endpoint
	if endpoint == "" {
		endpoint = provider.DefaultEndpoint()
	}
	if provider.Local() && err != nil && strings.Contains(err.Error(), "connect") {
		return string(provider) + " is not running at " + endpoint + " — start it, or type a model name below"
	}
	return modelListError(err) + " — type a model name below"
}

// modelListError turns a listing failure into something a status line can say.
func modelListError(err error) string {
	switch {
	case err == nil:
		return "That provider listed no models"
	case errors.Is(err, ai.ErrRefused):
		return "The API key was rejected"
	default:
		reason := strings.TrimPrefix(err.Error(), ai.ErrUnavailable.Error()+": ")
		if reason == "" || reason == err.Error() {
			return "Could not reach the provider"
		}
		return "Could not list models: " + reason
	}
}

// commitAIModel stores the chosen model and returns to the panel. The escape
// hatch opens the text editor rather than saving its own label.
//
// A model chosen on the way to enabling AI finishes that job here. Asking for
// the model was only ever a detour: leaving the reader on the model row with
// the switch still off sent them back into this picker on the next Enter, which
// is a loop, not a prompt.
func (m *Model) commitAIModel(choice string) tea.Cmd {
	m.closeModelPicker()
	m.selectSettingRow(settingAIModel)
	if choice == typeModelChoice || choice == "" {
		return m.beginSettingEdit(settingAIModel)
	}
	c := m.cfg
	c.AI.Model = choice
	return m.saveConfig(m.finishEnabling(c))
}

// finishEnabling switches AI on when a model was the only thing missing, and
// puts the cursor back on the row the reader pressed Enter on so the change is
// visible where they asked for it.
func (m *Model) finishEnabling(c config.Config) config.Config {
	if !m.enablingAI || c.AI.Model == "" {
		return c
	}
	m.enablingAI = false
	c.AI.Enabled = true
	m.selectSettingRow(settingAIEnabled)
	m.notify("AI on · "+c.AI.Provider+" · "+c.AI.Model, false)
	return c
}
