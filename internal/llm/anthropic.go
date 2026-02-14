package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/steveyegge/gastown/internal/config"
)

// AnthropicClient implements Client for Anthropic's Messages API.
// Supports Claude models via api.anthropic.com or compatible proxies.
type AnthropicClient struct {
	baseURL    string
	apiKey     string
	model      string
	maxTokens  int
	httpClient *http.Client
	headers    map[string]string
	modelInfo  *ModelInfo
}

const (
	anthropicAPIVersion    = "2023-06-01"
	anthropicDefaultMaxTok = 4096
)

// NewAnthropicClient creates a client for the Anthropic Messages API.
func NewAnthropicClient(cfg *config.APIConfig, apiKey string) (*AnthropicClient, error) {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}

	timeout := 300 * time.Second
	if cfg.TimeoutSeconds > 0 {
		timeout = time.Duration(cfg.TimeoutSeconds) * time.Second
	}

	maxTokens := cfg.MaxTokens
	if maxTokens == 0 {
		maxTokens = anthropicDefaultMaxTok
	}

	return &AnthropicClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   cfg.Model,
		maxTokens: maxTokens,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		headers: cfg.Headers,
		modelInfo: &ModelInfo{
			ID:             cfg.Model,
			Provider:       "anthropic",
			ContextWindow:  cfg.ContextWindow,
			SupportsTools:  cfg.SupportsTools,
			SupportsVision: cfg.SupportsVision,
		},
	}, nil
}

// Chat sends a messages request and returns the response.
func (c *AnthropicClient) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	// Build Anthropic messages request
	anthReq := map[string]interface{}{
		"model":      c.model,
		"max_tokens": c.maxTokens,
	}

	if req.MaxTokens > 0 {
		anthReq["max_tokens"] = req.MaxTokens
	}
	if req.Temperature != nil {
		anthReq["temperature"] = *req.Temperature
	}
	if len(req.StopSeqs) > 0 {
		anthReq["stop_sequences"] = req.StopSeqs
	}

	// Anthropic separates system messages from the messages array
	system, messages := splitSystemMessages(req.Messages)
	if system != "" {
		anthReq["system"] = system
	}
	anthReq["messages"] = convertAnthropicMessages(messages)

	if len(req.Tools) > 0 {
		anthReq["tools"] = convertAnthropicTools(req.Tools)
	}

	body, err := json.Marshal(anthReq)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)
	if c.apiKey != "" {
		httpReq.Header.Set("x-api-key", c.apiKey)
	}
	for k, v := range c.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(errBody))
	}

	var anthResp anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&anthResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	result := &ChatResponse{
		FinishReason: mapStopReason(anthResp.StopReason),
	}

	// Extract text and tool use from content blocks
	for _, block := range anthResp.Content {
		switch block.Type {
		case "text":
			if result.Content != "" {
				result.Content += "\n"
			}
			result.Content += block.Text
		case "tool_use":
			argsJSON, err := json.Marshal(block.Input)
			if err != nil {
				argsJSON = []byte("{}")
			}
			result.ToolCalls = append(result.ToolCalls, ToolCall{
				ID:   block.ID,
				Name: block.Name,
				Args: json.RawMessage(argsJSON),
			})
		}
	}

	if anthResp.Usage != nil {
		result.Usage = &Usage{
			PromptTokens:     anthResp.Usage.InputTokens,
			CompletionTokens: anthResp.Usage.OutputTokens,
			TotalTokens:      anthResp.Usage.InputTokens + anthResp.Usage.OutputTokens,
		}
	}

	return result, nil
}

// Stream sends a streaming messages request.
func (c *AnthropicClient) Stream(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error) {
	// TODO: Implement SSE streaming with Anthropic's server-sent events
	// For now, fall back to non-streaming and emit result as single chunk
	ch := make(chan StreamChunk, 1)

	go func() {
		defer close(ch)

		resp, err := c.Chat(ctx, req)
		if err != nil {
			ch <- StreamChunk{Err: err, Done: true}
			return
		}

		if resp.Content != "" {
			ch <- StreamChunk{Type: TextChunk, Text: resp.Content}
		}
		for _, tc := range resp.ToolCalls {
			tcCopy := tc
			ch <- StreamChunk{Type: ToolCallChunk, ToolCall: &tcCopy}
		}
		ch <- StreamChunk{Done: true}
	}()

	return ch, nil
}

// ModelInfo returns information about the connected model.
func (c *AnthropicClient) ModelInfo() *ModelInfo {
	return c.modelInfo
}

// Ping checks if the API endpoint is reachable.
func (c *AnthropicClient) Ping(ctx context.Context) error {
	// Anthropic doesn't have a /models endpoint, so we send a minimal request
	minReq := &ChatRequest{
		Messages:  []Message{{Role: "user", Content: "ping"}},
		MaxTokens: 1,
	}
	_, err := c.Chat(ctx, minReq)
	return err
}

// Close releases HTTP client resources.
func (c *AnthropicClient) Close() error {
	c.httpClient.CloseIdleConnections()
	return nil
}

// --- Anthropic wire format types ---

type anthropicResponse struct {
	ID         string             `json:"id"`
	Type       string             `json:"type"`
	Role       string             `json:"role"`
	Content    []anthropicContent `json:"content"`
	StopReason string             `json:"stop_reason"`
	Usage      *anthropicUsage    `json:"usage"`
}

type anthropicContent struct {
	Type  string      `json:"type"`            // "text" or "tool_use"
	Text  string      `json:"text,omitempty"`   // for type="text"
	ID    string      `json:"id,omitempty"`     // for type="tool_use"
	Name  string      `json:"name,omitempty"`   // for type="tool_use"
	Input interface{} `json:"input,omitempty"`  // for type="tool_use"
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// --- Helpers ---

// splitSystemMessages extracts system messages from the message list.
// Anthropic requires system messages to be passed as a top-level field,
// not in the messages array.
func splitSystemMessages(msgs []Message) (string, []Message) {
	var system string
	var rest []Message

	for _, m := range msgs {
		if m.Role == "system" {
			if system != "" {
				system += "\n\n"
			}
			system += m.Content
		} else {
			rest = append(rest, m)
		}
	}

	return system, rest
}

// convertAnthropicMessages converts our Message type to Anthropic's format.
func convertAnthropicMessages(msgs []Message) []map[string]interface{} {
	var result []map[string]interface{}
	for _, m := range msgs {
		msg := map[string]interface{}{
			"role": m.Role,
		}

		// Build content blocks
		if m.Role == "tool" {
			// Tool result — Anthropic uses role "user" with tool_result content
			msg["role"] = "user"
			msg["content"] = []map[string]interface{}{
				{
					"type":        "tool_result",
					"tool_use_id": m.ToolCallID,
					"content":     m.Content,
				},
			}
		} else if len(m.ToolCalls) > 0 {
			// Assistant message with tool calls
			var content []map[string]interface{}
			if m.Content != "" {
				content = append(content, map[string]interface{}{
					"type": "text",
					"text": m.Content,
				})
			}
			for _, tc := range m.ToolCalls {
				var input interface{}
				if err := json.Unmarshal(tc.Args, &input); err != nil {
					input = map[string]interface{}{}
				}
				content = append(content, map[string]interface{}{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Name,
					"input": input,
				})
			}
			msg["content"] = content
		} else {
			msg["content"] = m.Content
		}

		result = append(result, msg)
	}
	return result
}

// convertAnthropicTools converts our ToolDef type to Anthropic's tool format.
func convertAnthropicTools(tools []ToolDef) []map[string]interface{} {
	var result []map[string]interface{}
	for _, t := range tools {
		var schema interface{}
		if err := json.Unmarshal(t.Parameters, &schema); err != nil {
			schema = map[string]interface{}{}
		}
		result = append(result, map[string]interface{}{
			"name":         t.Name,
			"description":  t.Description,
			"input_schema": schema,
		})
	}
	return result
}

// mapStopReason maps Anthropic stop reasons to OpenAI-compatible finish reasons.
func mapStopReason(reason string) string {
	switch reason {
	case "end_turn":
		return "stop"
	case "max_tokens":
		return "length"
	case "stop_sequence":
		return "stop"
	case "tool_use":
		return "tool_calls"
	default:
		return reason
	}
}
