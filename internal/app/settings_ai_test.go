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

// Explicit catalogue setup does not send conversation text or change its policy.
func TestLocalPolicyAllowsExplicitCloudModelSetup(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	m.cfg.AI.Provider = "deepseek"
	m.cfg.AI.APIKey = "test-key"
	m.cfg.AI.Endpoint = modelServer(t, "deepseek-chat")
	d.run(m.action("Open settings"))
	selectSetting(t, m, "Model")
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	if m.modal != "ai-models" {
		t.Fatalf("model setup blocked: %q", m.notice)
	}
	if m.cfg.AI.DefaultPolicy != "local" {
		t.Fatal("model setup changed writing policy")
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
	m.localProviderStatus[ai.ProviderLMStudio] = localProviderStatus{endpoint: ai.ProviderLMStudio.DefaultEndpoint(), available: true}
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

func TestEmptyModelLeavesEditorAndAllowsNavigation(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyEnter, tea.KeyTab, tea.KeyDown, tea.KeyUp, tea.KeyShiftTab} {
		t.Run(tea.KeyMsg{Type: key}.String(), func(t *testing.T) {
			m, _, _, d, _ := conversationFixture(t)
			syncPhone(t, d)
			d.run(m.action("Open settings"))
			selectSetting(t, m, "Model")
			d.run(m.beginSettingEdit(settingAIModel))
			d.run(m.modalKey(tea.KeyMsg{Type: key}))
			if m.settingEdit || m.cfg.AI.Enabled || m.modal != "settings" {
				t.Fatal("empty model trapped editor or enabled AI")
			}
		})
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
	m.localProviderStatus[ai.ProviderLMStudio] = localProviderStatus{endpoint: ai.ProviderLMStudio.DefaultEndpoint(), available: true}
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

func TestAIEnableIndependentOfProviderAvailability(t *testing.T) {
	for _, provider := range []string{"disabled", "ollama", "openai"} {
		t.Run(provider, func(t *testing.T) {
			m, _, _, d, _ := conversationFixture(t)
			syncPhone(t, d)
			m.cfg.AI.Provider = provider
			m.cfg.AI.Model = ""
			d.run(m.action("Open settings"))
			m.localProviderStatus = localProvidersMsg{ai.ProviderOllama: {endpoint: ai.ProviderOllama.DefaultEndpoint(), available: false}}
			selectSetting(t, m, "Enabled")
			d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
			if !m.cfg.AI.Enabled || m.cfg.AI.Provider != provider || m.settingEdit || m.modal != "settings" {
				t.Fatalf("toggle changed setup: %+v", m.cfg.AI)
			}
			if f, _ := m.selectedSetting(); f.id != settingAIEnabled {
				t.Fatal("toggle moved focus")
			}
			saved, err := config.Load(m.configPath)
			if err != nil || !saved.AI.Enabled {
				t.Fatalf("enable did not survive reload: %v", err)
			}
		})
	}
}

func TestUnavailableOllamaCannotBeSelected(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	d.run(m.action("Open settings"))
	m.localProviderStatus = localProvidersMsg{ai.ProviderOllama: {endpoint: ai.ProviderOllama.DefaultEndpoint(), available: false}}
	selectSetting(t, m, "Provider")
	m.aiProviderCursor = providerIndex("ollama")
	if !strings.Contains(m.settingsValue(settingAIProvider, true), "unavailable") {
		t.Fatal("offline provider not marked")
	}
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	if m.cfg.AI.Provider == "ollama" {
		t.Fatal("offline provider selected")
	}
	m.aiProviderCursor = providerIndex("openai")
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	if m.cfg.AI.Provider != "openai" {
		t.Fatal("offline Ollama blocked another provider")
	}
}

func TestLocalProviderProbeRecovers(t *testing.T) {
	m, _, _, _, _ := conversationFixture(t)
	m.cfg.AI.Provider = "ollama"
	m.cfg.AI.Endpoint = modelServer(t, "test-model")
	result := m.probeLocalProviders()().(localProvidersMsg)
	if !result[ai.ProviderOllama].available {
		t.Fatal("running provider unavailable")
	}
	m.localProviderStatus = result
	if m.localProviderUnavailable(ai.ProviderOllama) {
		t.Fatal("running provider disabled")
	}
}

func TestAPIKeyPrecedesAndPopulatesModel(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	m.cfg.AI.Provider = "deepseek"
	m.cfg.AI.Endpoint = modelServer(t, "test-model")
	d.run(m.action("Open settings"))
	selectSetting(t, m, "API key")
	keyRow := m.choice
	selectSetting(t, m, "Model")
	if keyRow >= m.choice {
		t.Fatal("API key must precede model")
	}
	selectSetting(t, m, "API key")
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	m.settingInput.SetValue("test-key")
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	if m.modal != "ai-models" || len(m.choices) != 2 || m.choices[0] != "test-model" {
		t.Fatalf("models not populated: %q %v", m.modal, m.choices)
	}
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	saved, err := config.Load(m.configPath)
	if err != nil || saved.AI.Model != "test-model" {
		t.Fatalf("model not saved: %v", err)
	}
}

func TestSavingKeyRetriesFailedDeepSeekListing(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer test-key" || r.ContentLength > 0 {
			t.Error("unexpected catalogue request")
		}
		if attempts == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"deepseek-chat"},{"id":"deepseek-reasoner"}]}`))
	}))
	defer srv.Close()
	m.cfg.AI.Provider = "deepseek"
	m.cfg.AI.Endpoint = srv.URL + "/v1"
	d.run(m.action("Open settings"))
	for i := 0; i < 2; i++ {
		selectSetting(t, m, "API key")
		d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
		m.settingInput.SetValue("test-key")
		if i == 1 {
			m.modelListFailed = m.modelListKey()
		}
		d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
		if i == 0 {
			if m.modal != "settings" || m.settingEdit {
				t.Fatal("rejected key forced model entry")
			}
			if f, _ := m.selectedSetting(); f.id != settingAIKey {
				t.Fatal("rejected key did not return to API key")
			}
		}
	}
	if attempts != 2 || m.modal != "ai-models" || len(m.choices) != 3 {
		t.Fatalf("retry did not populate models: %d %q %v", attempts, m.modal, m.choices)
	}
	if m.cfg.AI.DefaultPolicy != "local" {
		t.Fatal("catalogue request changed writing policy")
	}
}

func TestOllamaCustomEndpointSurvivesProviderSwitch(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	endpoint := modelServer(t, "local-model")
	m.cfg.AI.Provider = "deepseek"
	m.cfg.AI.OllamaEndpoint = endpoint
	d.run(m.action("Open settings"))
	if m.localProviderUnavailable(ai.ProviderOllama) {
		t.Fatal("custom Ollama endpoint marked unavailable")
	}
	selectSetting(t, m, "Provider")
	m.aiProviderCursor = providerIndex("ollama")
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	if m.cfg.AI.Endpoint != endpoint {
		t.Fatal("provider selection lost custom endpoint")
	}
	selectSetting(t, m, "Model")
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	if m.modal != "ai-models" || m.choices[0] != "local-model" {
		t.Fatal("models not fetched from custom endpoint")
	}
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEsc}))
	selectSetting(t, m, "Provider")
	m.aiProviderCursor = providerIndex("deepseek")
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	saved, err := config.Load(m.configPath)
	if err != nil || saved.AI.OllamaEndpoint != endpoint || saved.AI.Endpoint != "" {
		t.Fatalf("custom endpoint not retained separately: %v", err)
	}
}

func TestSettingsRestoresEachProvidersCredentials(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	m.cfg.AI.Provider = "deepseek"
	m.cfg.AI.APIKey = "deepseek-secret"
	m.cfg.AI.Model = "deepseek-model"
	d.run(m.action("Open settings"))
	selectSetting(t, m, "Provider")
	m.aiProviderCursor = providerIndex("openai")
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	if m.cfg.AI.APIKey != "" {
		t.Fatal("DeepSeek key leaked to OpenAI")
	}
	c := m.cfg
	c.AI.APIKey = "openai-secret"
	c.AI.Model = "openai-model"
	d.run(m.saveConfig(c))
	for _, provider := range []string{"deepseek", "openai", "deepseek"} {
		m.aiProviderCursor = providerIndex(provider)
		d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
		saved, err := config.Load(m.configPath)
		if err != nil || saved.AI.APIKey != provider+"-secret" || saved.AI.Model != provider+"-model" {
			t.Fatal("provider credentials were not restored and saved")
		}
		view := ansi.Strip(m.View())
		if strings.Contains(view, "deepseek-secret") || strings.Contains(view, "openai-secret") {
			t.Fatal("provider credentials visible in settings")
		}
	}
}
