# Gas Town Nostr Integration Guide

> Version: 0.3.0 | Status: Draft | Last updated: 2026-02-13

Gas Town can publish its operational state to [Nostr](https://nostr.com) relays, making agent activity, lifecycle events, convoy progress, and issue state visible to external consumers (like the Flotilla dashboard or other monitoring tools) without filesystem access.

This guide covers how to enable, configure, and use the Nostr integration.

---

## Table of Contents

1. [Quick Start](#quick-start)
2. [Prerequisites](#prerequisites)
3. [Configuration](#configuration)
   - [Environment Variables](#environment-variables)
   - [Nostr Config File](#nostr-config-file)
   - [Agent Provider Modes](#agent-provider-modes)
4. [Features](#features)
   - [Activity Feed (Dual-Write)](#activity-feed-dual-write)
   - [Agent Lifecycle & Heartbeats](#agent-lifecycle--heartbeats)
   - [Convoy State Publishing](#convoy-state-publishing)
   - [Issue Mirroring](#issue-mirroring)
   - [Protocol Events](#protocol-events)
   - [Chat (DMs & Channels)](#chat-dms--channels)
   - [Work Queues](#work-queues)
5. [Health & Monitoring](#health--monitoring)
6. [Sunset Flags & Migration](#sunset-flags--migration)
7. [Spool & Offline Resilience](#spool--offline-resilience)
8. [Security](#security)
9. [Troubleshooting](#troubleshooting)
10. [Example Configurations](#example-configurations)

---

## Quick Start

1. **Set up a NIP-46 signer** (bunker) for your Gas Town identity.
2. **Create a Nostr config file** at `~/gt/.nostr.json` (see [Configuration](#nostr-config-file)).
3. **Enable Nostr** via environment variable:

```bash
export GT_NOSTR_ENABLED=1
```

4. **Verify** the setup:

```bash
gt nostr health
```

That's it. Gas Town will now dual-write events to both the local JSONL file and your configured Nostr relays.

---

## Prerequisites

- **Go 1.21+** (for building from source)
- **Nostr relays** — At least one write relay and one read relay (can be the same)
- **NIP-46 bunker** — An external signer for secure key management (no nsec on disk)
- **fiatjaf.com/nostr** — The Go Nostr library (added automatically via `go mod tidy`)

### Optional

- **Blossom server** — For offloading large artifacts (patches, diffs, screenshots)
- **Flotilla extension** — The `gastown-flotilla-extension` Svelte app for dashboard visualization

---

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `GT_NOSTR_ENABLED` | `0` | Master switch. Set to `1` to enable Nostr publishing. |
| `GT_NOSTR_CONFIG` | `~/gt/.nostr.json` | Path to the Nostr configuration file. |
| `GT_EVENTS_LOCAL` | `1` | When `1`, continue writing to `.events.jsonl`. |
| `GT_FEED_CURATOR` | `1` | When `1`, the feed curator daemon runs locally. |
| `GT_CONVOY_LOCAL` | `1` | When `1`, convoy uses local `bd dep list`. |
| `GT_MAIL_LOCAL` | `1` | When `1`, beads-native mail routing is active. |
| `GT_NUDGE_LOCAL` | `1` | When `1`, tmux nudge (local) is active. |
| `GT_TOWN_ROOT` | auto-detected | Path to the Gas Town root directory. |

**Note**: All `GT_*_LOCAL` flags default to `1` (enabled). During migration, both local and Nostr paths run in parallel (dual-write). Set individual flags to `0` to sunset local paths once Nostr equivalents are validated.

### Nostr Config File

The Nostr configuration lives at `~/gt/.nostr.json` (or the path specified by `GT_NOSTR_CONFIG`). Here's a complete example:

```json
{
  "type": "nostr",
  "version": 1,
  "enabled": true,
  "read_relays": [
    "wss://relay.gastown.example.com",
    "wss://relay.damus.io"
  ],
  "write_relays": [
    "wss://relay.gastown.example.com"
  ],
  "blossom_servers": [
    "https://blossom.gastown.example.com"
  ],
  "dm_relays": [
    "wss://dm-inbox.gastown.example.com"
  ],
  "identities": {
    "deacon": {
      "pubkey": "abc123...",
      "signer": {
        "type": "nip46",
        "bunker": "bunker://npub1...?relay=wss://bunker.example.com"
      },
      "profile": {
        "name": "deacon",
        "display_name": "Gas Town Deacon",
        "about": "Daemon beacon. Receives heartbeats, runs plugins and monitoring.",
        "bot": true
      }
    }
  },
  "defaults": {
    "heartbeat_interval_seconds": 60,
    "spool_drain_interval_seconds": 30,
    "convoy_recompute_interval_seconds": 300,
    "issue_mirror_poll_interval_seconds": 120
  }
}
```

#### Config Fields

| Field | Required | Description |
|-------|----------|-------------|
| `type` | Yes | Must be `"nostr"` |
| `version` | Yes | Config schema version (currently `1`) |
| `enabled` | Yes | Whether Nostr publishing is active |
| `read_relays` | Yes | Relay URLs for subscriptions |
| `write_relays` | Yes | Relay URLs for publishing events |
| `blossom_servers` | No | Blossom server URLs for blob uploads |
| `dm_relays` | No | Relay URLs specifically for DM delivery |
| `identities` | Yes | Map of role → identity config (see below) |
| `defaults` | No | Timing and behavior defaults |

#### Identity Configuration

Each identity maps a Gas Town role to a Nostr keypair:

```json
{
  "pubkey": "hex-encoded-public-key",
  "signer": {
    "type": "nip46",
    "bunker": "bunker://npub1...?relay=wss://bunker.example.com"
  },
  "profile": {
    "name": "deacon",
    "display_name": "Gas Town Deacon",
    "about": "Description for NIP-01 profile",
    "picture": "https://example.com/avatar.png",
    "bot": true
  }
}
```

**Supported signer types**:
- `nip46` — NIP-46 external signer (bunker). **Recommended for production**.
- `local` — Local private key. **Development/testing only**.

### Agent Provider Modes

Gas Town agents can execute via three different providers, configured in `agents.json`:

| Provider | Description | When to Use |
|----------|-------------|-------------|
| `cli` (default) | Runs via tmux + Claude Code CLI | Standard setup, single machine |
| `api` | Direct LLM API calls via Go agent loop | Cloud models, no tmux dependency |
| `mcp` | Connects to remote MCP tool server | GPU servers on LAN, distributed rigs |

#### API Mode Configuration

```json
{
  "preset": "claude-api",
  "provider_type": "api",
  "api": {
    "api_type": "anthropic",
    "model": "claude-sonnet-4-20250514",
    "api_key": "$ANTHROPIC_API_KEY",
    "context_window": 200000,
    "supports_tools": true
  }
}
```

Supported `api_type` values:
- `openai` — OpenAI Chat Completions format (also works with Ollama, vLLM, LiteLLM)
- `anthropic` — Anthropic Messages API format

For OpenAI-compatible endpoints, `base_url` is required. For Anthropic, it defaults to `https://api.anthropic.com`.

API keys prefixed with `$` are resolved from environment variables (e.g., `"$ANTHROPIC_API_KEY"` reads `$ANTHROPIC_API_KEY`).

#### MCP Mode Configuration

```json
{
  "preset": "gpu-server",
  "provider_type": "mcp",
  "mcp": {
    "transport": "sse",
    "url": "http://gpu-server.local:9500",
    "auth_token": "$GT_MCP_TOKEN",
    "exposed_tools": ["file_read", "file_write", "git_status", "gt_prime", "gt_done"]
  }
}
```

See [docs/examples/](examples/) for complete configuration examples.

---

## Features

### Activity Feed (Dual-Write)

When Nostr is enabled, every event written to `.events.jsonl` is also published as a **kind 30315** (`LOG_STATUS`) Nostr event. This includes:

- `sling`, `hook`, `unhook`, `handoff`, `done`
- `session_start`, `session_end`, `session_death`
- `merge_started`, `merged`, `merge_failed`
- `patrol_started`, `patrol_complete`
- All other event types

The dual-write is **async and non-blocking** — the local JSONL file remains the source of truth. If relay publishing fails, events are automatically spooled for later delivery.

**Correlation tags** are extracted from payloads and added as Nostr tags for filtering:
- `t` (issue/bead ID)
- `convoy` (convoy ID)
- `bead` (bead ID)
- `session` (session ID)
- `branch`, `mr` (merge-related)

### Agent Lifecycle & Heartbeats

Each agent publishes **kind 30316** (`LIFECYCLE`) NIP-33 replaceable events:

| State | Meaning |
|-------|---------|
| `ready` | Agent registered and waiting for work |
| `busy` | Agent actively processing a task |
| `retiring` | Agent shutting down gracefully |
| `dead` | Agent has terminated (published by deacon on crash detection) |

**Heartbeats** are published every 60 seconds (agents) or 30 seconds (deacon). Each heartbeat is a replaceable event keyed by the agent's instance ID, so only the latest state is visible.

The deacon detects stale agents by checking heartbeat timestamps:
- Agents: stale after 3× heartbeat interval (180s)
- Deacon: stale after 2× heartbeat interval (60s)

### Convoy State Publishing

**Kind 30318** (`GT_CONVOY_STATE`) replaceable events publish the current state of convoys:
- Tracked issues with status, assignee, and dependencies
- Summary statistics (total, done, in-progress, blocked)
- Convoy status (active, paused, completed)

The `d` tag is set to the convoy ID for NIP-33 deduplication.

### Issue Mirroring

**Kind 30319** (`GT_BEADS_ISSUE_STATE`) replaceable events mirror beads issues to Nostr:
- Full issue metadata (title, status, priority, type)
- Dependency graph (blocks, blocked_by, related)
- Blob references (links to Blossom-stored artifacts)

Issues are polled at a configurable interval (default: 120s). A content hash is computed to avoid republishing unchanged issues.

### Protocol Events

**Kind 30320** (`GT_PROTOCOL_EVENT`) events carry machine-to-machine signals:

| Message Type | Description |
|-------------|-------------|
| `POLECAT_DONE` | Polecat finished its assigned work |
| `MERGE_READY` | Branch is ready for merge |
| `MERGED` | Branch has been merged |
| `MERGE_FAILED` | Merge attempt failed |
| `REWORK_REQUEST` | Work needs rework |
| `HELP` | Agent requesting assistance |

Protocol events include sender, recipient, and correlation tags for routing.

### Chat (DMs & Channels)

#### Direct Messages (NIP-17)

Gas Town uses **NIP-17** (gift-wrapped sealed events) for private agent-to-agent and human-to-agent communication:
- `SendDM()` — Send an encrypted message to a recipient
- `SendInterrupt()` — High-priority DM with urgent flag
- `SendHandoff()` — Structured handoff message with context, status, and next steps

> **⚠️ Note**: Full NIP-17 implementation requires NIP-44 encryption support. The current implementation has a plaintext kind 4 fallback that is **disabled by default**. Set `AllowPlaintextFallback=true` on `DMSender` only for development/testing.

#### Public Channels (NIP-28)

Gas Town creates NIP-28 channels for broadcast communication:

**Town-wide channels**:
- `town-ops` — Operational updates (boots, halts, health)
- `activity` — Public activity feed mirror
- `alerts` — Escalations and warnings
- `announcements` — Human operator messages

**Per-rig channels**:
- `<rig>-dev` — Development activity
- `<rig>-merge` — Merge queue status
- `<rig>-patrol` — Witness patrol reports

#### DM Commands

Agents accept commands via DM:

| Role | Commands |
|------|----------|
| Mayor | `status`, `convoy`, `escalate`, `broadcast` |
| Witness | `patrol`, `check`, `nudge`, `escalate` |
| Refinery | `queue`, `retry`, `skip`, `status` |
| Deacon | `health`, `drain`, `restart`, `sunset` |

Send a DM with content `help` to any agent to see its available commands.

### Work Queues

**Kind 30325** (`GT_WORK_ITEM`) events implement a distributed work queue:
- `PublishWorkItem()` — Submit work to a named queue
- `ClaimWorkItem()` — Claim a work item for processing
- `CompleteWorkItem()` — Mark work as completed
- `FailWorkItem()` — Mark work as failed with reason

### Group, Queue, and Channel Definitions

Administrative events define organizational structure:
- **Kind 30321** (`GT_GROUP_DEF`) — Group membership (e.g., "polecat-squad-alpha")
- **Kind 30322** (`GT_QUEUE_DEF`) — Work queue configuration (merge-queue, patrol-queue)
- **Kind 30323** (`GT_CHANNEL_DEF`) — Pub/sub channel definitions with retention policies

---

## Health & Monitoring

Check the health of your Nostr integration:

```bash
gt nostr health
```

This reports:
- Whether Nostr is enabled
- Connection status of each write and read relay
- Signer configuration status
- Number of events in the spool (pending delivery)
- Sunset flag status for each subsystem

Example output:
```
Nostr Status:
  Enabled: true
  Write Relay: wss://relay.gastown.example.com (connected)
  Read Relay: wss://relay.damus.io (connected)
  Signer: configured
  Spool: 0 events pending

Sunset Status:
  Events Local:  ON  (dual-write)
  Feed Curator:  ON  (dual-write)
  Convoy Local:  ON  (dual-write)
  Mail Local:    ON  (dual-write)
  Nudge Local:   ON  (dual-write)
```

---

## Sunset Flags & Migration

Gas Town follows a phased migration from local subsystems to Nostr equivalents. During migration, both paths run in parallel (**dual-write**). Each subsystem has a sunset flag:

| Flag | Subsystem | What it controls |
|------|-----------|-----------------|
| `GT_EVENTS_LOCAL` | Events | Writing to `.events.jsonl` |
| `GT_FEED_CURATOR` | Feed | Local feed curator daemon |
| `GT_CONVOY_LOCAL` | Convoy | Using `bd dep list` for convoy tracking |
| `GT_MAIL_LOCAL` | Mail | Beads-native mail routing |
| `GT_NUDGE_LOCAL` | Nudge | Tmux-based session nudging |

All flags default to `1` (ON). To sunset a subsystem and switch to Nostr-only:

```bash
# After validating the Nostr equivalent works:
export GT_EVENTS_LOCAL=0   # Stop writing to .events.jsonl
export GT_FEED_CURATOR=0   # Stop running local feed curator
```

### Recommended Migration Order

1. **Events** (`GT_EVENTS_LOCAL`) — Safest to sunset first since it's append-only
2. **Feed Curator** (`GT_FEED_CURATOR`) — Sunset once Flotilla reads from Nostr
3. **Mail** (`GT_MAIL_LOCAL`) — Sunset once DMs and channels are validated
4. **Nudge** (`GT_NUDGE_LOCAL`) — Sunset once API/MCP mode replaces tmux
5. **Convoy** (`GT_CONVOY_LOCAL`) — Sunset last (most critical for coordination)

---

## Spool & Offline Resilience

When relay connectivity fails, events are **spooled locally** to `~/gt/.runtime/nostr-spool.jsonl`. The spool provides:

- **Automatic spooling**: Events that fail to publish are saved with retry metadata
- **Exponential backoff**: Retries at 30s, 60s, 120s, then 300s intervals
- **Periodic draining**: The deacon periodically calls `DrainSpool()` to retry spooled events
- **Soft limit** (10,000 events): Warning logged when reached
- **Hard limit** (100,000 events): Spooling stops; requires operator intervention
- **Archiving**: Events older than 24 hours are moved to `nostr-spool-archive.jsonl`

Spool files use `0600` permissions (owner-only read/write).

---

## Security

### Key Management

- **Production**: Use NIP-46 external signers (bunkers). No private keys are stored on Gas Town machines.
- **Development**: The `LocalSigner` is available for testing but stores the private key in memory. **Never use in production**.

### Path Sandboxing

The agent loop executor sandboxes all file operations to the working directory:
- All paths are resolved, cleaned, and verified against the worktree root
- Symlinks are evaluated to prevent escape via symlink chains
- `filepath.Rel` is used for containment checks (immune to prefix-matching bypasses)

### Authentication

- MCP server connections use **Bearer token** authentication
- Empty auth token in development mode allows all connections (warning: not for production)

### DM Security

- NIP-17 gift-wrapped DMs provide end-to-end encryption (when fully implemented)
- The plaintext kind 4 fallback is **disabled by default** and must be explicitly opted into
- Never enable `AllowPlaintextFallback` in production

---

## Troubleshooting

### "Nostr is not enabled"

Ensure `GT_NOSTR_ENABLED=1` is set and the config file exists at the expected path.

### No events reaching relays

1. Check `gt nostr health` for relay connection status
2. Check the spool count — events might be queued for retry
3. Verify your NIP-46 bunker is running and accessible
4. Check relay logs for rejected events

### Spool growing indefinitely

The spool will grow if relays are persistently unreachable:
1. Check relay connectivity
2. If the relay is permanently gone, update `write_relays` in your config
3. If the hard limit is hit, manually clear `~/gt/.runtime/nostr-spool.jsonl`

### Agent heartbeats not appearing

1. Verify the agent has a configured identity in `.nostr.json`
2. Check that the NIP-46 bunker can sign events
3. Ensure the agent's role has a heartbeat publisher started

### MCP connection refused

1. Verify the MCP server is running on the expected port
2. Check firewall rules for LAN connections
3. Verify the auth token matches between client and server

---

## Example Configurations

See the `docs/examples/` directory for complete configuration examples:

- **[agents-api.json](examples/agents-api.json)** — Cloud API configurations (OpenAI, Anthropic, Ollama)
- **[agents-lan.json](examples/agents-lan.json)** — LAN GPU server configurations
- **[agents-mixed.json](examples/agents-mixed.json)** — Mixed CLI + API + MCP setup
- **[nostr-config.json](examples/nostr-config.json)** — Complete Nostr configuration with identities

---

## Related Documentation

- **[Nostr Protocol Specification](design/nostr-protocol.md)** — Detailed protocol spec (event kinds, tag conventions, content schemas)
- **[Architecture Overview](design/nostr-architecture.md)** — Developer guide to the Go packages
- **[Mail Protocol](design/mail-protocol.md)** — How mail routing maps to Nostr DMs and channels
- **[Glossary](glossary.md)** — Gas Town terminology reference
