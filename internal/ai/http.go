package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Runtime is the resolved provider configuration. It mirrors the [ai] config
// section without importing it, so the ai package stays UI- and config-free.
type Runtime struct {
	Provider Provider
	Endpoint string
	Model    string
	APIKey   string
	// Timeout bounds a single request. Local models can be slow, so it is
	// generous; the caller's context still cancels sooner when the user does.
	Timeout time.Duration
}

// New builds the assistant a Runtime describes. An unknown or disabled provider
// yields the null assistant, and a request under a policy that forbids the
// provider is refused rather than silently sent elsewhere.
func New(rt Runtime) WritingAssistant {
	if rt.Provider == "" || rt.Provider == ProviderDisabled || rt.Model == "" {
		return Disabled{}
	}
	endpoint := rt.Endpoint
	if endpoint == "" {
		endpoint = rt.Provider.DefaultEndpoint()
	}
	if endpoint == "" {
		return Disabled{}
	}
	timeout := rt.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	if rt.Provider == ProviderAnthropic {
		return &Anthropic{endpoint: strings.TrimRight(endpoint, "/"), model: rt.Model, key: rt.APIKey, http: client}
	}
	return &OpenAICompatible{provider: rt.Provider, endpoint: strings.TrimRight(endpoint, "/"), model: rt.Model, key: rt.APIKey, http: client}
}

// OpenAICompatible serves Ollama, LM Studio, OpenAI, DeepSeek and any other
// /chat/completions endpoint.
type OpenAICompatible struct {
	provider Provider
	endpoint string
	model    string
	key      string
	http     *http.Client
}

func (c *OpenAICompatible) Review(ctx context.Context, req ReviewRequest) (ReviewResult, error) {
	out, err := c.chat(ctx, reviewSystem, reviewUser(req))
	if err != nil {
		return ReviewResult{}, err
	}
	changes, err := parseChanges(out, req.Text)
	if err != nil {
		return ReviewResult{}, err
	}
	return ReviewResult{Changes: changes}, nil
}

func (c *OpenAICompatible) Rewrite(ctx context.Context, req RewriteRequest) (RewriteResult, error) {
	out, err := c.chat(ctx, rewriteSystem(req.Instruction), req.Text)
	if err != nil {
		return RewriteResult{}, err
	}
	return RewriteResult{Text: cleanRewrite(out)}, nil
}

func (c *OpenAICompatible) chat(ctx context.Context, system, user string) (string, error) {
	body := map[string]any{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		// A low temperature keeps corrections faithful; providers that do not
		// know the field ignore it.
		"temperature": 0.2,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.key)
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%w: provider returned %s", ErrUnavailable, resp.Status)
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", fmt.Errorf("%w: malformed response", ErrUnavailable)
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("AI provider returned no choices")
	}
	return parsed.Choices[0].Message.Content, nil
}

// Anthropic speaks the Messages API, which differs enough to need its own
// request and response shape.
type Anthropic struct {
	endpoint string
	model    string
	key      string
	http     *http.Client
}

func (c *Anthropic) Review(ctx context.Context, req ReviewRequest) (ReviewResult, error) {
	out, err := c.chat(ctx, reviewSystem, reviewUser(req))
	if err != nil {
		return ReviewResult{}, err
	}
	changes, err := parseChanges(out, req.Text)
	if err != nil {
		return ReviewResult{}, err
	}
	return ReviewResult{Changes: changes}, nil
}

func (c *Anthropic) Rewrite(ctx context.Context, req RewriteRequest) (RewriteResult, error) {
	out, err := c.chat(ctx, rewriteSystem(req.Instruction), req.Text)
	if err != nil {
		return RewriteResult{}, err
	}
	return RewriteResult{Text: cleanRewrite(out)}, nil
}

func (c *Anthropic) chat(ctx context.Context, system, user string) (string, error) {
	body := map[string]any{
		"model":      c.model,
		"max_tokens": 1024,
		"system":     system,
		"messages":   []map[string]string{{"role": "user", "content": user}},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	if c.key != "" {
		httpReq.Header.Set("x-api-key", c.key)
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%w: provider returned %s", ErrUnavailable, resp.Status)
	}
	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", fmt.Errorf("%w: malformed response", ErrUnavailable)
	}
	var b strings.Builder
	for _, part := range parsed.Content {
		if part.Type == "text" {
			b.WriteString(part.Text)
		}
	}
	if b.Len() == 0 {
		return "", errors.New("AI provider returned no text")
	}
	return b.String(), nil
}
