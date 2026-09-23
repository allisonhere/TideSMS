package ai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListModelsOpenAICompatible(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":[{"id":"qwen2.5"},{"id":"llama3.2"},{"id":"llama3.2"},{"id":"  "}]}`))
	}))
	defer srv.Close()

	got, err := ListModels(context.Background(), Runtime{
		Provider: ProviderOllama, Endpoint: srv.URL + "/v1", APIKey: "k",
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if gotPath != "/v1/models" {
		t.Errorf("path = %q, want /v1/models", gotPath)
	}
	if gotAuth != "Bearer k" {
		t.Errorf("auth = %q", gotAuth)
	}
	// Sorted, deduplicated, and blanks dropped, so the picker is readable.
	if strings.Join(got, ",") != "llama3.2,qwen2.5" {
		t.Errorf("models = %v", got)
	}
}

func TestListModelsAnthropicUsesItsOwnPathAndHeader(t *testing.T) {
	var gotPath, gotKey, gotVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotKey, gotVersion = r.URL.Path, r.Header.Get("x-api-key"), r.Header.Get("anthropic-version")
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-opus-5"}]}`))
	}))
	defer srv.Close()

	got, err := ListModels(context.Background(), Runtime{
		Provider: ProviderAnthropic, Endpoint: srv.URL, APIKey: "secret",
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if gotPath != "/v1/models" {
		t.Errorf("path = %q; Anthropic carries the version in the path, not the endpoint", gotPath)
	}
	if gotKey != "secret" || gotVersion == "" {
		t.Errorf("key = %q version = %q", gotKey, gotVersion)
	}
	if len(got) != 1 || got[0] != "claude-opus-5" {
		t.Errorf("models = %v", got)
	}
}

// A rejected key is worth telling apart from an unreachable provider: one is
// fixed in the panel, the other is not.
func TestListModelsReportsARejectedKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()

	_, err := ListModels(context.Background(), Runtime{Provider: ProviderOpenAI, Endpoint: srv.URL + "/v1"})
	if !errors.Is(err, ErrRefused) {
		t.Errorf("err = %v, want ErrRefused", err)
	}
}

func TestListModelsRefusesWhatItCannotAsk(t *testing.T) {
	if _, err := ListModels(context.Background(), Runtime{Provider: ProviderDisabled}); err == nil {
		t.Error("listed models for no provider")
	}
	if _, err := ListModels(context.Background(), Runtime{Provider: ProviderCustomOpenAI}); err == nil {
		t.Error("listed models with no endpoint and no default")
	}
	// An empty catalogue is not a usable list, so it is an error rather than a
	// picker with nothing in it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	if _, err := ListModels(context.Background(), Runtime{Provider: ProviderOllama, Endpoint: srv.URL + "/v1"}); err == nil {
		t.Error("an empty list was accepted")
	}
}
