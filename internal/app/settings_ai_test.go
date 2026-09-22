package app

import (
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
	for _, label := range []string{"AI enabled", "AI provider", "AI endpoint", "AI model", "AI API key", "AI default policy"} {
		selectSetting(t, m, label)
	}
}

// Turning AI on with nothing behind it would leave every AI command failing,
// so enabling picks the local provider, which needs no key and no network.
func TestEnablingAIChoosesALocalProvider(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	d.run(m.action("Open settings"))
	selectSetting(t, m, "AI enabled")

	// With no model there is nothing to enable: the loader rejects an enabled
	// provider that names no model, so the panel asks for one instead of
	// writing a config the app could not read back.
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	if m.cfg.AI.Enabled {
		t.Fatal("AI was enabled with no model, which config.Load refuses")
	}
	if !m.settingEdit || m.settingEditing != settingAIModel {
		t.Fatal("enabling with no model did not ask for one")
	}
	for _, r := range "llama3.2" {
		m.modalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	d.settle("model", func() bool { return !m.busy && m.cfg.AI.Model == "llama3.2" })

	selectSetting(t, m, "AI enabled")
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	d.settle("saved", func() bool { return !m.busy && m.cfg.AI.Enabled })

	if got := ai.Provider(m.cfg.AI.Provider); got != ai.ProviderOllama {
		t.Fatalf("provider = %q, want a local one", got)
	}
	// The default policy is local, so the chosen provider has to satisfy it.
	policy := ai.Policy(m.cfg.AI.DefaultPolicy)
	if policy == "" {
		policy = ai.PolicyLocal
	}
	if !ai.Allowed(policy, ai.Provider(m.cfg.AI.Provider)) {
		t.Fatalf("enabling AI produced a setup its own policy forbids: %s / %s", policy, m.cfg.AI.Provider)
	}
	// The written config must load again: a panel that can write a file the
	// app then refuses would lock the reader out of their own settings.
	saved, err := config.Load(m.configPath)
	if err != nil || !saved.AI.Enabled || saved.AI.Model != "llama3.2" {
		t.Fatalf("not persisted or not loadable: %+v %v", saved.AI, err)
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
	selectSetting(t, m, "AI model")
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
	selectSetting(t, m, "AI provider")
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
	selectSetting(t, m, "AI provider")
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
	selectSetting(t, m, "AI API key")

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
	d.run(m.action("Open settings"))
	selectSetting(t, m, "AI model")
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter}))
	for _, r := range "llama3.2" {
		m.modalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	d.run(m.modalKey(tea.KeyMsg{Type: tea.KeyEsc}))

	if m.settingEdit {
		t.Error("Esc left the editor open")
	}
	if m.modal != "settings" {
		t.Errorf("Esc closed the panel instead of the edit: modal=%q", m.modal)
	}
	if m.cfg.AI.Model != "" {
		t.Errorf("a cancelled edit was saved: %q", m.cfg.AI.Model)
	}
}

// Typed rows take every key: j and k are text there, not navigation.
func TestTypedRowKeepsNavigationKeysAsText(t *testing.T) {
	m, _, _, d, _ := conversationFixture(t)
	syncPhone(t, d)
	d.run(m.action("Open settings"))
	selectSetting(t, m, "AI model")
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
