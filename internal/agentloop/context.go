package agentloop

import (
	"fmt"
	"strings"

	"github.com/steveyegge/gastown/internal/llm"
)

const (
	// DefaultContextWindow is used when model doesn't report context size.
	DefaultContextWindow = 128000
	// ContextReserve is the fraction of context window reserved for response.
	ContextReserve = 0.15
	// TokensPerChar is a rough estimate of tokens per character.
	// GPT-4/Claude average ~3.5 chars per token for code.
	TokensPerChar = 0.28
	// SummaryMaxTokens is the max tokens for a summarized message.
	SummaryMaxTokens = 500
)

// ContextManager tracks conversation size and manages context window limits.
// It handles truncation and summarization when the conversation gets too long.
type ContextManager struct {
	contextWindow int // model's total context window
	maxTokens     int // usable tokens (after response reserve)
	totalTokens   int // estimated current total
}

// NewContextManager creates a context manager for the given context window size.
func NewContextManager(contextWindow int) *ContextManager {
	if contextWindow <= 0 {
		contextWindow = DefaultContextWindow
	}
	maxTokens := int(float64(contextWindow) * (1 - ContextReserve))
	return &ContextManager{
		contextWindow: contextWindow,
		maxTokens:     maxTokens,
	}
}

// EstimateTokens returns a rough token count for a string.
func EstimateTokens(s string) int {
	return int(float64(len(s)) * TokensPerChar)
}

// EstimateMessageTokens returns estimated tokens for a message.
func EstimateMessageTokens(msg llm.Message) int {
	tokens := EstimateTokens(msg.Content)
	// Add overhead for role, tool calls, etc.
	tokens += 4 // role + formatting
	for _, tc := range msg.ToolCalls {
		tokens += EstimateTokens(tc.Name)
		tokens += EstimateTokens(string(tc.Args))
		tokens += 10 // overhead
	}
	if msg.ToolCallID != "" {
		tokens += 10
	}
	return tokens
}

// EstimateConversationTokens returns estimated tokens for a full conversation.
func EstimateConversationTokens(messages []llm.Message) int {
	total := 0
	for _, msg := range messages {
		total += EstimateMessageTokens(msg)
	}
	return total
}

// NeedsTruncation returns true if the conversation is likely to exceed the context window.
func (cm *ContextManager) NeedsTruncation(messages []llm.Message) bool {
	cm.totalTokens = EstimateConversationTokens(messages)
	return cm.totalTokens > cm.maxTokens
}

// Truncate reduces the conversation to fit within the context window.
// Strategy:
// 1. Always keep the system message (first message if role=system)
// 2. Always keep the last N messages (recent context)
// 3. Summarize or drop middle messages
// 4. Truncate long tool results
func (cm *ContextManager) Truncate(messages []llm.Message) []llm.Message {
	if !cm.NeedsTruncation(messages) {
		return messages
	}

	// Keep at least the system message and last 6 messages
	minKeepEnd := 6
	if len(messages) <= minKeepEnd+1 {
		// Can't truncate further — just trim tool results
		return cm.trimToolResults(messages)
	}

	var result []llm.Message

	// Keep system message
	startIdx := 0
	if len(messages) > 0 && messages[0].Role == "system" {
		result = append(result, messages[0])
		startIdx = 1
	}

	// Keep last N messages
	keepFrom := len(messages) - minKeepEnd
	if keepFrom < startIdx {
		keepFrom = startIdx
	}

	// Add summary of dropped messages
	if keepFrom > startIdx {
		droppedCount := keepFrom - startIdx
		summary := fmt.Sprintf("[%d earlier messages summarized]\n", droppedCount)
		summary += cm.summarizeMessages(messages[startIdx:keepFrom])
		result = append(result, llm.Message{
			Role:    "user",
			Content: summary,
		})
	}

	// Add recent messages
	result = append(result, messages[keepFrom:]...)

	// If still too large, trim tool results
	if cm.NeedsTruncation(result) {
		result = cm.trimToolResults(result)
	}

	return result
}

// trimToolResults truncates long tool result messages.
func (cm *ContextManager) trimToolResults(messages []llm.Message) []llm.Message {
	result := make([]llm.Message, len(messages))
	copy(result, messages)

	maxToolResult := 2000 // characters per tool result

	for i := range result {
		if result[i].Role == "tool" && len(result[i].Content) > maxToolResult {
			truncated := result[i].Content[:maxToolResult]
			result[i].Content = truncated + "\n... (truncated for context window)"
		}
	}

	return result
}

// summarizeMessages creates a brief summary of dropped messages.
func (cm *ContextManager) summarizeMessages(messages []llm.Message) string {
	var sb strings.Builder

	toolCalls := 0
	toolResults := 0
	assistantMsgs := 0
	userMsgs := 0

	for _, msg := range messages {
		switch msg.Role {
		case "assistant":
			assistantMsgs++
			toolCalls += len(msg.ToolCalls)
		case "tool":
			toolResults++
		case "user":
			userMsgs++
		}
	}

	if userMsgs > 0 {
		fmt.Fprintf(&sb, "- %d user messages\n", userMsgs)
	}
	if assistantMsgs > 0 {
		fmt.Fprintf(&sb, "- %d assistant responses\n", assistantMsgs)
	}
	if toolCalls > 0 {
		// Collect unique tool names
		toolNames := make(map[string]int)
		for _, msg := range messages {
			for _, tc := range msg.ToolCalls {
				toolNames[tc.Name]++
			}
		}
		fmt.Fprintf(&sb, "- %d tool calls: ", toolCalls)
		first := true
		for name, count := range toolNames {
			if !first {
				sb.WriteString(", ")
			}
			fmt.Fprintf(&sb, "%s(%d)", name, count)
			first = false
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// UsageReport returns a human-readable context usage report.
func (cm *ContextManager) UsageReport(messages []llm.Message) string {
	current := EstimateConversationTokens(messages)
	pct := float64(current) / float64(cm.maxTokens) * 100
	return fmt.Sprintf("Context: ~%d/%d tokens (%.0f%% of usable window, %d total)",
		current, cm.maxTokens, pct, cm.contextWindow)
}
