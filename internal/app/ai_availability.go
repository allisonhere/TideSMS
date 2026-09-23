package app

import (
	"net/http"
	"strings"
	"time"

	"github.com/allisonhere/tidesms/internal/ai"
	tea "github.com/charmbracelet/bubbletea"
)

type localProviderStatus struct {
	endpoint  string
	available bool
}
type localProvidersMsg map[ai.Provider]localProviderStatus

func (m *Model) localEndpoint(provider ai.Provider) string {
	if string(provider) == m.cfg.AI.Provider {
		if m.cfg.AI.Endpoint != "" {
			return m.cfg.AI.Endpoint
		}
		if provider == ai.ProviderOllama && m.cfg.AI.OllamaEndpoint != "" {
			return m.cfg.AI.OllamaEndpoint
		}
		return provider.DefaultEndpoint()
	}
	if profile, ok := m.cfg.AI.Providers[string(provider)]; ok {
		if profile.Endpoint != "" {
			return profile.Endpoint
		}
		return provider.DefaultEndpoint()
	}
	if provider == ai.ProviderOllama && m.cfg.AI.OllamaEndpoint != "" {
		return m.cfg.AI.OllamaEndpoint
	}
	return provider.DefaultEndpoint()
}

func (m *Model) localProviderUnavailable(provider ai.Provider) bool {
	status, ok := m.localProviderStatus[provider]
	return ok && status.endpoint == m.localEndpoint(provider) && !status.available
}

func (m *Model) providerLabel(provider ai.Provider) string {
	label := string(provider)
	if m.localProviderUnavailable(provider) {
		label += " (unavailable)"
	}
	return label
}

// Probe only local providers; opening settings never sends cloud credentials.
func (m *Model) probeLocalProviders() tea.Cmd {
	endpoints := map[ai.Provider]string{}
	for _, p := range []ai.Provider{ai.ProviderOllama, ai.ProviderLMStudio} {
		endpoints[p] = m.localEndpoint(p)
	}
	ctx := m.ctx
	return func() tea.Msg {
		result := localProvidersMsg{}
		client := &http.Client{Timeout: time.Second}
		for p, endpoint := range endpoints {
			available := false
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint, "/")+"/models", nil)
			if err == nil {
				resp, err := client.Do(req)
				if err == nil {
					available = resp.StatusCode >= 200 && resp.StatusCode < 300
					_ = resp.Body.Close()
				}
			}
			result[p] = localProviderStatus{endpoint: endpoint, available: available}
		}
		return result
	}
}
