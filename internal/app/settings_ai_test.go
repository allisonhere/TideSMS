package app

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/allisonhere/tidesms/internal/ai"
	"github.com/allisonhere/tidesms/internal/config"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// The settings panel is the only place AI can be switched on from inside the
// app: the palette's policy pickers narrow an assistant that already exists.
func TestSettingsOffersAIConfiguration(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	d.run(m.action("Open settings"))
	if m.modal != "settings" {
		t.Fatalf("modal %q", m.modal)
	}
	for _, label := range []string{"Enabled", "Provider", "Endpoint", "Model", "API key", "Default policy"} {
		selectSetting(t, m, label)
	}
}

// modelServer stands in for a provider's model listing, so the flow is driven
// without reaching the network or depending on a local Ollama.
func modelServer(t *testing.T, models ...string) string {
	t.Helper()
	body := `{"data":[`
	for i, name := range models {
		if i > 0 {
			body += ","
		}
		body += `{"id":"` + name + `"}`
	}
	body += `]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/v1"
}

// The model is chosen from what the provider actually serves. Typing a name
// from memory fails later, at the first request, with an error that never says
// the name was the problem.
func TestEnablingAIChoosesAModelFromTheProvider(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	c := m.cfg
	c.AI.Provider = string(ai.ProviderOllama)
	c.AI.Endpoint = modelServer(t, "qwen2.5", "llama3.2")
	d.run(m.saveConfig(c))
	d.settle("seeded", func() bool { return !m.busy && m.cfg.AI.Endpoint != "" })

	d.run(m.action("Open settings"))
	selectSetting(t, m, "Enabled")

	// Enabling with no model goes and asks rather than writing a config the
	// loader would refuse on the next start.
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	if m.cfg.AI.Enabled {
		t.Fatal("AI was enabled with no model, which config.Load refuses")
	}
	if m.modal != "ai-models" {
		t.Fatalf("the model picker did not open: modal=%q", m.modal)
	}
	// Sorted, with the escape hatch last.
	if len(m.choices) != 3 || m.choices[0] != "llama3.2" || m.choices[2] != typeModelChoice {
		t.Fatalf("choices = %v", m.choices)
	}

	pickChoice(t, m, "qwen2.5")
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	d.settle("model", func() bool { return !m.busy && m.cfg.AI.Model == "qwen2.5" })
	if m.modal != "settings" {
		t.Errorf("the picker did not return to the panel: %q", m.modal)
	}
	// Choosing the model finishes the enable: Enter was pressed on Enabled,
	// and being asked for a model was only a detour on the way there. A second
	// press would toggle it straight back off.
	if !m.cfg.AI.Enabled {
		t.Fatal("choosing the model did not finish switching AI on")
	}

	// The written config must load again: a panel that writes a file the app
	// then refuses would lock the reader out of their own settings.
	saved, err := config.Load(m.configPath)
	if err != nil || !saved.AI.Enabled || saved.AI.Model != "qwen2.5" {
		t.Fatalf("not persisted or not loadable: %+v %v", saved.AI, err)
	}
}

// Enabling with no provider at all picks the local one, which needs no key and
// no network, and then goes looking for its models.
func TestEnablingAIChoosesALocalProvider(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	d.run(m.action("Open settings"))
	selectSetting(t, m, "Enabled")

	cmd := m.modalKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enabling with nothing configured did nothing")
	}
	if m.cfg.AI.Enabled {
		t.Fatal("AI was enabled with no model")
	}
	// The provider is saved before the lookup, which reads the configuration to
	// know who to ask.
	d.run(cmd)
	d.settle("provider", func() bool { return !m.busy })
	if got := ai.Provider(m.cfg.AI.Provider); got != ai.ProviderOllama {
		t.Fatalf("provider = %q, want a local one", got)
	}
	policy := ai.Policy(m.cfg.AI.DefaultPolicy)
	if policy == "" {
		policy = ai.PolicyLocal
	}
	if !ai.Allowed(policy, ai.Provider(m.cfg.AI.Provider)) {
		t.Fatalf("enabling produced a setup its own policy forbids: %s / %s", policy, m.cfg.AI.Provider)
	}
}

// A provider that cannot be reached leaves the model typeable rather than
// stranding the reader with no way to name one.
func TestUnreachableProviderFallsBackToTyping(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	d.run(m.action("Open settings"))
	selectSetting(t, m, "Model")

	d.run(m.applyAIModels(aiModelsMsg{err: ai.ErrUnavailable}))
	if m.modal != "settings" {
		t.Fatalf("modal = %q, want the panel", m.modal)
	}
	if !m.settingEdit || m.settingEditing != settingAIModel {
		t.Fatal("an unreachable provider did not fall back to typing the model")
	}
	for _, r := range "llama3.2" {
		m.modalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	d.settle("model", func() bool { return !m.busy && m.cfg.AI.Model == "llama3.2" })
}

// A cloud provider cannot list anything without its key, so the panel asks for
// the key first instead of producing an authentication failure to interpret.
func TestCloudProviderAsksForTheKeyBeforeListing(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	c := m.cfg
	c.AI.Provider = string(ai.ProviderOpenAI)
	c.AI.DefaultPolicy = "cloud"
	d.run(m.saveConfig(c))
	d.settle("seeded", func() bool { return !m.busy && m.cfg.AI.Provider == string(ai.ProviderOpenAI) })

	d.run(m.action("Open settings"))
	selectSetting(t, m, "Model")
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))

	if m.modal == "ai-models" {
		t.Fatal("a keyless cloud provider was asked for its models")
	}
	if !m.settingEdit || m.settingEditing != settingAIKey {
		t.Fatalf("the reader was not sent to the key row: edit=%v row=%v", m.settingEdit, m.settingEditing)
	}
	if !strings.Contains(m.notice, "API key") {
		t.Errorf("notice = %q, want it to name the key", m.notice)
	}
}

// Listing sends the key to the provider, which is what the privacy policy
// governs: a local-only policy forbids the request as much as a completion.
func TestLocalOnlyPolicyBlocksListingACloudProvider(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	c := m.cfg
	c.AI.Provider = string(ai.ProviderOpenAI)
	c.AI.APIKey = "sk-test"
	c.AI.DefaultPolicy = "local"
	d.run(m.saveConfig(c))
	d.settle("seeded", func() bool { return !m.busy && m.cfg.AI.APIKey == "sk-test" })

	d.run(m.action("Open settings"))
	selectSetting(t, m, "Model")
	if cmd := m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Fatal("a forbidden provider was contacted for its models")
	}
	if m.modal == "ai-models" {
		t.Fatal("a forbidden provider's models were offered")
	}
	if !strings.Contains(m.notice, "does not allow") {
		t.Errorf("notice = %q", m.notice)
	}
}

// Clearing the model of an enabled provider must not leave a config the loader
// refuses; AI is switched off with it.
func TestClearingTheModelDisablesAI(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	c := m.cfg
	c.AI.Enabled = true
	c.AI.Provider = string(ai.ProviderOllama)
	c.AI.Model = "llama3.2"
	d.run(m.saveConfig(c))
	d.settle("seeded", func() bool { return !m.busy && m.cfg.AI.Enabled })

	d.run(m.action("Open settings"))
	selectSetting(t, m, "Model")
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	for range "llama3.2" {
		m.modalKey(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	d.settle("cleared", func() bool { return !m.busy && m.cfg.AI.Model == "" })

	if m.cfg.AI.Enabled {
		t.Error("clearing the model left AI enabled, which config.Load refuses")
	}
	if saved, err := config.Load(m.configPath); err != nil {
		t.Fatalf("the written config no longer loads: %v (%+v)", err, saved.AI)
	}
}

// Switching provider drops the endpoint, which belonged to the old one.
func TestChangingProviderClearsTheEndpoint(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	c := m.cfg
	c.AI.Enabled = true
	c.AI.Provider = string(ai.ProviderOllama)
	c.AI.Endpoint = "http://127.0.0.1:11434/v1"
	d.run(m.saveConfig(c))
	d.settle("seeded", func() bool { return !m.busy && m.cfg.AI.Endpoint != "" })

	d.run(m.action("Open settings"))
	selectSetting(t, m, "Provider")
	m.aiProviderCursor = providerIndex(string(ai.ProviderLMStudio))
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	d.settle("switched", func() bool { return !m.busy && m.cfg.AI.Provider == string(ai.ProviderLMStudio) })

	if m.cfg.AI.Endpoint != "" {
		t.Fatalf("endpoint %q survived a provider switch and now points at the old one", m.cfg.AI.Endpoint)
	}
}

// Choosing the disabled provider turns AI off rather than leaving it enabled
// with nothing to call.
func TestDisabledProviderTurnsAIOff(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	c := m.cfg
	c.AI.Enabled = true
	c.AI.Provider = string(ai.ProviderOllama)
	d.run(m.saveConfig(c))
	d.settle("seeded", func() bool { return !m.busy && m.cfg.AI.Enabled })

	d.run(m.action("Open settings"))
	selectSetting(t, m, "Provider")
	m.aiProviderCursor = providerIndex(string(ai.ProviderDisabled))
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	d.settle("off", func() bool { return !m.busy && !m.cfg.AI.Enabled })
}

// The API key is typed in and stored, but never rendered.
func TestAPIKeyIsEditedMaskedAndNeverShown(t *testing.T) {
	const secret = "sk-test-abcdef123456"
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	d.run(m.action("Open settings"))
	selectSetting(t, m, "API key")

	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	if !m.settingEdit {
		t.Fatal("Enter on the key row did not open the editor")
	}
	for _, r := range secret {
		m.modalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	// Even mid-edit the value must not be on screen.
	if view := ansi.Strip(m.View()); strings.Contains(view, secret) {
		t.Fatal("the key was rendered while being typed")
	}
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	d.settle("saved", func() bool { return !m.busy && m.cfg.AI.APIKey == secret })

	if m.settingEdit {
		t.Error("the editor stayed open after Enter")
	}
	if view := ansi.Strip(m.View()); strings.Contains(view, secret) {
		t.Fatalf("the stored key was rendered in the panel:\n%s", view)
	}
	if got := maskedKey(secret); strings.Contains(got, secret) {
		t.Errorf("maskedKey leaked the value: %q", got)
	}
	saved, err := config.Load(m.configPath)
	if err != nil || saved.AI.APIKey != secret {
		t.Fatalf("key not persisted: %v", err)
	}
}

// Esc abandons an edit instead of closing the panel, which the shared modal
// Esc would otherwise do first.
func TestEscapeCancelsTheEditNotThePanel(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	// The endpoint row is typed directly; the model row goes through the
	// picker, which is a different path.
	d.run(m.action("Open settings"))
	selectSetting(t, m, "Endpoint")
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	for _, r := range "http://example" {
		m.modalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEsc}))

	if m.settingEdit {
		t.Error("Esc left the editor open")
	}
	if m.modal != "settings" {
		t.Errorf("Esc closed the panel instead of the edit: modal=%q", m.modal)
	}
	if m.cfg.AI.Endpoint != "" {
		t.Errorf("a cancelled edit was saved: %q", m.cfg.AI.Endpoint)
	}
}

// Typed rows take every key: j and k are text there, not navigation.
func TestTypedRowKeepsNavigationKeysAsText(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	d.run(m.action("Open settings"))
	selectSetting(t, m, "Endpoint")
	before := m.choice
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	for _, r := range "jk" {
		m.modalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if m.choice != before {
		t.Errorf("typing moved the selection: %d -> %d", before, m.choice)
	}
	if got := m.settingInput.Value(); got != "jk" {
		t.Errorf("input = %q, want the typed text", got)
	}
}

// The panel names the setting that is actually wrong, at the point it can be
// fixed. A disabled provider must not be reported as a privacy-policy problem.
func TestSettingsNoticeNamesTheRealProblem(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)

	if notice := m.aiSettingsNotice(); !strings.Contains(notice, "AI is off") {
		t.Errorf("notice for a disabled setup = %q", notice)
	}
	c := m.cfg
	c.AI.Enabled = true
	c.AI.Provider = string(ai.ProviderOpenAI)
	c.AI.DefaultPolicy = "local"
	d.run(m.saveConfig(c))
	d.settle("cloud under local", func() bool { return !m.busy && m.cfg.AI.Enabled })
	if notice := m.aiSettingsNotice(); !strings.Contains(notice, "forbids") {
		t.Errorf("notice for a cloud provider under a local policy = %q", notice)
	}

	c = m.cfg
	c.AI.DefaultPolicy = "cloud"
	d.run(m.saveConfig(c))
	d.settle("needs key", func() bool { return !m.busy && m.cfg.AI.DefaultPolicy == "cloud" })
	if notice := m.aiSettingsNotice(); !strings.Contains(notice, "API key") {
		t.Errorf("notice for a cloud provider with no key = %q", notice)
	}
}

// Ctrl+G on an unconfigured setup must say so, not blame the privacy policy.
func TestUnconfiguredAIReportsItself(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	m.editor.SetValue("draft text")
	if cmd := m.startAI("review", ""); cmd != nil {
		t.Fatal("an unconfigured assistant produced a request")
	}
	if !strings.Contains(m.notice, "not configured") {
		t.Errorf("notice = %q, want it to name the missing configuration", m.notice)
	}
	if strings.Contains(m.notice, "local-only") {
		t.Errorf("a missing provider was reported as a privacy policy: %q", m.notice)
	}
}

// The panel grew past what a short window holds, so it shows a slice around
// the selection instead of overflowing the modal.
func TestSettingsPanelScrollsWhenItOutgrowsTheWindow(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	d.run(m.action("Open settings"))

	tall := ansi.Strip(m.View())
	if strings.Contains(tall, "more") {
		t.Fatalf("a window with room to spare was scrolled:\n%s", tall)
	}

	m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	selectSetting(t, m, "Background sending")
	short := ansi.Strip(m.View())
	if !strings.Contains(short, "more") {
		t.Fatalf("a short window did not scroll:\n%s", short)
	}
	// The selected row has to be among the ones actually drawn.
	if !strings.Contains(short, "Background sending") {
		t.Fatalf("the selection scrolled out of view:\n%s", short)
	}
	// And the panel must still fit: every line of the frame within the window.
	if lines := strings.Count(m.View(), "\n") + 1; lines > 24 {
		t.Errorf("rendered %d lines into a 24-line window", lines)
	}
}

// Switching AI on is one act, not two. Asking for a model is a detour on the
// way; finishing it has to finish the job, or the reader is left on the model
// row with the switch still off, where Enter only reopens the picker.
func TestEnablingCompletesOnceTheModelIsChosen(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	c := m.cfg
	c.AI.Provider = string(ai.ProviderOllama)
	c.AI.Endpoint = modelServer(t, "llama3.2")
	d.run(m.saveConfig(c))
	d.settle("seeded", func() bool { return !m.busy && m.cfg.AI.Endpoint != "" })

	d.run(m.action("Open settings"))
	selectSetting(t, m, "Enabled")
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	if m.modal != "ai-models" {
		t.Fatalf("the picker did not open: %q", m.modal)
	}
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	d.settle("enabled", func() bool { return !m.busy && m.cfg.AI.Model != "" })

	if !m.cfg.AI.Enabled {
		t.Error("choosing a model did not finish switching AI on")
	}
	// The change is shown where it was asked for.
	if f, _ := m.selectedSetting(); f.id != settingAIEnabled {
		t.Errorf("cursor left on %q, want the row Enter was pressed on", f.label)
	}
	if m.modal != "settings" {
		t.Errorf("modal = %q, want the panel", m.modal)
	}
	saved, err := config.Load(m.configPath)
	if err != nil || !saved.AI.Enabled || saved.AI.Model == "" {
		t.Fatalf("not persisted or not loadable: %+v %v", saved.AI, err)
	}
}

// The typed fallback finishes the enable too, so an unreachable provider is
// not a different outcome, only a different route.
func TestEnablingCompletesFromTheTypedFallback(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	d.run(m.action("Open settings"))
	selectSetting(t, m, "Enabled")

	cmd := m.modalKey(tea.KeyMsg{Type: tea.KeyEnter})
	d.run(cmd)
	d.settle("provider", func() bool { return !m.busy })
	d.run(m.applyAIModels(aiModelsMsg{err: ai.ErrUnavailable}))
	if !m.settingEdit || m.settingEditing != settingAIModel {
		t.Fatal("no fallback to typing the model")
	}
	for _, r := range "llama3.2" {
		m.modalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	d.settle("enabled", func() bool { return !m.busy && m.cfg.AI.Model == "llama3.2" })

	if !m.cfg.AI.Enabled {
		t.Error("typing the model did not finish switching AI on")
	}
	if f, _ := m.selectedSetting(); f.id != settingAIEnabled {
		t.Errorf("cursor left on %q", f.label)
	}
}

// Abandoning the choice abandons the enable: with no model, switching AI on
// would write a configuration the loader refuses.
func TestCancellingTheModelLeavesAIOff(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	c := m.cfg
	c.AI.Provider = string(ai.ProviderOllama)
	c.AI.Endpoint = modelServer(t, "llama3.2")
	d.run(m.saveConfig(c))
	d.settle("seeded", func() bool { return !m.busy && m.cfg.AI.Endpoint != "" })

	d.run(m.action("Open settings"))
	selectSetting(t, m, "Enabled")
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEsc}))

	if m.modal != "settings" {
		t.Errorf("Esc left the panel instead of the picker: modal=%q", m.modal)
	}
	if m.cfg.AI.Enabled {
		t.Error("AI was switched on with no model")
	}
	if m.enablingAI {
		t.Error("an abandoned enable stayed pending")
	}
	if !strings.Contains(m.notice, "stayed off") {
		t.Errorf("notice = %q, want it to say the enable did not happen", m.notice)
	}
	// A later, unrelated model choice must not switch AI on behind the reader.
	selectSetting(t, m, "Model")
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	d.settle("model", func() bool { return !m.busy && m.cfg.AI.Model != "" })
	if m.cfg.AI.Enabled {
		t.Error("an abandoned enable was resurrected by a later model choice")
	}
}

// The picker borrows m.choice from the panel, so leaving it must put the
// panel's own cursor back rather than stranding the reader on whatever row the
// picker's index happened to line up with.
func TestModelPickerRestoresThePanelCursor(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	c := m.cfg
	c.AI.Provider = string(ai.ProviderOllama)
	c.AI.Endpoint = modelServer(t, "a", "b", "c", "d")
	d.run(m.saveConfig(c))
	d.settle("seeded", func() bool { return !m.busy && m.cfg.AI.Endpoint != "" })

	d.run(m.action("Open settings"))
	selectSetting(t, m, "Model")
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	if m.modal != "ai-models" {
		t.Fatalf("picker did not open: %q", m.modal)
	}
	// Move within the picker, so its index is nowhere near the panel's row.
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyDown}))
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEsc}))

	if m.modal != "settings" {
		t.Fatalf("modal = %q, want the panel", m.modal)
	}
	if f, _ := m.selectedSetting(); f.id != settingAIModel {
		t.Errorf("returned to %q, want the row the picker was opened from", f.label)
	}
}

// The loop this guards: with the provider unreachable there is no list, and
// pressing Enter on an empty editor closed it, put the cursor back on the model
// row, and reopened the editor on the next Enter. The switch never moved and
// nothing said what to do.
func TestEmptyModelKeepsTheEditorOpen(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	d.run(m.action("Open settings"))
	selectSetting(t, m, "Model")
	d.run(m.applyAIModels(aiModelsMsg{err: ai.ErrUnavailable}))
	if !m.settingEdit {
		t.Fatal("no editor opened")
	}

	for i := 0; i < 3; i++ {
		d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
		if !m.settingEdit {
			t.Fatalf("press %d closed the editor on an empty model", i+1)
		}
		if f, _ := m.selectedSetting(); f.id != settingAIModel {
			t.Fatalf("press %d moved off the model row", i+1)
		}
	}
	if !strings.Contains(m.notice, "Type a model name") {
		t.Errorf("notice = %q, want it to say what the editor wants", m.notice)
	}
	// Esc is the way out, and it leaves AI off rather than half-configured.
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEsc}))
	if m.settingEdit {
		t.Error("Esc did not leave the editor")
	}
	if m.cfg.AI.Enabled {
		t.Error("AI was switched on with no model")
	}
}

// Clearing a model that exists is still meaningful, so an empty commit is only
// refused when there is nothing to clear.
func TestEmptyModelStillClearsAnExistingOne(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	c := m.cfg
	c.AI.Provider = string(ai.ProviderOllama)
	c.AI.Model = "llama3.2"
	c.AI.Enabled = true
	d.run(m.saveConfig(c))
	d.settle("seeded", func() bool { return !m.busy && m.cfg.AI.Model == "llama3.2" })

	d.run(m.action("Open settings"))
	selectSetting(t, m, "Model")
	d.run(m.beginSettingEdit(settingAIModel))
	for range "llama3.2" {
		m.modalKey(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	d.settle("cleared", func() bool { return !m.busy && m.cfg.AI.Model == "" })

	if m.settingEdit {
		t.Error("clearing an existing model left the editor open")
	}
	if m.cfg.AI.Enabled {
		t.Error("AI stayed on with no model, which config.Load refuses")
	}
}

// A listing that has already failed for this configuration is not attempted
// again on every Enter: that retry is what made the row and its editor bounce.
func TestFailedListingIsNotRetried(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	c := m.cfg
	c.AI.Provider = string(ai.ProviderOllama)
	c.AI.Endpoint = "http://127.0.0.1:1/v1"
	d.run(m.saveConfig(c))
	d.settle("seeded", func() bool { return !m.busy && m.cfg.AI.Endpoint != "" })

	d.run(m.action("Open settings"))
	selectSetting(t, m, "Model")
	d.run(m.applyAIModels(aiModelsMsg{err: ai.ErrUnavailable}))
	if m.modelListFailed == "" {
		t.Fatal("the failure was not remembered")
	}
	m.cancelSettingEdit()

	// Enter now goes straight to typing rather than repeating the request.
	if cmd := m.chooseAIModel(); cmd == nil {
		t.Fatal("no editor offered")
	}
	if !m.settingEdit {
		t.Error("a known-failed provider was asked again instead of opening the editor")
	}

	// A different provider is a different question, so it gets a fresh chance.
	m.cancelSettingEdit()
	selectSetting(t, m, "Provider")
	m.aiProviderCursor = providerIndex(string(ai.ProviderLMStudio))
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	d.settle("switched", func() bool { return !m.busy && m.cfg.AI.Provider == string(ai.ProviderLMStudio) })
	if m.modelListFailed != "" {
		t.Error("changing provider kept the old refusal")
	}
}

// A local provider that is not running is the common failure, and a dial error
// is not what to do about it.
func TestLocalProviderFailureNamesWhatToStart(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	c := m.cfg
	c.AI.Provider = string(ai.ProviderOllama)
	d.run(m.saveConfig(c))
	d.settle("seeded", func() bool { return !m.busy && m.cfg.AI.Provider == string(ai.ProviderOllama) })

	reason := m.modelListReason(errors.New("Get \"http://127.0.0.1:11434/v1/models\": dial tcp: connect: connection refused"))
	if !strings.Contains(reason, "ollama is not running") {
		t.Errorf("reason = %q, want it to name the provider that is down", reason)
	}
	if !strings.Contains(reason, "11434") {
		t.Errorf("reason = %q, want it to name the endpoint", reason)
	}
	if !strings.Contains(reason, "type a model name") {
		t.Errorf("reason = %q, want it to give the way forward", reason)
	}
}
