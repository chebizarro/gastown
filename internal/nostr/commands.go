package nostr

import (
	"fmt"
	"strings"
)

// CommandRouter dispatches DM commands to handler functions.
// Each role (Mayor, Witness, Refinery, Deacon) has its own set of commands.
type CommandRouter struct {
	role     string
	commands map[string]CommandHandler
	help     map[string]string // command → help text
}

// CommandHandler processes a parsed command.
type CommandHandler func(args []string) (string, error)

// NewCommandRouter creates a command router for a specific role.
func NewCommandRouter(role string) *CommandRouter {
	return &CommandRouter{
		role:     role,
		commands: make(map[string]CommandHandler),
		help:     make(map[string]string),
	}
}

// Register adds a command handler.
func (r *CommandRouter) Register(name, helpText string, handler CommandHandler) {
	r.commands[strings.ToLower(name)] = handler
	r.help[strings.ToLower(name)] = helpText
}

// Dispatch parses and executes a command from a DM.
func (r *CommandRouter) Dispatch(content string) (string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return r.HelpText(), nil
	}

	parts := strings.Fields(content)
	cmd := strings.ToLower(parts[0])
	args := parts[1:]

	handler, ok := r.commands[cmd]
	if !ok {
		return fmt.Sprintf("Unknown command: %s\n\n%s", cmd, r.HelpText()), nil
	}

	return handler(args)
}

// HelpText returns formatted help for all registered commands.
func (r *CommandRouter) HelpText() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Available commands (%s):\n\n", r.role))

	for name, helpText := range r.help {
		sb.WriteString(fmt.Sprintf("  %s - %s\n", name, helpText))
	}

	return sb.String()
}

// RegisterMayorCommands registers the standard mayor commands.
func RegisterMayorCommands(router *CommandRouter, executor func(cmd string, args []string) (string, error)) {
	router.Register("assign", "assign <polecat> <issue> - Assign work to a polecat", func(args []string) (string, error) {
		if len(args) < 2 {
			return "Usage: assign <polecat> <issue>", nil
		}
		return executor("assign", args)
	})

	router.Register("status", "status - Show summary of all rigs and agents", func(args []string) (string, error) {
		return executor("status", args)
	})

	router.Register("kill", "kill <target> - Kill an agent session", func(args []string) (string, error) {
		if len(args) < 1 {
			return "Usage: kill <target>", nil
		}
		return executor("kill", args)
	})

	router.Register("spawn", "spawn <count> - Spawn polecat workers", func(args []string) (string, error) {
		if len(args) < 1 {
			return "Usage: spawn <count>", nil
		}
		return executor("spawn", args)
	})

	router.Register("convoy", "convoy <convoy-id> - Show convoy status", func(args []string) (string, error) {
		if len(args) < 1 {
			return "Usage: convoy <convoy-id>", nil
		}
		return executor("convoy", args)
	})

	router.Register("help", "help - Show available commands", func(args []string) (string, error) {
		return router.HelpText(), nil
	})
}

// RegisterWitnessCommands registers the standard witness commands.
func RegisterWitnessCommands(router *CommandRouter, executor func(cmd string, args []string) (string, error)) {
	router.Register("status", "status - Show patrol and merge queue status", func(args []string) (string, error) {
		return executor("status", args)
	})

	router.Register("merge-queue", "merge-queue - Show merge queue details", func(args []string) (string, error) {
		return executor("merge-queue", args)
	})

	router.Register("patrol", "patrol - Trigger immediate patrol", func(args []string) (string, error) {
		return executor("patrol", args)
	})

	router.Register("nudge", "nudge <polecat> - Nudge a polecat", func(args []string) (string, error) {
		if len(args) < 1 {
			return "Usage: nudge <polecat>", nil
		}
		return executor("nudge", args)
	})

	router.Register("help", "help - Show available commands", func(args []string) (string, error) {
		return router.HelpText(), nil
	})
}

// RegisterRefineryCommands registers the standard refinery commands.
func RegisterRefineryCommands(router *CommandRouter, executor func(cmd string, args []string) (string, error)) {
	router.Register("status", "status - Show merge processing status", func(args []string) (string, error) {
		return executor("status", args)
	})

	router.Register("pause", "pause - Pause merge processing", func(args []string) (string, error) {
		return executor("pause", args)
	})

	router.Register("resume", "resume - Resume merge processing", func(args []string) (string, error) {
		return executor("resume", args)
	})

	router.Register("retry", "retry <branch> - Retry a failed merge", func(args []string) (string, error) {
		if len(args) < 1 {
			return "Usage: retry <branch>", nil
		}
		return executor("retry", args)
	})

	router.Register("help", "help - Show available commands", func(args []string) (string, error) {
		return router.HelpText(), nil
	})
}

// RegisterDeaconCommands registers the standard deacon commands.
func RegisterDeaconCommands(router *CommandRouter, executor func(cmd string, args []string) (string, error)) {
	router.Register("restart", "restart <role> <rig> - Kill and respawn an agent", func(args []string) (string, error) {
		if len(args) < 2 {
			return "Usage: restart <role> <rig>", nil
		}
		return executor("restart", args)
	})

	router.Register("agents", "agents - List all agents and their status", func(args []string) (string, error) {
		return executor("agents", args)
	})

	router.Register("spool", "spool - Show Nostr spool status", func(args []string) (string, error) {
		return executor("spool", args)
	})

	router.Register("drain", "drain - Force spool drain", func(args []string) (string, error) {
		return executor("drain", args)
	})

	router.Register("help", "help - Show available commands", func(args []string) (string, error) {
		return router.HelpText(), nil
	})
}
