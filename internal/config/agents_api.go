package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// AgentsAPIFile matches docs/examples/agents-api.json.
// It is intentionally separate from TownSettings.Agents (which is for CLI/tmux runtimes).
type AgentsAPIFile struct {
	Version int                     `json:"version"`
	Agents  map[string]*AgentsAgent `json:"agents"`
}

type AgentsAgent struct {
	Name         string       `json:"name"`
	ProviderType ProviderType `json:"provider_type"`
	API          json.RawMessage `json:"api,omitempty"`
}

// APIRetryConfig matches the "retry" object inside agents-api.json.
type APIRetryConfig struct {
	MaxRetries       int `json:"max_retries,omitempty"`
	InitialBackoffMS int `json:"initial_backoff_ms,omitempty"`
	MaxBackoffMS     int `json:"max_backoff_ms,omitempty"`
}

// ResolvedAPIAgent is a normalized agent definition ready to run.
type ResolvedAPIAgent struct {
	ID           string
	Name         string
	ProviderType ProviderType
	API          *APIConfig
	Retry        *APIRetryConfig
}

func LoadAgentsAPIFile(path string) (*AgentsAPIFile, error) {
	if path == "" {
		return nil, fmt.Errorf("agents config path is empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading agents config: %w", err)
	}

	var f AgentsAPIFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing agents config JSON: %w", err)
	}

	if f.Version <= 0 {
		return nil, fmt.Errorf("invalid agents config version: %d", f.Version)
	}
	if f.Agents == nil || len(f.Agents) == 0 {
		return nil, fmt.Errorf("agents config has no agents")
	}

	return &f, nil
}

func (f *AgentsAPIFile) Resolve(id string) (*ResolvedAPIAgent, error) {
	if f == nil {
		return nil, fmt.Errorf("agents config is nil")
	}
	if id == "" {
		return nil, fmt.Errorf("agent id is empty")
	}
	agent, ok := f.Agents[id]
	if !ok || agent == nil {
		return nil, fmt.Errorf("agent %q not found in agents config", id)
	}

	if string(agent.ProviderType) != "api" {
		return nil, fmt.Errorf("agent %q provider_type=%q is not supported (expected %q)", id, string(agent.ProviderType), "api")
	}

	apiCfg, retryCfg, err := parseAPIConfigAndRetry(agent.API)
	if err != nil {
		return nil, fmt.Errorf("agent %q api config: %w", id, err)
	}

	return &ResolvedAPIAgent{
		ID:           id,
		Name:         agent.Name,
		ProviderType: agent.ProviderType,
		API:          apiCfg,
		Retry:        retryCfg,
	}, nil
}

func parseAPIConfigAndRetry(raw json.RawMessage) (*APIConfig, *APIRetryConfig, error) {
	if len(raw) == 0 {
		return nil, nil, fmt.Errorf("missing api block")
	}

	// Decode to map so we can safely extract "retry" without requiring APIConfig changes.
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, nil, fmt.Errorf("parsing api object: %w", err)
	}

	var retry *APIRetryConfig
	if rv, ok := m["retry"]; ok && rv != nil {
		b, err := json.Marshal(rv)
		if err == nil {
			var r APIRetryConfig
			if err := json.Unmarshal(b, &r); err == nil {
				// Only keep non-zero retry config.
				if r.MaxRetries > 0 || r.InitialBackoffMS > 0 || r.MaxBackoffMS > 0 {
					retry = &r
				}
			}
		}
		delete(m, "retry")
	}

	b, err := json.Marshal(m)
	if err != nil {
		return nil, retry, fmt.Errorf("re-encoding api object: %w", err)
	}

	var api APIConfig
	if err := json.Unmarshal(b, &api); err != nil {
		return nil, retry, fmt.Errorf("parsing api config: %w", err)
	}

	if api.APIType == "" {
		return nil, retry, fmt.Errorf("api.api_type is required")
	}
	if api.Model == "" {
		return nil, retry, fmt.Errorf("api.model is required")
	}
	// base_url may be optional for anthropic (client defaults), but required for openai.
	// Validation is enforced in llm.NewClient.

	return &api, retry, nil
}