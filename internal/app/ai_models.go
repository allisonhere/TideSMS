package app

import (
	"errors"
	"strings"

	"github.com/allisonhere/tidesms/internal/ai"
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
		m.notify(modelListError(v.err)+" — type the model name instead", true)
		m.selectSettingRow(settingAIModel)
		return m.beginSettingEdit(settingAIModel)
	}
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
func (m *Model) commitAIModel(choice string) tea.Cmd {
	m.modal = "settings"
	m.selectSettingRow(settingAIModel)
	if choice == typeModelChoice || choice == "" {
		return m.beginSettingEdit(settingAIModel)
	}
	c := m.cfg
	c.AI.Model = choice
	return m.saveConfig(c)
}
