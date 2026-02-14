// Package llm provides a unified interface for calling language models.
// It supports OpenAI-compatible APIs (Ollama, vLLM, OpenAI, Azure) and
// Anthropic's native API format.
//
// This package decouples Gas Town's agent execution from specific LLM providers,
// enabling agents to run against any LLM endpoint (local, LAN, or cloud).
package llm

import (
	"context"
	"encoding/json"
)

// Client is the interface for calling language models.
// Implementations handle wire protocol differences between providers.
type Client interface {
	// Chat sends a conversation and returns the model's complete response.
	Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)

	// Stream sends a conversation and returns a streaming response channel.
	// Each chunk contains either text content or a tool call.
	// The channel is closed when the response is complete.
	Stream(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error)

	// ModelInfo returns information about the connected model.
	ModelInfo() *ModelInfo

	// Ping checks if the API endpoint is reachable.
	Ping(ctx context.Context) error

	// Close releases any resources (HTTP connections, etc.).
	Close() error
}

// ChatRequest is the input to a Chat/Stream call.
type ChatRequest struct {
	Messages    []Message  `json:"messages"`
	Tools       []ToolDef  `json:"tools,omitempty"`
	MaxTokens   int        `json:"max_tokens,omitempty"`
	Temperature *float64   `json:"temperature,omitempty"`
	StopSeqs    []string   `json:"stop,omitempty"`
}

// Message represents a conversation message.
type Message struct {
	Role       string     `json:"role"`                  // "system", "user", "assistant", "tool"
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"` // for role="tool" responses
	Name       string     `json:"name,omitempty"`
}

// ToolDef defines a tool the model can call (function-calling).
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"` // JSON Schema
}

// ToolCall represents the model requesting a tool invocation.
type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"arguments"`
}

// ChatResponse is the model's complete response.
type ChatResponse struct {
	Content      string     `json:"content"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	Usage        *Usage     `json:"usage,omitempty"`
	FinishReason string     `json:"finish_reason"`
}

// StreamChunk is a single piece of a streaming response.
type StreamChunk struct {
	Type     ChunkType // TextChunk or ToolCallChunk
	Text     string    // for TextChunk
	ToolCall *ToolCall // for ToolCallChunk (may be partial)
	Done     bool      // true on final chunk
	Err      error     // non-nil on stream error
}

// ChunkType distinguishes text content from tool calls in streaming.
type ChunkType int

const (
	// TextChunk contains text content.
	TextChunk ChunkType = iota
	// ToolCallChunk contains a tool call (or partial tool call).
	ToolCallChunk
)

// Usage tracks token consumption.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ModelInfo describes the connected model.
type ModelInfo struct {
	ID             string `json:"id"`
	Provider       string `json:"provider"` // "ollama", "openai", "anthropic", "vllm"
	ContextWindow  int    `json:"context_window"`
	SupportsTools  bool   `json:"supports_tools"`
	SupportsVision bool   `json:"supports_vision"`
}