package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/allisonhere/tidesms/internal/ai"
	"github.com/allisonhere/tidesms/internal/storage"
	"github.com/allisonhere/tidesms/ui/composer"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
)

// aiReviewState is an unaccepted set of proposed changes. Nothing reaches the
// editor until the user accepts it, and accepted changes are applied through
// Ripple so one undo restores the previous text.
type aiReviewState struct {
	kind      string // "review" or "rewrite"
	selection bool   // rewrite only: the request operated on a selection
	original  string
	changes   []ai.Change
	accepted  []bool
	choice    int
}

// aiResultMsg carries an assistant response back to the event loop.
type aiResultMsg struct {
	epoch     uint64
	kind      string
	selection bool
	original  string
	changes   []ai.Change
	err       error
}

// AIInstruction is one palette rewrite entry: a label and the instruction sent
// to the model. An empty instruction asks for a custom one.
type AIInstruction struct {
	Label       string
	Instruction string
}

// AIInstructions are the rewrite actions offered in the palette.
var AIInstructions = []AIInstruction{
	{"AI: Fix spelling", "Correct spelling mistakes only. Leave grammar, wording and punctuation as they are."},
	{"AI: Fix grammar", "Correct grammar mistakes only. Leave spelling and wording as they are."},
	{"AI: Polish writing", "Fix spelling, grammar, punctuation and capitalization, and rewrite awkward sentences for clear, natural flow. Preserve the writer's meaning, facts, tone and level of formality. Do not add information or answer the message. Keep it suitable for a text message."},
	{"AI: Clean up", "Fix spelling, grammar, punctuation and capitalization. Change as little as possible."},
	{"AI: Make shorter", "Rewrite more concisely while keeping the meaning."},
	{"AI: Make friendlier", "Rewrite in a warmer, friendlier tone."},
	{"AI: Make professional", "Rewrite in a clear, professional tone."},
	{"AI: Make clearer", "Rewrite so the meaning is unambiguous and easy to read."},
}

// aiAction handles the palette's AI entries. It reports whether name matched.
func (m *Model) aiAction(name string) (tea.Cmd, bool) {
	switch name {
	case "AI: Review writing":
		return m.startAI("review", ""), true
	case "AI: Custom rewrite…":
		m.startCustomRewrite()
		return nil, true
	}
	for _, in := range AIInstructions {
		if name == in.Label {
			return m.startAI("rewrite", in.Instruction), true
		}
	}
	return nil, false
}

// policyChoices is the AI privacy picker. "inherit" clears an override and
// falls back to the next scope.
var policyChoices = []string{"inherit", "local", "cloud", "disabled"}

// openAIPolicyPicker edits the privacy policy for a contact or a thread.
func (m *Model) openAIPolicyPicker(scope string) {
	m.policyScope = scope
	m.modal = "ai-policy"
	m.choices = append([]string{}, policyChoices...)
	m.choice = 0
	if id := m.aiScopeID(scope); id != "" && m.store != nil {
		if p, _, _, ok, err := m.store.AIPolicy(scope, id); err == nil && ok {
			for i, c := range policyChoices {
				if c == p {
					m.choice = i
				}
			}
		}
	}
}

func (m *Model) aiPolicyName() string {
	if m.policyScope == storage.ScopeThread {
		if t := m.history.active; t != nil && t.DisplayName != "" {
			return t.DisplayName
		}
		return "Thread"
	}
	if m.editing.Name != "" {
		return m.editing.Name
	}
	return "Contact"
}

func (m *Model) aiScopeID(scope string) string {
	if scope == storage.ScopeThread {
		if t := m.history.active; t != nil {
			return t.ID
		}
		return ""
	}
	return m.aiContactKey()
}

// commitAIPolicy stores the chosen privacy policy, or clears an override.
func (m *Model) commitAIPolicy(choice string) tea.Cmd {
	id := m.aiScopeID(m.policyScope)
	m.modal = ""
	if id == "" || m.store == nil {
		return nil
	}
	var err error
	if choice == "inherit" {
		err = m.store.ClearAIPolicy(m.policyScope, id)
	} else {
		err = m.store.SetAIPolicy(m.policyScope, id, choice, "", "")
	}
	if err != nil {
		m.notify("Could not save the AI policy", true)
		return nil
	}
	m.notify("AI policy saved", false)
	return nil
}

// aiPolicy resolves the effective privacy policy: a thread override beats a
// contact override, which beats the global default.
func (m *Model) aiPolicy() ai.Policy {
	global := ai.Policy(m.cfg.AI.DefaultPolicy)
	if global == "" {
		global = ai.PolicyLocal
	}
	var contactPolicy, threadPolicy *ai.Policy
	if m.store != nil {
		if p, _, _, ok, err := m.store.AIPolicy(storage.ScopeContact, m.aiContactKey()); err == nil && ok {
			v := ai.Policy(p)
			contactPolicy = &v
		}
		if t := m.history.active; t != nil {
			if p, _, _, ok, err := m.store.AIPolicy(storage.ScopeThread, t.ID); err == nil && ok {
				v := ai.Policy(p)
				threadPolicy = &v
			}
		}
	}
	return ai.ResolvePolicy(global, contactPolicy, threadPolicy)
}

func (m *Model) aiContactKey() string {
	if m.recipient.ID != "" {
		return m.recipient.ID
	}
	return m.recipient.PhoneNumber
}

// cancelAI aborts an in-flight request, if any.
func (m *Model) cancelAI() {
	if m.aiCancel != nil {
		m.aiCancel()
		m.aiCancel = nil
	}
	if m.aiBusy {
		m.aiBusy = false
		m.aiEpoch++ // Ignore results that arrive after cancellation.
		m.notify("AI cancelled", false)
	}
}

// startAI runs a review or rewrite off the event loop. The draft is never sent
// to a provider the active policy forbids, and the result is only ever applied
// after explicit acceptance.
func (m *Model) startAI(kind, instruction string) tea.Cmd {
	if !m.loaded {
		return nil
	}
	// Whether a provider exists is asked before whether the policy permits it.
	// ProviderDisabled is not local, so a setup with no assistant at all used to
	// fall out of the local-only branch and blame the privacy policy for a
	// missing configuration.
	provider := ai.Provider(m.cfg.AI.Provider)
	if m.assistant == nil || !m.cfg.AI.Enabled || provider == ai.ProviderDisabled || strings.TrimSpace(m.cfg.AI.Model) == "" {
		m.notify("AI is not configured — Ctrl+P → Open settings", true)
		return nil
	}
	policy := m.aiPolicy()
	if policy == ai.PolicyDisabled {
		m.notify("AI is off for this conversation", true)
		return nil
	}
	if !ai.Allowed(policy, provider) {
		m.notify("AI is local-only for this conversation; "+string(provider)+" is a cloud provider", true)
		return nil
	}
	target := m.editor.Value()
	selection := false
	if kind == "rewrite" {
		if sel := m.editor.Selected(); sel != "" {
			target = sel
			selection = true
		}
	}
	if strings.TrimSpace(target) == "" {
		m.notify("Write something first", true)
		return nil
	}
	m.cancelAI()
	ctx, cancel := context.WithCancel(m.ctx)
	m.aiCancel = cancel
	m.aiBusy = true
	m.aiEpoch++
	epoch := m.aiEpoch
	assistant := m.assistant
	m.notify("Asking the assistant…", false)

	return func() tea.Msg {
		defer cancel()
		if kind == "review" {
			res, err := assistant.Review(ctx, ai.ReviewRequest{Text: target, Policy: policy})
			return aiResultMsg{epoch: epoch, kind: kind, original: target, changes: res.Changes, err: err}
		}
		res, err := assistant.Rewrite(ctx, ai.RewriteRequest{Text: target, Instruction: instruction, Policy: policy})
		var changes []ai.Change
		if err == nil && res.Text != "" && res.Text != target {
			changes = []ai.Change{{Original: target, Suggested: res.Text, Whole: true}}
		}
		return aiResultMsg{epoch: epoch, kind: kind, selection: selection, original: target, changes: changes, err: err}
	}
}

// handleAIResult folds an assistant response into the UI.
func (m *Model) handleAIResult(v aiResultMsg) {
	if v.epoch != m.aiEpoch {
		return
	}
	m.aiBusy = false
	if m.aiCancel != nil {
		m.aiCancel()
		m.aiCancel = nil
	}
	if v.err != nil {
		if errors.Is(v.err, context.Canceled) {
			m.notify("AI cancelled", false)
			return
		}
		// The draft is never discarded on failure.
		m.notify("AI unavailable — draft unchanged", true)
		return
	}
	// If the draft changed while the request was in flight, the offsets no
	// longer describe it, so the suggestion is dropped rather than misapplied.
	if (v.kind == "review" || (v.kind == "rewrite" && !v.selection)) && m.editor.Value() != v.original {
		m.notify("Draft changed; AI suggestion discarded", true)
		return
	}
	if v.kind == "rewrite" && v.selection && m.editor.Selected() != v.original {
		m.notify("Selection changed; AI suggestion discarded", true)
		return
	}
	if len(v.changes) == 0 {
		m.notify("No changes suggested", false)
		return
	}
	m.review = aiReviewState{kind: v.kind, selection: v.selection, original: v.original, changes: v.changes, accepted: make([]bool, len(v.changes))}
	m.choice = 0
	m.modal = "ai-review"
	m.notify("AI suggestions ready", false)
	m.setReviewMarkers()
}

func (m *Model) setReviewMarkers() {
	if m.review.kind == "review" {
		markers := make([]composer.Marker, 0, len(m.review.changes))
		for _, c := range m.review.changes {
			markers = append(markers, composer.Marker{Start: c.Start, End: c.End})
		}
		m.editor.SetMarkers(markers)
		return
	}
	if !m.review.selection {
		m.editor.SetMarkers([]composer.Marker{{Start: 0, End: len([]rune(m.review.original))}})
	}
}

// applyReview recomputes the draft from the accepted subset. Review changes
// carry offsets into the original text, so recomputing from the original keeps
// them valid however many decisions have been made.
func (m *Model) applyReview() {
	r := m.review
	if len(r.changes) == 0 {
		return
	}
	if r.kind == "rewrite" {
		text := r.original
		if len(r.accepted) > 0 && r.accepted[0] {
			text = r.changes[0].Suggested
		}
		if r.selection {
			m.editor.ApplySelection(text)
		} else {
			m.editor.ApplyAll(text)
		}
		return
	}
	m.editor.ApplyAll(ai.Apply(r.original, r.changes, r.accepted))
}

func (m *Model) closeReview() {
	m.editor.ClearMarkers()
	m.modal = ""
	m.review = aiReviewState{}
}

// reviewKey handles the review modal.
func (m *Model) reviewKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "n", "down", "j":
		m.review.choice = min(len(m.review.changes)-1, m.review.choice+1)
	case "p", "up", "k":
		m.review.choice = max(0, m.review.choice-1)
	case "a":
		m.decideReview(true)
	case "r":
		m.decideReview(false)
	case "A":
		for i := range m.review.accepted {
			m.review.accepted[i] = true
		}
		m.applyReview()
		m.closeReview()
	case "e":
		m.aiInput.SetValue(m.review.changes[m.review.choice].Suggested)
		m.aiInput.CursorEnd()
		m.aiInput.Focus()
		m.modal = "ai-edit"
	case "esc", "q":
		m.closeReview()
	}
	return nil
}

func (m *Model) decideReview(accept bool) {
	if m.review.choice < 0 || m.review.choice >= len(m.review.accepted) {
		return
	}
	m.review.accepted[m.review.choice] = accept
	m.applyReview()
	// Applying rewrites the whole draft, so the original offsets no longer
	// describe it; the inline marks are cleared rather than drawn wrong.
	m.editor.ClearMarkers()
}

// renderReview lists the proposed changes, each with its decision so far.
func (m *Model) renderReview() string {
	var b strings.Builder
	for i, c := range m.review.changes {
		mark := " "
		if i < len(m.review.accepted) {
			switch {
			case m.review.accepted[i]:
				mark = "✓"
			default:
				mark = "·"
			}
		}
		line := fmt.Sprintf("%s %s → %s", mark, c.Original, c.Suggested)
		if i == m.review.choice {
			line = "▸ " + line
		} else {
			line = "  " + line
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// renderReviewPreview shows the original and the current suggestion in full.
func (m *Model) renderReviewPreview() string {
	if len(m.review.changes) == 0 {
		return ""
	}
	c := m.review.changes[max(0, min(m.review.choice, len(m.review.changes)-1))]
	return "Original\n" + c.Original + "\n\nSuggested\n" + c.Suggested
}

// startCustomRewrite opens the instruction prompt.
func (m *Model) startCustomRewrite() {
	m.aiInput.SetValue("")
	m.aiInput.CursorEnd()
	m.aiInput.Focus()
	m.modal = "ai-instruction"
}

// instructionKey handles the custom-instruction prompt.
func (m *Model) instructionKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		m.modal = ""
		return nil
	case "enter":
		instruction := strings.TrimSpace(m.aiInput.Value())
		m.modal = ""
		if instruction == "" {
			return nil
		}
		return m.startAI("rewrite", instruction)
	}
	var cmd tea.Cmd
	m.aiInput, cmd = m.aiInput.Update(k)
	return cmd
}

// editSuggestionKey handles editing one suggestion before it is accepted.
func (m *Model) editSuggestionKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		m.modal = "ai-review"
		return nil
	case "enter":
		if m.review.choice >= 0 && m.review.choice < len(m.review.changes) {
			m.review.changes[m.review.choice].Suggested = strings.TrimSpace(m.aiInput.Value())
		}
		m.modal = "ai-review"
		return nil
	}
	var cmd tea.Cmd
	m.aiInput, cmd = m.aiInput.Update(k)
	return cmd
}
