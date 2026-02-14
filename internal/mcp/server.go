// Package mcp provides an MCP (Model Context Protocol) server that exposes
// Gastown tools to remote agents over the network. This enables ProviderMCP
// agents running on remote machines (e.g., GPU servers) to access local
// git repos, beads, and GT commands.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/steveyegge/gastown/internal/agentloop"
)

const (
	// DefaultPort is the default MCP server port.
	DefaultPort = 9500
	// DefaultBindAddress binds to localhost only for security.
	DefaultBindAddress = "127.0.0.1"
	// DefaultShutdownTimeout is the graceful shutdown timeout.
	DefaultShutdownTimeout = 10 * time.Second
)

// ToolHandler is a function that handles an MCP tool call.
type ToolHandler func(ctx context.Context, args json.RawMessage) (string, error)

// ToolRegistration describes a registered tool.
type ToolRegistration struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Handler     ToolHandler     `json:"-"`
}

// Server exposes Gastown tools via MCP protocol.
// Remote agents connect to this server to access git repos, beads,
// and GT commands on the local machine.
type Server struct {
	addr      string
	authToken string
	executor  *agentloop.Executor

	mu    sync.RWMutex
	tools map[string]*ToolRegistration

	httpServer *http.Server
	started    bool
}

// NewServer creates an MCP server.
func NewServer(addr string, executor *agentloop.Executor, authToken string) *Server {
	if addr == "" {
		addr = fmt.Sprintf("%s:%d", DefaultBindAddress, DefaultPort)
	}

	s := &Server{
		addr:      addr,
		authToken: authToken,
		executor:  executor,
		tools:     make(map[string]*ToolRegistration),
	}

	return s
}

// RegisterTool adds a tool to the MCP server.
func (s *Server) RegisterTool(name, description string, schema json.RawMessage, handler ToolHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tools[name] = &ToolRegistration{
		Name:        name,
		Description: description,
		InputSchema: schema,
		Handler:     handler,
	}
}

// RegisterGTTools registers all standard GT tools from the agentloop package.
func (s *Server) RegisterGTTools() {
	gtTools := agentloop.GTTools()
	for _, tool := range gtTools {
		toolName := tool.Name
		s.RegisterTool(tool.Name, tool.Description, tool.Parameters, func(ctx context.Context, args json.RawMessage) (string, error) {
			return s.executor.Execute(ctx, llmToolCall(toolName, args))
		})
	}
}

// Start begins listening for MCP connections.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// MCP protocol endpoints
	mux.HandleFunc("/mcp/tools/list", s.authMiddleware(s.handleToolsList))
	mux.HandleFunc("/mcp/tools/call", s.authMiddleware(s.handleToolsCall))
	mux.HandleFunc("/mcp/health", s.handleHealth)

	// SSE endpoint for streaming
	mux.HandleFunc("/mcp/sse", s.authMiddleware(s.handleSSE))

	s.httpServer = &http.Server{
		Addr:         s.addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 300 * time.Second, // Long timeout for tool execution
		IdleTimeout:  120 * time.Second,
	}

	s.started = true
	log.Printf("[mcp] Server listening on %s", s.addr)

	go func() {
		<-ctx.Done()
		s.Stop()
	}()

	if err := s.httpServer.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("MCP server error: %w", err)
	}
	return nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() error {
	if !s.started || s.httpServer == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), DefaultShutdownTimeout)
	defer cancel()

	log.Printf("[mcp] Shutting down server")
	s.started = false
	return s.httpServer.Shutdown(ctx)
}

// Addr returns the server's listen address.
func (s *Server) Addr() string {
	return s.addr
}

// --- HTTP handlers ---

func (s *Server) handleToolsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var tools []map[string]interface{}
	for _, t := range s.tools {
		tools = append(tools, map[string]interface{}{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": json.RawMessage(t.InputSchema),
		})
	}

	resp := map[string]interface{}{
		"tools": tools,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

type toolCallRequest struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type toolCallResponse struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (s *Server) handleToolsCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req toolCallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	tool, ok := s.tools[req.Name]
	s.mu.RUnlock()

	if !ok {
		resp := toolCallResponse{
			Content: []toolContent{{Type: "text", Text: fmt.Sprintf("Unknown tool: %s", req.Name)}},
			IsError: true,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	result, err := tool.Handler(r.Context(), req.Arguments)

	var resp toolCallResponse
	if err != nil {
		resp = toolCallResponse{
			Content: []toolContent{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}},
			IsError: true,
		}
	} else {
		resp = toolCallResponse{
			Content: []toolContent{{Type: "text", Text: result}},
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	toolCount := len(s.tools)
	s.mu.RUnlock()

	resp := map[string]interface{}{
		"status":     "ok",
		"tools":      toolCount,
		"started":    s.started,
		"work_dir":   s.executor.WorkDir(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Send initial connection event
	fmt.Fprintf(w, "event: connected\ndata: {\"status\":\"ok\"}\n\n")
	flusher.Flush()

	// Keep connection alive with heartbeats until client disconnects
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, "event: heartbeat\ndata: {\"time\":\"%s\"}\n\n", time.Now().UTC().Format(time.RFC3339))
			flusher.Flush()
		}
	}
}

// --- Middleware ---

func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.authToken == "" {
			// No auth configured — allow all (development mode)
			next(w, r)
			return
		}

		auth := r.Header.Get("Authorization")
		expected := "Bearer " + s.authToken
		if auth != expected {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}

// --- Helpers ---

// llmToolCall creates a minimal llm.ToolCall for the executor.
func llmToolCall(name string, args json.RawMessage) llm.ToolCall {
	return llm.ToolCall{
		ID:   "mcp-" + name,
		Name: name,
		Args: args,
	}
}
