package llm

import (
	"fmt"
	"os"
	"strings"

	"github.com/steveyegge/gastown/internal/config"
)

// NewClient constructs an LLM client from API config.
// It resolves api_key values that start with '$' as environment variables.
func NewClient(cfg *config.APIConfig) (Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("API config is nil")
	}

	apiType := strings.ToLower(strings.TrimSpace(cfg.APIType))
	if apiType == "" {
		return nil, fmt.Errorf("api_type is required")
	}

	apiKey, err := resolveAPIKey(cfg.APIKey)
	if err != nil {
		return nil, err
	}

	switch apiType {
	case "openai", "openai-compatible":
		if strings.TrimSpace(cfg.BaseURL) == "" {
			return nil, fmt.Errorf("base_url is required for api_type=%q", apiType)
		}
		return NewOpenAIClient(cfg, apiKey)

	case "anthropic":
		// base_url is optional; NewAnthropicClient defaults to https://api.anthropic.com
		return NewAnthropicClient(cfg, apiKey)

	default:
		return nil, fmt.Errorf("unsupported api_type: %q", cfg.APIType)
	}
}

func resolveAPIKey(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", nil
	}
	if strings.HasPrefix(s, "$") {
		name := strings.TrimPrefix(s, "$")
		if name == "" {
			return "", fmt.Errorf("invalid api_key: %q", raw)
		}
		return os.Getenv(name), nil
	}
	return s, nil
}