package agentloop

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/steveyegge/gastown/internal/llm"
)

const (
	// DefaultMaxIterations is the maximum think-act-observe cycles per task.
	DefaultMaxIterations = 50
	// DefaultMaxTokensPerTask limits total token usage per task.
	DefaultMaxTokensPerTask = 200000
	// DefaultIdleTimeout is how long to wait for work before sleeping.
	DefaultIdleTimeout = 5 * time.Minute
	// DefaultToolTimeout is the max time for a single tool execution.
	DefaultToolTimeout = 120 * time.Second
)

// LoopState represents the current state of the agent loop.
type LoopState string

const (
	// StateIdle means the loop is waiting for work.
	StateIdle LoopState = "idle"
	// StateWorking means the loop is processing a task.
	StateWorking LoopState = "working"
	// StateStopped means the loop has been stopped.
	StateStopped LoopState = "stopped"
	// StateError means the loop encountered a fatal error.
	StateError LoopState = "error"
)

// AgentLoopConfig controls loop behavior.
type AgentLoopConfig struct {
	// SystemPrompt is the system message prepended to every conversation.
	SystemPrompt string

	// MaxIterations limits the think-act-observe cycles per task.
	// Prevents infinite loops. Default: 50.
	MaxIterations int

	// MaxTokensPerTask limits total token usage per task.
	// Prevents runaway costs. Default: 200000.
	MaxTokensPerTask int

	// IdleTimeout is how long to wait for work before the loop sleeps.
	// Default: 5 minutes.
	IdleTimeout time.Duration

	// ToolTimeout is the maximum time for a single tool execution.
	// Default: 120 seconds.
	ToolTimeout time.Duration

	// Role is the agent's role (polecat, witness, refinery, etc.)
	Role string

	// RigName is the rig this agent is working on.
	RigName string

	// Actor is the agent's full address (e.g., "rig/polecats/Toast").
	Actor string

	// OnHeartbeat is called periodically during task execution.
	// Used to publish Nostr lifecycle events.
	OnHeartbeat func(state LoopState, iteration int, totalTokens int)

	// OnTaskComplete is called when a task finishes.
	OnTaskComplete func(task string, iterations int, totalTokens int, err error)
}

// LoopStatus contains the current status of the agent loop.
type LoopStatus struct {
	State       LoopState `json:"state"`
	CurrentTask string    `json:"current_task,omitempty"`
	Iteration   int       `json:"iteration"`
	TotalTokens int       `json:"total_tokens"`
	StartedAt   time.Time `json:"started_at"`
	LastActive  time.Time `json:"last_active"`
	Error       string    `json:"error,omitempty"`
}

// AgentLoop orchestrates the think-act-observe cycle for API-mode agents.
// It replaces tmux sessions for agents configured with provider_type="api".
// The LLM runs remotely; tools execute locally in the git worktree.
type AgentLoop struct {
	client   llm.Client
	executor *Executor
	tools    []llm.ToolDef
	config   *AgentLoopConfig
	context  *ContextManager

	mu          sync.Mutex
	state       LoopState
	currentTask string
	iteration   int
	totalTokens int
	startedAt   time.Time
	lastActive  time.Time
	lastError   error

	workCh     chan string
	cancelFunc context.CancelFunc
	done       chan struct{}
}

// NewAgentLoop creates an agent loop for an API-mode agent.
func NewAgentLoop(client llm.Client, executor *Executor, cfg *AgentLoopConfig) *AgentLoop {
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = DefaultMaxIterations
	}
	if cfg.MaxTokensPerTask <= 0 {
		cfg.MaxTokensPerTask = DefaultMaxTokensPerTask
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = DefaultIdleTimeout
	}
	if cfg.ToolTimeout <= 0 {
		cfg.ToolTimeout = DefaultToolTimeout
	}

	contextWindow := 0
	if mi := client.ModelInfo(); mi != nil {
		contextWindow = mi.ContextWindow
	}

	return &AgentLoop{
		client:   client,
		executor: executor,
		tools:    GTTools(),
		config:   cfg,
		context:  NewContextManager(contextWindow),
		state:    StateStopped,
		workCh:   make(chan string, 1),
		done:     make(chan struct{}),
	}
}

// Start begins the agent loop. It runs until stopped or the context is cancelled.
// The loop:
// 1. Waits for work (via AssignWork or initial gt_prime)
// 2. Enters the think-act-observe cycle
// 3. Returns to idle when task is complete
func (l *AgentLoop) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	l.cancelFunc = cancel

	l.mu.Lock()
	l.state = StateIdle
	l.startedAt = time.Now()
	l.lastActive = time.Now()
	l.mu.Unlock()

	defer func() {
		l.mu.Lock()
		l.state = StateStopped
		l.mu.Unlock()
		close(l.done)
	}()

	log.Printf("[agentloop] Started for %s (role=%s, rig=%s)", l.config.Actor, l.config.Role, l.config.RigName)

	idleTimer := time.NewTimer(l.config.IdleTimeout)
	defer idleTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("[agentloop] Context cancelled, stopping")
			return ctx.Err()

		case <-idleTimer.C:
			log.Printf("[agentloop] Idle timeout reached (%v), sleeping", l.config.IdleTimeout)
			// Reset and continue - the loop stays alive but logs the idle state.
			// In production, the deacon may use this signal to scale down.
			idleTimer.Reset(l.config.IdleTimeout)

		case task := <-l.workCh:
			idleTimer.Stop()
			l.mu.Lock()
			l.state = StateWorking
			l.currentTask = task
			l.iteration = 0
			l.totalTokens = 0
			l.lastActive = time.Now()
			l.mu.Unlock()

			err := l.runTask(ctx, task)

			l.mu.Lock()
			l.state = StateIdle
			l.currentTask = ""
			l.lastActive = time.Now()
			if err != nil {
				l.lastError = err
				log.Printf("[agentloop] Task failed: %v", err)
			}
			l.mu.Unlock()

			if l.config.OnTaskComplete != nil {
				l.config.OnTaskComplete(task, l.iteration, l.totalTokens, err)
			}

			// Reset idle timer after task completion
			idleTimer.Reset(l.config.IdleTimeout)
		}
	}
}

// AssignWork sends a new task to the running agent loop.
// This is the API-mode equivalent of tmux NudgeSession.
func (l *AgentLoop) AssignWork(task string) error {
	l.mu.Lock()
	state := l.state
	l.mu.Unlock()

	if state == StateStopped {
		return fmt.Errorf("agent loop is stopped")
	}
	if state == StateWorking {
		return fmt.Errorf("agent is already working on a task")
	}

	select {
	case l.workCh <- task:
		return nil
	default:
		return fmt.Errorf("work channel full, agent may be busy")
	}
}

// Stop gracefully stops the agent loop.
func (l *AgentLoop) Stop() error {
	l.mu.Lock()
	state := l.state
	l.mu.Unlock()

	if state == StateStopped {
		return nil
	}

	if l.cancelFunc != nil {
		l.cancelFunc()
	}

	// Wait for loop to finish with timeout
	select {
	case <-l.done:
		return nil
	case <-time.After(30 * time.Second):
		return fmt.Errorf("agent loop did not stop within 30 seconds")
	}
}

// Status returns the current loop status.
func (l *AgentLoop) Status() *LoopStatus {
	l.mu.Lock()
	defer l.mu.Unlock()

	status := &LoopStatus{
		State:       l.state,
		CurrentTask: l.currentTask,
		Iteration:   l.iteration,
		TotalTokens: l.totalTokens,
		StartedAt:   l.startedAt,
		LastActive:  l.lastActive,
	}
	if l.lastError != nil {
		status.Error = l.lastError.Error()
	}
	return status
}

// IsRunning returns true if the agent loop is running (idle or working).
func (l *AgentLoop) IsRunning() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.state == StateIdle || l.state == StateWorking
}

// runTask executes a single task using the think-act-observe cycle.
func (l *AgentLoop) runTask(ctx context.Context, task string) error {
	// Build initial conversation
	var messages []llm.Message

	if l.config.SystemPrompt != "" {
		messages = append(messages, llm.Message{
			Role:    "system",
			Content: l.config.SystemPrompt,
		})
	}

	messages = append(messages, llm.Message{
		Role:    "user",
		Content: task,
	})

	for i := 0; i < l.config.MaxIterations; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		l.mu.Lock()
		l.iteration = i + 1
		l.lastActive = time.Now()
		l.mu.Unlock()

		// Context window management
		if l.context.NeedsTruncation(messages) {
			log.Printf("[agentloop] Context window pressure at iteration %d, truncating", i+1)
			messages = l.context.Truncate(messages)
		}

		// Think: call LLM
		resp, err := l.client.Chat(ctx, &llm.ChatRequest{
			Messages: messages,
			Tools:    l.tools,
		})
		if err != nil {
			return fmt.Errorf("LLM call failed at iteration %d: %w", i+1, err)
		}

		// Track token usage
		if resp.Usage != nil {
			l.mu.Lock()
			l.totalTokens += resp.Usage.TotalTokens
			l.mu.Unlock()

			// Check token budget
			if l.totalTokens > l.config.MaxTokensPerTask {
				return fmt.Errorf("token budget exceeded: %d > %d", l.totalTokens, l.config.MaxTokensPerTask)
			}
		}

		// Add assistant response to history
		assistantMsg := llm.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		messages = append(messages, assistantMsg)

		// If no tool calls, the model is done with the task
		if len(resp.ToolCalls) == 0 {
			log.Printf("[agentloop] Task complete after %d iterations (~%d tokens)",
				i+1, l.totalTokens)
			return nil
		}

		// Act: execute each tool call
		for _, tc := range resp.ToolCalls {
			toolCtx, toolCancel := context.WithTimeout(ctx, l.config.ToolTimeout)

			result, err := l.executor.Execute(toolCtx, tc)
			toolCancel()

			if err != nil {
				result = fmt.Sprintf("Error executing %s: %v", tc.Name, err)
				log.Printf("[agentloop] Tool error: %s: %v", tc.Name, err)
			}

			// Observe: add tool result to conversation
			messages = append(messages, llm.Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
				Name:       tc.Name,
			})
		}

		// Publish heartbeat
		if l.config.OnHeartbeat != nil && (i+1)%5 == 0 {
			l.config.OnHeartbeat(StateWorking, i+1, l.totalTokens)
		}
	}

	return fmt.Errorf("max iterations (%d) reached without completion", l.config.MaxIterations)
}
