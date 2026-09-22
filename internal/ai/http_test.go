package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseChangesFindsOffsetsAndDropsBadEntries(t *testing.T) {
	text := "hey their, tommorow is fine"
	raw := `{"changes":[
		{"original":"their","suggested":"there"},
		{"original":"tommorow","suggested":"tomorrow"},
		{"original":"nonexistent","suggested":"x"},
		{"original":"hey","suggested":"Hey,"}
	]}`
	changes, err := parseChanges(raw, text)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 3 {
		t.Fatalf("expected 3 usable changes, got %d: %+v", len(changes), changes)
	}
	if changes[0].Original != "hey" || changes[0].Suggested != "Hey," {
		t.Fatalf("changes should be sorted by offset: %+v", changes)
	}
	got := Apply(text, changes, []bool{true, true, true})
	if got != "Hey, there, tomorrow is fine" {
		t.Fatalf("apply = %q", got)
	}
	// Accepting only the first change leaves the rest untouched.
	got = Apply(text, changes, []bool{true, false, false})
	if got != "Hey, their, tommorow is fine" {
		t.Fatalf("partial apply = %q", got)
	}
}

func TestParseChangesRejectsMalformedJSON(t *testing.T) {
	if _, err := parseChanges("not json", "text"); err == nil {
		t.Fatal("malformed JSON should error so the draft is left alone")
	}
	if changes, err := parseChanges(`{"changes":[]}`, "text"); err != nil || len(changes) != 0 {
		t.Fatalf("empty changes = %+v err=%v", changes, err)
	}
}

func TestParseChangesStripsCodeFence(t *testing.T) {
	raw := "```json\n{\"changes\":[{\"original\":\"teh\",\"suggested\":\"the\"}]}\n```"
	changes, err := parseChanges(raw, "teh cat")
	if err != nil || len(changes) != 1 || changes[0].Suggested != "the" {
		t.Fatalf("fenced JSON not handled: %+v err=%v", changes, err)
	}
}

func openAIServer(t *testing.T, content string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("auth = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": content}}},
		})
	}))
}

func TestOpenAICompatibleReview(t *testing.T) {
	content := `{"changes":[{"original":"teh","suggested":"the"}]}`
	srv := openAIServer(t, content)
	defer srv.Close()
	c := New(Runtime{Provider: ProviderOpenAI, Endpoint: srv.URL, Model: "gpt-test", APIKey: "secret"})
	res, err := c.Review(context.Background(), ReviewRequest{Text: "teh cat", Policy: PolicyCloud})
	if err != nil || len(res.Changes) != 1 || res.Changes[0].Start != 0 || res.Changes[0].End != 3 {
		t.Fatalf("review = %+v err=%v", res, err)
	}
}

func TestOpenAICompatibleRewriteCleansFurniture(t *testing.T) {
	srv := openAIServer(t, `"Yep, see you at seven."`)
	defer srv.Close()
	c := New(Runtime{Provider: ProviderOpenAI, Endpoint: srv.URL, Model: "m", APIKey: "secret"})
	res, err := c.Rewrite(context.Background(), RewriteRequest{Text: "yes see u at 7", Instruction: "professional"})
	if err != nil || res.Text != "Yep, see you at seven." {
		t.Fatalf("rewrite = %q err=%v", res.Text, err)
	}
}

func TestProviderErrorsBecomeUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := New(Runtime{Provider: ProviderOllama, Endpoint: srv.URL, Model: "m"})
	if _, err := c.Review(context.Background(), ReviewRequest{Text: "x"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestAnthropicReview(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "akey" {
			t.Errorf("x-api-key = %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got == "" {
			t.Error("missing anthropic-version")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]string{{"type": "text", "text": `{"changes":[{"original":"teh","suggested":"the"}]}`}},
		})
	}))
	defer srv.Close()
	c := New(Runtime{Provider: ProviderAnthropic, Endpoint: srv.URL, Model: "claude", APIKey: "akey"})
	res, err := c.Review(context.Background(), ReviewRequest{Text: "teh cat"})
	if err != nil || len(res.Changes) != 1 {
		t.Fatalf("review = %+v err=%v", res, err)
	}
}

func TestLocalModelCancellation(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		// Release the handler even if the client's disconnect is not observed,
		// so the test never depends on server-side cancellation timing.
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()
	c := New(Runtime{Provider: ProviderOllama, Endpoint: srv.URL, Model: "m"})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := c.Review(ctx, ReviewRequest{Text: "x"})
		done <- err
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancel should surface an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request did not cancel")
	}
}

func TestNewFactory(t *testing.T) {
	if _, ok := New(Runtime{Provider: ProviderDisabled, Model: "m"}).(Disabled); !ok {
		t.Fatal("disabled provider should yield the null assistant")
	}
	if _, ok := New(Runtime{Provider: ProviderOllama, Model: ""}).(Disabled); !ok {
		t.Fatal("a missing model should yield the null assistant")
	}
	if _, ok := New(Runtime{Provider: ProviderDeepSeek, Model: "deepseek-chat"}).(*OpenAICompatible); !ok {
		t.Fatal("deepseek should use the OpenAI-compatible transport")
	}
	if _, ok := New(Runtime{Provider: ProviderAnthropic, Model: "claude"}).(*Anthropic); !ok {
		t.Fatal("anthropic should use its own transport")
	}
}
