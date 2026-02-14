// Package agentloop provides a Go-native agent execution loop for API-mode agents.
// It implements the think-act-observe cycle, calling a remote LLM and executing
// tools locally in the git worktree context. This replaces tmux sessions for
// agents configured with provider_type="api".
package agentloop

import (
	"encoding/json"

	"github.com/steveyegge/gastown/internal/llm"
)

// GTTools returns the tool definitions for Gastown operations.
// These are exposed to API-mode agents as function-calling tools
// and to MCP-mode agents as MCP tools.
func GTTools() []llm.ToolDef {
	return []llm.ToolDef{
		{
			Name:        "gt_prime",
			Description: "Read current work assignment and context. Call this first when starting work.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{},"required":[]}`),
		},
		{
			Name:        "gt_done",
			Description: "Mark current task as complete. Commits work and signals the witness.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"message": {
						"type": "string",
						"description": "Completion summary describing what was done"
					}
				},
				"required": ["message"]
			}`),
		},
		{
			Name:        "bd_show",
			Description: "Show details of a beads issue including status, description, dependencies, and comments.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"issue_id": {
						"type": "string",
						"description": "The issue identifier (e.g., 'gt-abc123')"
					}
				},
				"required": ["issue_id"]
			}`),
		},
		{
			Name:        "bd_list",
			Description: "List beads issues with optional filters for status, label, or assignee.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"status": {
						"type": "string",
						"description": "Filter by status: open, in-progress, closed"
					},
					"label": {
						"type": "string",
						"description": "Filter by label"
					}
				},
				"required": []
			}`),
		},
		{
			Name:        "bd_update",
			Description: "Update a beads issue status, priority, or other fields.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"issue_id": {
						"type": "string",
						"description": "The issue identifier"
					},
					"status": {
						"type": "string",
						"description": "New status: open, in-progress, closed"
					},
					"comment": {
						"type": "string",
						"description": "Optional comment to add"
					}
				},
				"required": ["issue_id"]
			}`),
		},
		{
			Name:        "git_diff",
			Description: "Show git diff of current changes in the working directory.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"staged": {
						"type": "boolean",
						"description": "If true, show staged changes only"
					},
					"path": {
						"type": "string",
						"description": "Optional path to restrict diff to"
					}
				},
				"required": []
			}`),
		},
		{
			Name:        "git_status",
			Description: "Show git status of the working directory.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{},"required":[]}`),
		},
		{
			Name:        "git_commit",
			Description: "Stage all changes and commit with a message.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"message": {
						"type": "string",
						"description": "Commit message"
					},
					"paths": {
						"type": "array",
						"items": {"type": "string"},
						"description": "Optional specific paths to stage (default: all)"
					}
				},
				"required": ["message"]
			}`),
		},
		{
			Name:        "file_read",
			Description: "Read file contents. Returns the file content with line numbers.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"path": {
						"type": "string",
						"description": "File path relative to the working directory"
					},
					"start_line": {
						"type": "integer",
						"description": "Optional 1-based start line"
					},
					"end_line": {
						"type": "integer",
						"description": "Optional 1-based end line"
					}
				},
				"required": ["path"]
			}`),
		},
		{
			Name:        "file_write",
			Description: "Write content to a file. Creates parent directories if needed.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"path": {
						"type": "string",
						"description": "File path relative to the working directory"
					},
					"content": {
						"type": "string",
						"description": "Content to write"
					}
				},
				"required": ["path", "content"]
			}`),
		},
		{
			Name:        "file_edit",
			Description: "Apply a search-and-replace edit to a file. Finds the first occurrence of search text and replaces it.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"path": {
						"type": "string",
						"description": "File path relative to the working directory"
					},
					"search": {
						"type": "string",
						"description": "Text to find (exact match)"
					},
					"replace": {
						"type": "string",
						"description": "Replacement text"
					}
				},
				"required": ["path", "search", "replace"]
			}`),
		},
		{
			Name:        "file_list",
			Description: "List files and directories in a path. Like 'ls' or 'find'.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"path": {
						"type": "string",
						"description": "Directory path to list (default: working directory root)"
					},
					"recursive": {
						"type": "boolean",
						"description": "If true, list recursively"
					},
					"pattern": {
						"type": "string",
						"description": "Optional glob pattern to filter results"
					}
				},
				"required": []
			}`),
		},
		{
			Name:        "file_search",
			Description: "Search for text content across files using grep-like matching.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"pattern": {
						"type": "string",
						"description": "Search pattern (regex supported)"
					},
					"path": {
						"type": "string",
						"description": "Optional path to restrict search to"
					},
					"include": {
						"type": "string",
						"description": "Optional file glob to include (e.g., '*.go')"
					}
				},
				"required": ["pattern"]
			}`),
		},
		{
			Name:        "shell_exec",
			Description: "Execute a shell command in the working directory. Use sparingly and prefer specific tools when available.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"command": {
						"type": "string",
						"description": "Shell command to execute"
					},
					"timeout_seconds": {
						"type": "integer",
						"description": "Maximum execution time in seconds (default: 120)"
					}
				},
				"required": ["command"]
			}`),
		},
		{
			Name:        "gt_mail_send",
			Description: "Send a message to another agent or broadcast channel.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"to": {
						"type": "string",
						"description": "Recipient address (e.g., 'rig/witness' or 'rig/polecats/Name')"
					},
					"subject": {
						"type": "string",
						"description": "Message subject"
					},
					"body": {
						"type": "string",
						"description": "Message body"
					}
				},
				"required": ["to", "subject"]
			}`),
		},
		{
			Name:        "gt_mail_read",
			Description: "Read messages from the agent's mailbox.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"count": {
						"type": "integer",
						"description": "Maximum number of messages to return (default: 10)"
					},
					"unread_only": {
						"type": "boolean",
						"description": "If true, only return unread messages"
					}
				},
				"required": []
			}`),
		},
	}
}

// ToolNames returns the names of all available GT tools.
func ToolNames() []string {
	tools := GTTools()
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	return names
}

// FilterTools returns only the tools whose names are in the allowed list.
// If allowed is nil or empty, all tools are returned.
func FilterTools(allowed []string) []llm.ToolDef {
	if len(allowed) == 0 {
		return GTTools()
	}

	allowMap := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		allowMap[name] = true
	}

	var filtered []llm.ToolDef
	for _, t := range GTTools() {
		if allowMap[t.Name] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}
