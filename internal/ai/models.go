package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// maxListedModels bounds what a provider can make the caller hold. A large
// catalogue is a picker nobody can read, and the list is only an aid: any name
// can still be typed.
const maxListedModels = 200

// ListModels asks a provider which models it serves, so a model can be chosen
// from what actually exists rather than typed from memory. It never falls back
// to a guess: an empty list or an error means the caller should let the reader
// type a name instead.
//
// The request carries the API key, so a caller under a policy that forbids the
// provider must not make it. Allowed reports that, and the settings panel
// checks it before calling here.
func ListModels(ctx context.Context, rt Runtime) ([]string, error) {
	if rt.Provider == "" || rt.Provider == ProviderDisabled {
		return nil, fmt.Errorf("%w: no provider", ErrUnavailable)
	}
	endpoint := rt.Endpoint
	if endpoint == "" {
		endpoint = rt.Provider.DefaultEndpoint()
	}
	if endpoint == "" {
		return nil, fmt.Errorf("%w: no endpoint", ErrUnavailable)
	}
	endpoint = strings.TrimRight(endpoint, "/")

	// Anthropic's configured endpoint is the bare host, with the version in the
	// path; the OpenAI-compatible providers carry /v1 in the endpoint already.
	url := endpoint + "/models"
	if rt.Provider == ProviderAnthropic {
		url = endpoint + "/v1/models"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if rt.Provider == ProviderAnthropic {
		req.Header.Set("anthropic-version", "2023-06-01")
		if rt.APIKey != "" {
			req.Header.Set("x-api-key", rt.APIKey)
		}
	} else if rt.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+rt.APIKey)
	}

	timeout := rt.Timeout
	if timeout <= 0 {
		// Listing is a small request made while someone waits, so it gives up
		// far sooner than a completion would.
		timeout = 15 * time.Second
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("%w: the API key was rejected", ErrRefused)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%w: %s", ErrUnavailable, strings.TrimSpace(firstLine(string(data))))
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("%w: unrecognised model list", ErrUnavailable)
	}
	seen := make(map[string]bool, len(payload.Data))
	out := make([]string, 0, len(payload.Data))
	for _, model := range payload.Data {
		id := strings.TrimSpace(model.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
		if len(out) == maxListedModels {
			break
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: the provider listed no models", ErrUnavailable)
	}
	sort.Strings(out)
	return out, nil
}

// firstLine keeps an error message to something a status line can hold.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return s
}
