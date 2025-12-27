# No-Tmux Mode

Gas Town can operate without tmux or the daemon, using beads as the universal data plane for passing args and context.

## Background

Tmux instability can crash workers. The daemon relies on tmux for:
- Session management (creating/killing panes)
- Nudging agents via SendKeys
- Crash detection via pane-died hooks

When tmux is unstable, the entire operation fails. No-tmux mode enables continued operation in degraded mode.

## Key Insight: Beads Replace SendKeys

In normal mode, `--args` are injected via tmux SendKeys. In no-tmux mode:
- Args are stored in the pinned bead description (`attached_args` field)
- `gt prime` reads and displays args from the pinned bead
- No prompt injection needed - agents discover everything via `bd show`

## Usage

### Slinging Work with Args

```bash
# Normal mode: args injected via tmux + stored in bead
gt sling gt-abc --args "patch release"

# In no-tmux mode: nudge fails gracefully, but args are in the bead
# Agent discovers args via gt prime when it starts
```

### Spawning without Tmux

```bash
# Use --naked to skip tmux session creation
gt spawn gastown/Toast --issue gt-abc --naked

# Output tells you how to start the agent manually:
#   cd ~/gt/gastown/polecats/Toast
#   claude
```

### Agent Discovery

When an agent starts (manually or via IDE), the SessionStart hook runs `gt prime`, which:
1. Detects the agent's role from cwd
2. Finds pinned work
3. Displays attached args prominently
4. Shows current molecule step

The agent sees:

```
## ATTACHED WORK DETECTED

Pinned bead: gt-abc
Attached molecule: gt-xyz
Attached at: 2025-12-26T12:00:00Z

ARGS (use these to guide execution):
  patch release

**Progress:** 0/5 steps complete
```

## What Works vs What's Degraded

### What Still Works

| Feature | How It Works |
|---------|--------------|
| Propulsion via pinned beads | Agents pick up work on startup |
| Self-handoff | Agents can cycle themselves |
| Patrol loops | Deacon, Witness, Refinery keep running |
| Mail system | Beads-based, no tmux needed |
| Args passing | Stored in bead description |
| Work discovery | `gt prime` reads from bead |

### What Is Degraded

| Limitation | Impact |
|------------|--------|
| No interrupts | Cannot nudge busy agents mid-task |
| Polling only | Agents must actively check inbox (no push) |
| Await steps block | "Wait for human" steps require manual agent restart |
| No crash detection | pane-died hooks unavailable |
| Manual startup | Human must start each agent in separate terminal |

### Workflow Implications

- **Patrol agents** work fine (they poll as part of their loop)
- **Task workers** need restart to pick up new work
- Cannot redirect a busy worker to urgent task
- Human must monitor and restart crashed agents

## Commands Summary

| Command | Purpose |
|---------|---------|
| `gt sling <bead> --args "..."` | Store args in bead, nudge gracefully |
| `gt spawn --naked` | Assign work without tmux session |
| `gt prime` | Display attached work + args on startup |
| `gt mol status` | Show current work status including args |
| `bd show <bead>` | View raw bead with attached_args field |

## Implementation Details

Args are stored in the bead description as a `key: value` field:

```
attached_molecule: gt-xyz
attached_at: 2025-12-26T12:00:00Z
attached_args: patch release
```

The `beads.AttachmentFields` struct includes:
- `AttachedMolecule` - the work molecule ID
- `AttachedAt` - timestamp when attached
- `AttachedArgs` - natural language instructions

These are parsed by `beads.ParseAttachmentFields()` and formatted by `beads.FormatAttachmentFields()`.
