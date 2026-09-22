package ai

import (
	"context"
	"errors"
	"testing"
)

func TestResolvePolicyPrefersMostSpecific(t *testing.T) {
	global := PolicyCloud
	contact := PolicyLocal
	thread := PolicyDisabled
	if got := ResolvePolicy(global, &contact, &thread); got != PolicyDisabled {
		t.Fatalf("thread should win: %q", got)
	}
	if got := ResolvePolicy(global, &contact, nil); got != PolicyLocal {
		t.Fatalf("contact should beat global: %q", got)
	}
	if got := ResolvePolicy(global, nil, nil); got != PolicyCloud {
		t.Fatalf("global fallback: %q", got)
	}
}

func TestAllowedNeverFallsBackToCloud(t *testing.T) {
	cases := []struct {
		policy   Policy
		provider Provider
		want     bool
	}{
		{PolicyLocal, ProviderOllama, true},
		{PolicyLocal, ProviderLMStudio, true},
		{PolicyLocal, ProviderOpenAI, false},
		{PolicyLocal, ProviderDisabled, false},
		{PolicyCloud, ProviderOpenAI, true},
		{PolicyCloud, ProviderOllama, true},
		{PolicyCloud, ProviderDisabled, false},
		{PolicyDisabled, ProviderOllama, false},
	}
	for _, c := range cases {
		if got := Allowed(c.policy, c.provider); got != c.want {
			t.Errorf("Allowed(%q,%q) = %v, want %v", c.policy, c.provider, got, c.want)
		}
	}
}

func TestDisabledAssistantChangesNothing(t *testing.T) {
	var a WritingAssistant = Disabled{}
	if _, err := a.Review(context.Background(), ReviewRequest{Text: "hi"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("review err = %v", err)
	}
	if _, err := a.Rewrite(context.Background(), RewriteRequest{Text: "hi"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("rewrite err = %v", err)
	}
}

func TestValidators(t *testing.T) {
	if !ValidProvider("ollama") || ValidProvider("gpt") {
		t.Fatal("provider validation")
	}
	if !ValidProvider("deepseek") {
		t.Fatal("deepseek should be a known provider")
	}
	if !ValidPolicy("local") || ValidPolicy("maybe") {
		t.Fatal("policy validation")
	}
}

func TestDeepSeekIsCloudWithDefaultEndpoint(t *testing.T) {
	if ProviderDeepSeek.Local() {
		t.Fatal("deepseek is a cloud provider")
	}
	if Allowed(PolicyLocal, ProviderDeepSeek) {
		t.Fatal("deepseek must be refused under a local-only policy")
	}
	if !Allowed(PolicyCloud, ProviderDeepSeek) {
		t.Fatal("deepseek should be allowed under a cloud policy")
	}
	if got := ProviderDeepSeek.DefaultEndpoint(); got != "https://api.deepseek.com/v1" {
		t.Fatalf("endpoint = %q", got)
	}
}
