package llm

import (
	"fmt"
	"os"
	"strings"

	"github.com/steveyegge/gastown/internal/config"
)

// NewClient creates an LLM client from the API configuration.
// It dispatches to the correct implementation based on api_type.
func NewClient(cfg *config.APIConfig) (Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("APIConfig is nil")
	}

	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("base_url is required")
	}

	if cfg.Model == "" {
		return nil, fmt.Errorf("model is required")
	}

	// Resolve API key from env var if prefixed with $
	apiKey := cfg.APIKey
	if strings.HasPrefix(apiKey, "$") {
		apiKey = os.Getenv(strings.TrimPrefix(apiKey, "$"))
	}

	apiType := cfg.APIType
	if apiType == "" {
		apiType = "openai" // default
	}

	switch apiType {
	case "openai":
		return NewOpenAIClient(cfg, apiKey)
	case "anthropic":
		return NewAnthropicClient(cfg, apiKey)
	default:
		return nil, fmt.Errorf("unsupported api_type: %q (supported: openai, anthropic)", apiType)
	}
}