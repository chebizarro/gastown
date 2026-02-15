package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/agentloop"
	"github.com/steveyegge/gastown/internal/mcp"
	"github.com/steveyegge/gastown/internal/workspace"
)

var (
	mcpAddr      string
	mcpRig       string
	mcpWorkdir   string
	mcpAuthToken string
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Run the MCP (Model Context Protocol) server",
	RunE:  requireSubcommand,
}

var mcpServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the MCP HTTP/SSE server (rig-scoped)",
	RunE:  runMCPServe,
}

func runMCPServe(cmd *cobra.Command, args []string) error {
	townRoot, err := townRootFromEnvOrCwd()
	if err != nil {
		return err
	}

	// In Docker deployments the workspace marker may not exist yet.
	if err := ensureWorkspaceSkeleton(townRoot); err != nil {
		return fmt.Errorf("initializing workspace skeleton: %w", err)
	}

	rigName := strings.TrimSpace(mcpRig)
	if rigName == "" {
		// Prefer GT_RIG if present, else derive from townRoot basename.
		if envRig := strings.TrimSpace(os.Getenv("GT_RIG")); envRig != "" {
			rigName = envRig
		} else {
			rigName = filepath.Base(filepath.Clean(townRoot))
		}
	}

	workdir := strings.TrimSpace(mcpWorkdir)
	if workdir == "" {
		workdir = townRoot
	}
	if err := ensureRigScopedWorkdir(townRoot, workdir); err != nil {
		return err
	}

	authToken := strings.TrimSpace(mcpAuthToken)
	if authToken == "" {
		authToken = strings.TrimSpace(os.Getenv("GT_MCP_TOKEN"))
	}

	actor := fmt.Sprintf("%s/deacon/mcp", rigName)
	role := "deacon"

	executor := agentloop.NewExecutor(
		workdir,
		rigName,
		townRoot, // rigPath (rig-rooted model)
		townRoot, // townRoot
		actor,
		role,
	)

	addr := strings.TrimSpace(mcpAddr)
	srv := mcp.NewServer(addr, executor, authToken)
	srv.RegisterGTTools()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := srv.Start(ctx); err != nil {
		// When ctx is canceled, server.go returns ctx.Err() upstream only if ListenAndServe closes with non-ServerClosed.
		// Treat context cancellation as a clean shutdown.
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	return nil
}

func ensureRigScopedWorkdir(townRoot, workdir string) error {
	tr := filepath.Clean(townRoot)
	wd := filepath.Clean(workdir)
	if tr != wd {
		return fmt.Errorf("invalid workdir: must equal GT_TOWN_ROOT (rig-scoped). workdir=%q townRoot=%q", wd, tr)
	}
	return nil
}

func townRootFromEnvOrCwd() (string, error) {
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
	mcpServeCmd.Flags().StringVar(&mcpAddr, "addr", "", "Listen address (default: 127.0.0.1:9500)")
	mcpServeCmd.Flags().StringVar(&mcpRig, "rig", "", "Rig name (defaults to $GT_RIG or basename of GT_TOWN_ROOT)")
	mcpServeCmd.Flags().StringVar(&mcpWorkdir, "workdir", "", "Rig workdir (must equal GT_TOWN_ROOT)")
	mcpServeCmd.Flags().StringVar(&mcpAuthToken, "auth-token", "", "Bearer auth token (defaults to $GT_MCP_TOKEN)")

	mcpCmd.AddCommand(mcpServeCmd)
	rootCmd.AddCommand(mcpCmd)
}