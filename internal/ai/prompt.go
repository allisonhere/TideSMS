package ai

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"
)

const reviewSystem = `You are a careful copy editor for short text and SMS messages.
Correct spelling, grammar, punctuation and capitalization, and fix obviously
mistyped words. Preserve the writer's meaning, tone and formatting. Change as
little as possible. Respond with strict JSON only, no prose and no markdown:
{"changes":[{"original":"<text as written>","suggested":"<corrected text>"}]}
Each original must be an exact, unique substring of the input. If nothing needs
correcting, return {"changes":[]}.`

const rewriteSystemBase = `You rewrite short text and SMS messages. Return only the rewritten message: no
quotes, no labels, no explanation, no markdown. Preserve the writer's intent.`

func rewriteSystem(instruction string) string {
	if strings.TrimSpace(instruction) == "" {
		return rewriteSystemBase
	}
	return rewriteSystemBase + "\nApply this instruction: " + instruction
}

// reviewUser keeps the instruction and the text clearly separated so the model
// edits rather than answers.
func reviewUser(req ReviewRequest) string {
	if req.Context == "" {
		return "Text:\n" + req.Text
	}
	return "Context: " + req.Context + "\nText:\n" + req.Text
}

type changeJSON struct {
	Original  string `json:"original"`
	Suggested string `json:"suggested"`
}

// parseChanges reads the model's JSON and turns each pair into a rune-offset
// change within text. Entries whose original cannot be found, that change
// nothing, or that overlap an earlier change are dropped. Malformed JSON is an
// error, so the caller leaves the draft untouched.
func parseChanges(raw, text string) ([]Change, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var parsed struct {
		Changes []changeJSON `json:"changes"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, err
	}
	runes := []rune(text)
	candidates := make([]Change, 0, len(parsed.Changes))
	for _, c := range parsed.Changes {
		if c.Original == "" || c.Original == c.Suggested {
			continue
		}
		start, end, ok := findRunes(runes, c.Original)
		if !ok {
			continue
		}
		candidates = append(candidates, Change{Original: c.Original, Suggested: c.Suggested, Start: start, End: end})
	}
	// Sort before dropping overlaps: the model's order is arbitrary, and an
	// earlier change must not be rejected because a later one was listed first.
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Start < candidates[j].Start })
	var out []Change
	usedUntil := -1
	for _, c := range candidates {
		if c.Start < usedUntil {
			continue
		}
		usedUntil = c.End
		out = append(out, c)
	}
	return out, nil
}

// findRunes finds the first occurrence of want in text and returns rune bounds.
func findRunes(text []rune, want string) (int, int, bool) {
	hay := string(text)
	byteIdx := strings.Index(hay, want)
	if byteIdx < 0 {
		return 0, 0, false
	}
	start := utf8.RuneCountInString(hay[:byteIdx])
	end := start + utf8.RuneCountInString(want)
	return start, end, true
}

// cleanRewrite strips the furniture a model may wrap an answer in.
func cleanRewrite(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			s = strings.TrimSpace(s[1 : len(s)-1])
		}
	}
	return s
}

// Apply returns text with the given changes applied in order. It is used both
// by the UI, to preview an accepted subset, and by tests. A change out of range
// is ignored.
func Apply(text string, changes []Change, accepted []bool) string {
	runes := []rune(text)
	if len(changes) != len(accepted) {
		return text
	}
	// Apply from the end so earlier offsets stay valid.
	for i := len(changes) - 1; i >= 0; i-- {
		if !accepted[i] {
			continue
		}
		c := changes[i]
		if c.Start < 0 || c.End > len(runes) || c.Start > c.End {
			continue
		}
		repl := []rune(c.Suggested)
		next := make([]rune, 0, len(runes)-(c.End-c.Start)+len(repl))
		next = append(next, runes[:c.Start]...)
		next = append(next, repl...)
		next = append(next, runes[c.End:]...)
		runes = next
	}
	return string(runes)
}
