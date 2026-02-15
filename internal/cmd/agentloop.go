package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/agentloop"
	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/llm"
	"github.com/steveyegge/gastown/internal/workspace"
)

var (
	alRig          string
	alRole         string
	alInstance     string
	alWorkdir      string
	alAgentsConfig string
	alAgentID      string

	alTask         string
	alPrimeInterval time.Duration

	alSystemPrompt  string
	alMaxIterations int
	alMaxTokens     int
	alIdleTimeout   time.Duration
	alToolTimeout   time.Duration
)

var agentLoopCmd = &cobra.Command{
	Use:   "agentloop",
	Short: "Run API-mode agent loops (Go-native)",
	RunE:  requireSubcommand,
}

var agentLoopRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run a single API-mode agent loop in the foreground",
	RunE:  runAgentLoopRun,
}

func runAgentLoopRun(cmd *cobra.Command, args []string) error {
	townRoot, err := townRootFromEnvOrCwdAgentLoop()
	if err != nil {
		return err
	}

	// In Docker deployments the workspace marker may not exist yet.
	if err := ensureWorkspaceSkeleton(townRoot); err != nil {
		return fmt.Errorf("initializing workspace skeleton: %w", err)
	}

	role := strings.TrimSpace(alRole)
	if role == "" {
		return fmt.Errorf("--role is required")
	}

	rigName := strings.TrimSpace(alRig)
	if rigName == "" {
		if envRig := strings.TrimSpace(os.Getenv("GT_RIG")); envRig != "" {
			rigName = envRig
		} else {
			rigName = filepath.Base(filepath.Clean(townRoot))
		}
	}

	instance := strings.TrimSpace(alInstance)
	if instance == "" {
		instance = "agent1"
	}

	workdir := strings.TrimSpace(alWorkdir)
	if workdir == "" {
		workdir = townRoot
	}
	if err := ensureRigScopedWorkdirAgentLoop(townRoot, workdir); err != nil {
		return err
	}

	agentsPath := strings.TrimSpace(alAgentsConfig)
	if agentsPath == "" {
		agentsPath = filepath.Join(townRoot, "settings", "agents.json")
	}

	agentID := strings.TrimSpace(alAgentID)
	if agentID == "" {
		return fmt.Errorf("--agent is required (e.g., claude-api, gpt4-api)")
	}

	agentsFile, err := config.LoadAgentsAPIFile(agentsPath)
	if err != nil {
		return err
	}

	resolved, err := agentsFile.Resolve(agentID)
	if err != nil {
		return err
	}

	client, err := llm.NewClient(resolved.API)
	if err != nil {
		return err
	}

	// Honor retry settings from agents.json.
	if resolved.Retry != nil && resolved.Retry.MaxRetries > 0 {
		rc := llm.RetryConfig{
			MaxRetries:     resolved.Retry.MaxRetries,
			InitialBackoff: time.Duration(resolved.Retry.InitialBackoffMS) * time.Millisecond,
			MaxBackoff:     time.Duration(resolved.Retry.MaxBackoffMS) * time.Millisecond,
		}
		client = llm.WithRetry(client, rc)
	}

	actor := makeActor(rigName, role, instance)

	executor := agentloop.NewExecutor(
		workdir,
		rigName,
		townRoot, // rigPath (rig-rooted model)
		townRoot, // townRoot
		actor,
		role,
	)

	cfg := &agentloop.AgentLoopConfig{
		SystemPrompt:     alSystemPrompt,
		MaxIterations:    alMaxIterations,
		MaxTokensPerTask: alMaxTokens,
		IdleTimeout:      alIdleTimeout,
		ToolTimeout:      alToolTimeout,
		Role:             role,
		RigName:          rigName,
		Actor:            actor,
		OnHeartbeat: func(state agentloop.LoopState, iteration int, totalTokens int) {
			// Keep minimal output; full lifecycle/Nostr publishing is handled elsewhere.
			_ = state
			_ = iteration
			_ = totalTokens
		},
		OnTaskComplete: func(task string, iterations int, totalTokens int, err error) {
			_ = json.Marshal // keep import used even if logging is disabled below
			if err != nil {
				fmt.Fprintf(os.Stderr, "[agentloop] task failed: iterations=%d tokens=%d err=%v\n", iterations, totalTokens, err)
				return
			}
			fmt.Fprintf(os.Stdout, "[agentloop] task complete: iterations=%d tokens=%d\n", iterations, totalTokens)
		},
	}

	loop := agentloop.NewAgentLoop(client, executor, cfg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Background coordinator: wait for loop to become running, then seed work and/or start prime polling.
	go func() {
		if !waitForLoopRunning(ctx, loop, 10*time.Second) {
			return
		}

		task := strings.TrimSpace(alTask)
		if task != "" {
			_ = loop.AssignWork(task)
		}

		if alPrimeInterval > 0 {
			runPrimeTicker(ctx, loop, executor, alPrimeInterval)
		}
	}()

	err = loop.Start(ctx)

	// Treat signal cancellation as a clean shutdown.
	if ctx.Err() != nil {
		return nil
	}
	return err
}

func runPrimeTicker(ctx context.Context, loop *agentloop.AgentLoop, executor *agentloop.Executor, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			primeCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			out, err := executor.Execute(primeCtx, llm.ToolCall{Name: "gt_prime"})
			cancel()
			if err != nil {
				continue
			}
			task := strings.TrimSpace(out)
			if task == "" {
				continue
			}
			// AssignWork fails if busy; ignore and try again on next tick.
			_ = loop.AssignWork(task)
		}
	}
}

func waitForLoopRunning(ctx context.Context, loop *agentloop.AgentLoop, timeout time.Duration) bool {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return loop.IsRunning()
		case <-ticker.C:
			if loop.IsRunning() {
				return true
			}
		}
	}
}

func makeActor(rig, role, instance string) string {
	r := strings.TrimSpace(rig)
	ro := strings.TrimSpace(role)
	in := strings.TrimSpace(instance)

	switch ro {
	case "polecat":
		if in == "" {
			in = "polecat"
		}
		return r + "/polecats/" + in
	case "crew":
		if in == "" {
			in = "crew"
		}
		return r + "/crew/" + in
	default:
		if in == "" {
			return r + "/" + ro
		}
		return r + "/" + ro + "/" + in
	}
}

func ensureRigScopedWorkdirAgentLoop(townRoot, workdir string) error {
	tr := filepath.Clean(townRoot)
	wd := filepath.Clean(workdir)
	if tr != wd {
		return fmt.Errorf("invalid workdir: must equal GT_TOWN_ROOT (rig-scoped). workdir=%q townRoot=%q", wd, tr)
	}
	return nil
}

func townRootFromEnvOrCwdAgentLoop() (string, error) {
	if env := strings.TrimSpace(os.Getenv("GT_TOWN_ROOT")); env != "" {
		return filepath.Clean(env), nil
	}
	townRoot, err := workspace.FindFromCwd()
	if err != nil {
		return "", fmt.Errorf("finding town root: %w", err)
	}
	return filepath.Clean(townRoot), nil
}

func init() {
	agentLoopRunCmd.Flags().StringVar(&alRig, "rig", "", "Rig name (defaults to $GT_RIG or basename of GT_TOWN_ROOT)")
	agentLoopRunCmd.Flags().StringVar(&alRole, "role", "", "Agent role (required)")
	agentLoopRunCmd.Flags().StringVar(&alInstance, "instance", "agent1", "Agent instance name (e.g., Toast)")
	agentLoopRunCmd.Flags().StringVar(&alWorkdir, "workdir", "", "Rig workdir (must equal GT_TOWN_ROOT)")
	agentLoopRunCmd.Flags().StringVar(&alAgentsConfig, "agents-config", "", "Path to agents.json (default: $GT_TOWN_ROOT/settings/agents.json)")
	agentLoopRunCmd.Flags().StringVar(&alAgentID, "agent", "", "Agent id in agents.json (e.g., claude-api, gpt4-api)")

	agentLoopRunCmd.Flags().StringVar(&alTask, "task", "", "Seed an initial task immediately")
	agentLoopRunCmd.Flags().DurationVar(&alPrimeInterval, "prime-interval", 0, "Poll for work via `gt prime` at this interval (e.g., 30s)")

	agentLoopRunCmd.Flags().StringVar(&alSystemPrompt, "system-prompt", "", "System prompt prepended to conversations")
	agentLoopRunCmd.Flags().IntVar(&alMaxIterations, "max-iterations", 0, "Max think-act iterations per task (0 uses default)")
	agentLoopRunCmd.Flags().IntVar(&alMaxTokens, "max-tokens", 0, "Max tokens per task (0 uses default)")
	agentLoopRunCmd.Flags().DurationVar(&alIdleTimeout, "idle-timeout", 0, "Idle timeout (0 uses default)")
	agentLoopRunCmd.Flags().DurationVar(&alToolTimeout, "tool-timeout", 0, "Tool timeout (0 uses default)")

	_ = agentLoopRunCmd.MarkFlagRequired("role")
	_ = agentLoopRunCmd.MarkFlagRequired("agent")

	agentLoopCmd.AddCommand(agentLoopRunCmd)
	rootCmd.AddCommand(agentLoopCmd)
}