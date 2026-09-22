// Package ai defines the writing-assistant boundary and the privacy policy
// that decides whether text may leave the machine. Provider transports live in
// this package too; nothing here talks to the UI.
package ai

import (
	"context"
	"errors"
)

// Policy decides where a rewrite or review may run.
type Policy string

const (
	// PolicyLocal allows only models on the local machine.
	PolicyLocal Policy = "local"
	// PolicyCloud allows a configured remote provider.
	PolicyCloud Policy = "cloud"
	// PolicyDisabled refuses all assistance.
	PolicyDisabled Policy = "disabled"
)

// Provider names the transport. The zero value is disabled so an unset config
// never calls out.
type Provider string

const (
	ProviderDisabled     Provider = "disabled"
	ProviderOllama       Provider = "ollama"
	ProviderLMStudio     Provider = "lmstudio"
	ProviderOpenAI       Provider = "openai"
	ProviderAnthropic    Provider = "anthropic"
	ProviderDeepSeek     Provider = "deepseek"
	ProviderCustomOpenAI Provider = "custom-openai-compatible"
)

// Providers lists every accepted provider name, in install order, for pickers.
var Providers = []Provider{ProviderDisabled, ProviderOllama, ProviderLMStudio, ProviderOpenAI, ProviderAnthropic, ProviderDeepSeek, ProviderCustomOpenAI}

// Policies lists the accepted policies for pickers.
var Policies = []Policy{PolicyLocal, PolicyCloud, PolicyDisabled}

// ValidProvider reports whether name is a known provider.
func ValidProvider(name string) bool {
	for _, p := range Providers {
		if string(p) == name {
			return true
		}
	}
	return false
}

// ValidPolicy reports whether name is a known policy.
func ValidPolicy(name string) bool {
	for _, p := range Policies {
		if string(p) == name {
			return true
		}
	}
	return false
}

// Local reports whether the provider runs on this machine.
func (p Provider) Local() bool {
	return p == ProviderOllama || p == ProviderLMStudio
}

// DefaultEndpoint is the base URL used when the configuration leaves endpoint
// empty. Anthropic and the OpenAI-compatible providers each need their own
// path, which the transports add.
func (p Provider) DefaultEndpoint() string {
	switch p {
	case ProviderOllama:
		return "http://127.0.0.1:11434/v1"
	case ProviderLMStudio:
		return "http://127.0.0.1:1234/v1"
	case ProviderOpenAI:
		return "https://api.openai.com/v1"
	case ProviderAnthropic:
		return "https://api.anthropic.com"
	case ProviderDeepSeek:
		return "https://api.deepseek.com/v1"
	default:
		return ""
	}
}

// Change is one proposed edit within the original text. Offsets are rune
// offsets so a caller can apply just that span and leave Ripple's undo stack
// to record it as an ordinary edit.
type Change struct {
	Original  string
	Suggested string
	// Start and End bound Original in the source text, in runes.
	Start, End int
	// Whole is set when the suggestion replaces the entire text rather than a
	// span, which is what free-form rewrites produce.
	Whole bool
}

// ReviewRequest asks for corrections that preserve the writer's intent.
type ReviewRequest struct {
	Text   string
	Policy Policy
	// Context is optional, such as the recipient's display name, and may be
	// empty. It is never required.
	Context string
}

// ReviewResult carries the proposed changes. An empty slice means the text is
// already acceptable.
type ReviewResult struct{ Changes []Change }

// RewriteRequest asks for a full-text transformation.
type RewriteRequest struct {
	Text        string
	Instruction string
	Policy      Policy
	Context     string
}

// RewriteResult is the transformed text. The caller must still obtain explicit
// acceptance before applying it.
type RewriteResult struct{ Text string }

// WritingAssistant is the provider boundary. Implementations must be safe for
// concurrent use.
type WritingAssistant interface {
	Review(ctx context.Context, req ReviewRequest) (ReviewResult, error)
	Rewrite(ctx context.Context, req RewriteRequest) (RewriteResult, error)
}

// ErrUnavailable is returned when no provider is configured or reachable. The
// draft is always left untouched.
var ErrUnavailable = errors.New("AI unavailable")

// ErrRefused is returned when the active policy forbids the request, such as a
// local-only thread that would otherwise need a cloud provider.
var ErrRefused = errors.New("AI disabled for this conversation")

// Disabled is the null assistant, used whenever AI is off.
type Disabled struct{}

func (Disabled) Review(context.Context, ReviewRequest) (ReviewResult, error) {
	return ReviewResult{}, ErrUnavailable
}

func (Disabled) Rewrite(context.Context, RewriteRequest) (RewriteResult, error) {
	return RewriteResult{}, ErrUnavailable
}

// ResolvePolicy picks the effective policy: a thread override beats a contact
// override, which beats the global default. Nil overrides are skipped, so
// callers pass only the scopes that have an explicit value.
func ResolvePolicy(global Policy, contact, thread *Policy) Policy {
	if thread != nil {
		return *thread
	}
	if contact != nil {
		return *contact
	}
	return global
}

// Allowed reports whether a provider may serve a request under policy. A
// local-only policy never falls back to a remote provider.
func Allowed(policy Policy, provider Provider) bool {
	switch policy {
	case PolicyDisabled:
		return false
	case PolicyLocal:
		return provider.Local()
	case PolicyCloud:
		return provider != ProviderDisabled
	default:
		return false
	}
}
