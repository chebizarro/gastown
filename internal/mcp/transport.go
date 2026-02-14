package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// TransportType identifies the MCP transport mechanism.
type TransportType string

const (
	// TransportSSE uses Server-Sent Events over HTTP (default for network).
	TransportSSE TransportType = "sse"
	// TransportStdio uses stdin/stdout (for local processes).
	TransportStdio TransportType = "stdio"
	// TransportWebSocket uses WebSocket (bidirectional).
	TransportWebSocket TransportType = "ws"
)

// Transport abstracts the MCP communication mechanism.
// Different transports are used depending on the deployment:
// - SSE for network-accessible servers (LAN, cloud)
// - stdio for locally spawned agent processes
// - WebSocket for bidirectional real-time communication
type Transport interface {
	// Connect establishes the transport connection.
	Connect(ctx context.Context) error

	// ListTools returns the available tools from the remote server.
	ListTools(ctx context.Context) ([]ToolRegistration, error)

	// CallTool invokes a tool on the remote server and returns the result.
	CallTool(ctx context.Context, name string, args json.RawMessage) (string, error)

	// Close tears down the transport connection.
	Close() error
}

// SSETransport implements Transport over HTTP with SSE for streaming.
type SSETransport struct {
	baseURL    string
	authToken  string
	httpClient *http.Client
}

// NewSSETransport creates an SSE transport client.
func NewSSETransport(baseURL, authToken string) *SSETransport {
	// Normalize URL
	baseURL = strings.TrimRight(baseURL, "/")

	return &SSETransport{
		baseURL:   baseURL,
		authToken: authToken,
		httpClient: &http.Client{
			Timeout: 300 * time.Second,
		},
	}
}

// Connect checks that the MCP server is reachable.
func (t *SSETransport) Connect(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", t.baseURL+"/mcp/health", nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("connecting to MCP server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("MCP server returned %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// ListTools retrieves available tools from the MCP server.
func (t *SSETransport) ListTools(ctx context.Context) ([]ToolRegistration, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", t.baseURL+"/mcp/tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	t.setAuth(req)

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing tools: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list tools failed %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Tools []ToolRegistration `json:"tools"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding tools: %w", err)
	}

	return result.Tools, nil
}

// CallTool invokes a tool on the MCP server.
func (t *SSETransport) CallTool(ctx context.Context, name string, args json.RawMessage) (string, error) {
	body, err := json.Marshal(toolCallRequest{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		return "", fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", t.baseURL+"/mcp/tools/call", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	t.setAuth(req)

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("calling tool: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("tool call failed %d: %s", resp.StatusCode, string(respBody))
	}

	var result toolCallResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}

	if result.IsError {
		if len(result.Content) > 0 {
			return "", fmt.Errorf("tool error: %s", result.Content[0].Text)
		}
		return "", fmt.Errorf("tool error (no details)")
	}

	if len(result.Content) > 0 {
		return result.Content[0].Text, nil
	}
	return "", nil
}

// Close releases HTTP client resources.
func (t *SSETransport) Close() error {
	t.httpClient.CloseIdleConnections()
	return nil
}

func (t *SSETransport) setAuth(req *http.Request) {
	if t.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+t.authToken)
	}
}

// NewTransport creates a transport based on the transport type.
func NewTransport(transportType TransportType, serverURL, authToken string) (Transport, error) {
	switch transportType {
	case TransportSSE, "":
		return NewSSETransport(serverURL, authToken), nil
	case TransportStdio:
		return nil, fmt.Errorf("stdio transport not yet implemented")
	case TransportWebSocket:
		return nil, fmt.Errorf("websocket transport not yet implemented")
	default:
		return nil, fmt.Errorf("unknown transport type: %s", transportType)
	}
}
