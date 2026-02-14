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

// OpenAIClient implements Client for OpenAI-compatible APIs.
// Works with Ollama, vLLM, OpenAI, Azure OpenAI, LiteLLM, and any
// endpoint that speaks the OpenAI Chat Completions format.
type OpenAIClient struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
	headers    map[string]string
	modelInfo  *ModelInfo
}

// NewOpenAIClient creates a client for OpenAI-compatible endpoints.
func NewOpenAIClient(cfg *config.APIConfig, apiKey string) (*OpenAIClient, error) {
	timeout := 300 * time.Second
	if cfg.TimeoutSeconds > 0 {
		timeout = time.Duration(cfg.TimeoutSeconds) * time.Second
	}

	return &OpenAIClient{
		baseURL: cfg.BaseURL,
		apiKey:  apiKey,
		model:   cfg.Model,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		headers: cfg.Headers,
		modelInfo: &ModelInfo{
			ID:             cfg.Model,
			Provider:       detectProvider(cfg.BaseURL),
			ContextWindow:  cfg.ContextWindow,
			SupportsTools:  cfg.SupportsTools,
			SupportsVision: cfg.SupportsVision,
		},
	}, nil
}

// Chat sends a chat completion request and returns the response.
func (c *OpenAIClient) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	// Build OpenAI request
	oaiReq := map[string]interface{}{
		"model":    c.model,
		"messages": convertMessages(req.Messages),
	}

	if req.MaxTokens > 0 {
		oaiReq["max_tokens"] = req.MaxTokens
	}
	if req.Temperature != nil {
		oaiReq["temperature"] = *req.Temperature
	}
	if len(req.StopSeqs) > 0 {
		oaiReq["stop"] = req.StopSeqs
	}
	if len(req.Tools) > 0 {
		oaiReq["tools"] = convertTools(req.Tools)
	}

	body, err := json.Marshal(oaiReq)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
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

	var oaiResp openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&oaiResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	if len(oaiResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	choice := oaiResp.Choices[0]
	result := &ChatResponse{
		Content:      choice.Message.Content,
		FinishReason: choice.FinishReason,
	}

	if oaiResp.Usage != nil {
		result.Usage = &Usage{
			PromptTokens:     oaiResp.Usage.PromptTokens,
			CompletionTokens: oaiResp.Usage.CompletionTokens,
			TotalTokens:      oaiResp.Usage.TotalTokens,
		}
	}

	// Convert tool calls
	for _, tc := range choice.Message.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, ToolCall{
			ID:   tc.ID,
			Name: tc.Function.Name,
			Args: json.RawMessage(tc.Function.Arguments),
		})
	}

	return result, nil
}

// Stream sends a streaming chat completion request.
func (c *OpenAIClient) Stream(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error) {
	// TODO: Implement SSE streaming for chat completions
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
func (c *OpenAIClient) ModelInfo() *ModelInfo {
	return c.modelInfo
}

// Ping checks if the API endpoint is reachable.
func (c *OpenAIClient) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/models", nil)
	if err != nil {
		return err
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("endpoint unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("endpoint returned status %d", resp.StatusCode)
	}
	return nil
}

// Close releases HTTP client resources.
func (c *OpenAIClient) Close() error {
	c.httpClient.CloseIdleConnections()
	return nil
}

// --- OpenAI wire format types ---

type openAIResponse struct {
	Choices []openAIChoice `json:"choices"`
	Usage   *openAIUsage   `json:"usage"`
}

type openAIChoice struct {
	Message      openAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type openAIMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []openAIToolCall  `json:"tool_calls,omitempty"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// --- Helpers ---

func convertMessages(msgs []Message) []map[string]interface{} {
	var result []map[string]interface{}
	for _, m := range msgs {
		msg := map[string]interface{}{
			"role":    m.Role,
			"content": m.Content,
		}
		if m.ToolCallID != "" {
			msg["tool_call_id"] = m.ToolCallID
		}
		if len(m.ToolCalls) > 0 {
			var tcs []map[string]interface{}
			for _, tc := range m.ToolCalls {
				tcs = append(tcs, map[string]interface{}{
					"id":   tc.ID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      tc.Name,
						"arguments": string(tc.Args),
					},
				})
			}
			msg["tool_calls"] = tcs
		}
		result = append(result, msg)
	}
	return result
}

func convertTools(tools []ToolDef) []map[string]interface{} {
	var result []map[string]interface{}
	for _, t := range tools {
		result = append(result, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  json.RawMessage(t.Parameters),
			},
		})
	}
	return result
}

func detectProvider(baseURL string) string {
	switch {
	case contains(baseURL, "ollama") || contains(baseURL, ":11434"):
		return "ollama"
	case contains(baseURL, "openai.com"):
		return "openai"
	case contains(baseURL, "anthropic.com"):
		return "anthropic"
	case contains(baseURL, ":8000"):
		return "vllm"
	default:
		return "openai-compatible"
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && searchSubstring(s, substr))
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}