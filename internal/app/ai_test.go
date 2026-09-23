package app

import (
	"errors"
	"github.com/allisonhere/tidesms/internal/ai"
	aifake "github.com/allisonhere/tidesms/internal/ai/fake"
	"github.com/allisonhere/tidesms/internal/storage"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

// aiFixture readies a composer with a local Ollama-style policy and a fake
// assistant that answers deterministically.
func aiFixture(t *testing.T, a *aifake.Assistant) (*Model, *fakeBackend) {
	t.Helper()
	m, f, _ := fixture(t)
	m.cfg.AI.Enabled = true
	m.cfg.AI.Provider = "ollama"
	m.cfg.AI.Model = "test"
	m.cfg.AI.DefaultPolicy = "local"
	m.assistant = a
	m.choose(m.contacts[0])
	typeText(m, "teh cat")
	return m, f
}

func runAIAction(t *testing.T, m *Model, name string) {
	t.Helper()
	cmd := m.action(name)
	if cmd == nil {
		// A refusal (disabled or local-only) returns no command on purpose.
		return
	}
	// The command runs the assistant and returns an aiResultMsg.
	if _, next := m.Update(cmd()); next != nil {
		m.Update(next())
	}
}

func TestAIReviewAcceptAndUndo(t *testing.T) {
	a := &aifake.Assistant{ReviewResult: ai.ReviewResult{Changes: []ai.Change{{Original: "teh", Suggested: "the", Start: 0, End: 3}}}}
	m, _ := aiFixture(t, a)
	runAIAction(t, m, "AI: Review writing")
	if m.modal != "ai-review" {
		t.Fatalf("review modal not open: %q", m.modal)
	}
	if len(a.ReviewCalls) != 1 || a.ReviewCalls[0].Text != "teh cat" {
		t.Fatalf("assistant got %+v", a.ReviewCalls)
	}
	pressReview(m, "a")
	if m.editor.Value() != "the cat" {
		t.Fatalf("accept did not apply: %q", m.editor.Value())
	}
	if !m.editor.Undo() || m.editor.Value() != "teh cat" {
		t.Fatalf("undo did not restore: %q", m.editor.Value())
	}
}

func TestAIReviewRejectLeavesDraft(t *testing.T) {
	a := &aifake.Assistant{ReviewResult: ai.ReviewResult{Changes: []ai.Change{{Original: "teh", Suggested: "the", Start: 0, End: 3}}}}
	m, _ := aiFixture(t, a)
	runAIAction(t, m, "AI: Review writing")
	pressReview(m, "r")
	if m.editor.Value() != "teh cat" {
		t.Fatalf("reject changed the draft: %q", m.editor.Value())
	}
}

func TestAIUnavailableLeavesDraftUntouched(t *testing.T) {
	a := &aifake.Assistant{ReviewError: ai.ErrUnavailable}
	m, _ := aiFixture(t, a)
	runAIAction(t, m, "AI: Review writing")
	if m.modal != "" || m.editor.Value() != "teh cat" {
		t.Fatalf("unavailable assistant touched the draft: modal=%q value=%q", m.modal, m.editor.Value())
	}
	if !m.failed {
		t.Fatal("failure should be reported")
	}
}

func TestAICancellationLeavesDraftUntouched(t *testing.T) {
	a := &aifake.Assistant{ReviewError: errors.New("context canceled")}
	m, _ := aiFixture(t, a)
	runAIAction(t, m, "AI: Review writing")
	if m.editor.Value() != "teh cat" {
		t.Fatalf("cancelled request changed the draft: %q", m.editor.Value())
	}
}

func TestAIPolicyDisabledRefusesLocally(t *testing.T) {
	a := &aifake.Assistant{}
	m, _ := aiFixture(t, a)
	m.cfg.AI.DefaultPolicy = "disabled"
	runAIAction(t, m, "AI: Review writing")
	if len(a.ReviewCalls) != 0 {
		t.Fatal("disabled policy still called the provider")
	}
	if !m.failed {
		t.Fatal("disabled policy should explain itself")
	}
}

func TestAILocalPolicyNeverFallsBackToCloud(t *testing.T) {
	a := &aifake.Assistant{}
	m, _ := aiFixture(t, a)
	m.cfg.AI.Provider = "deepseek"
	m.cfg.AI.DefaultPolicy = "local"
	runAIAction(t, m, "AI: Review writing")
	if len(a.ReviewCalls) != 0 {
		t.Fatal("local-only policy called a cloud provider")
	}
}

// A per-contact override disables AI for that person without touching the
// global default.
func TestAIContactPolicyOverrideDisables(t *testing.T) {
	a := &aifake.Assistant{}
	m, _ := aiFixture(t, a)
	if err := m.store.SetAIPolicy(storage.ScopeContact, m.contacts[0].ID, "disabled", "", ""); err != nil {
		t.Fatal(err)
	}
	runAIAction(t, m, "AI: Review writing")
	if len(a.ReviewCalls) != 0 {
		t.Fatal("contact override was ignored")
	}
	if m.aiPolicy() != ai.PolicyDisabled {
		t.Fatalf("policy = %q", m.aiPolicy())
	}
}

func TestAIRewriteCustomInstruction(t *testing.T) {
	a := &aifake.Assistant{RewriteResult: ai.RewriteResult{Text: "Yep, I'll be there."}}
	m, _ := aiFixture(t, a)
	cmd := m.action("AI: Custom rewrite…")
	if cmd != nil {
		t.Fatal("custom rewrite should open a prompt first")
	}
	if m.modal != "ai-instruction" {
		t.Fatalf("instruction prompt not open: %q", m.modal)
	}
	m.aiInput.SetValue("make it casual")
	_, next := m.Update(m.modalKey(tea.KeyMsg{Type: tea.KeyEnter})())
	if next != nil {
		m.Update(next())
	}
	if m.modal != "ai-review" {
		t.Fatalf("rewrite review not open: %q", m.modal)
	}
	if len(a.RewriteCalls) != 1 || a.RewriteCalls[0].Instruction != "make it casual" {
		t.Fatalf("instruction not sent: %+v", a.RewriteCalls)
	}
	pressReview(m, "a")
	if m.editor.Value() != "Yep, I'll be there." {
		t.Fatalf("rewrite not applied: %q", m.editor.Value())
	}
}

func TestCtrlGOpensReview(t *testing.T) {
	a := &aifake.Assistant{ReviewResult: ai.ReviewResult{}}
	m, _ := aiFixture(t, a)
	// No changes is reported rather than opening an empty modal.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	if cmd == nil {
		t.Fatal("ctrl+g produced no request")
	}
	m.Update(cmd())
	if m.modal != "" {
		t.Fatalf("an empty review should not open a modal: %q", m.modal)
	}
}

func pressReview(m *Model, key string) {
	k := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	m.modalKey(k)
}

func TestAICompletionReplacesPendingStatusAfterReviewCloses(t *testing.T) {
	for _, action := range []string{"AI: Review writing", "AI: Make clearer"} {
		for _, closeKey := range []string{"esc", "A"} {
			t.Run(action+"/"+closeKey, func(t *testing.T) {
				a := &aifake.Assistant{
					ReviewResult:  ai.ReviewResult{Changes: []ai.Change{{Original: "teh", Suggested: "the", Start: 0, End: 3}}},
					RewriteResult: ai.RewriteResult{Text: "the cat"},
				}
				m, _ := aiFixture(t, a)
				cmd := m.action(action)
				if cmd == nil || !m.aiBusy || !strings.Contains(m.notice, "Asking") {
					t.Fatal("request did not start")
				}
				m.Update(cmd())
				if m.aiBusy || m.aiCancel != nil || m.notice != "AI suggestions ready" {
					t.Fatalf("completion left stale state: %q", m.notice)
				}
				if closeKey == "esc" {
					m.modalKey(tea.KeyMsg{Type: tea.KeyEsc})
				} else {
					pressReview(m, closeKey)
				}
				if m.modal != "" || strings.Contains(m.View(), "Asking the assistant") {
					t.Fatal("pending status survived closing review")
				}
			})
		}
	}
}

func TestAICancelImmediatelyClearsBusyAndIgnoresLateResult(t *testing.T) {
	a := &aifake.Assistant{ReviewResult: ai.ReviewResult{Changes: []ai.Change{{Original: "teh", Suggested: "the", Start: 0, End: 3}}}}
	m, _ := aiFixture(t, a)
	cmd := m.startAI("review", "")
	m.cancelAI()
	if m.aiBusy || m.aiCancel != nil || m.notice != "AI cancelled" {
		t.Fatal("cancellation left pending status")
	}
	// Simulate an assistant that returns success despite cancellation.
	m.Update(cmd())
	if m.modal != "" || m.notice != "AI cancelled" || m.editor.Value() != "teh cat" {
		t.Fatal("late result revived cancelled request")
	}
}

func TestPolishWritingPreviewAcceptRejectAndUndo(t *testing.T) {
	const polished = "The cat is here."
	for _, accept := range []bool{false, true} {
		a := &aifake.Assistant{RewriteResult: ai.RewriteResult{Text: polished}}
		m, _ := aiFixture(t, a)
		found := false
		for _, name := range commands {
			if name == "AI: Polish writing" {
				found = true
			}
		}
		if !found {
			t.Fatal("polish missing from palette")
		}
		runAIAction(t, m, "AI: Polish writing")
		if len(a.RewriteCalls) != 1 || !strings.Contains(a.RewriteCalls[0].Instruction, "spelling, grammar") {
			t.Fatal("polish instruction not sent")
		}
		if m.modal != "ai-review" || m.editor.Value() != "teh cat" {
			t.Fatal("polish bypassed preview")
		}
		if accept {
			pressReview(m, "a")
			if m.editor.Value() != polished {
				t.Fatal("accept did not apply polish")
			}
			if !m.editor.Undo() || m.editor.Value() != "teh cat" {
				t.Fatal("polish could not be undone")
			}
		} else {
			pressReview(m, "r")
			if m.editor.Value() != "teh cat" {
				t.Fatal("reject applied polish")
			}
		}
	}
}

func TestPolishDiscardsResultAfterDraftChanges(t *testing.T) {
	a := &aifake.Assistant{RewriteResult: ai.RewriteResult{Text: "The cat."}}
	m, _ := aiFixture(t, a)
	cmd := m.action("AI: Polish writing")
	m.editor.SetValue("new draft")
	m.Update(cmd())
	if m.modal != "" || m.editor.Value() != "new draft" || !strings.Contains(m.notice, "discarded") {
		t.Fatal("stale polish result was offered")
	}
}
