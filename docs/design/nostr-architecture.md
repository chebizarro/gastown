# Nostr Integration Architecture

> Developer guide to the Go packages implementing Gas Town's Nostr-native capabilities.
>
> For the protocol specification (event kinds, tag schemas, migration phases), see [nostr-protocol.md](nostr-protocol.md).
> For user-facing setup and configuration, see [../NOSTR.md](../NOSTR.md).

---

## Package Overview

The Nostr integration spans five new packages and modifications to two existing packages:

```
internal/
├── nostr/           # Core Nostr layer (signing, publishing, relays, spool)
├── llm/             # LLM client abstraction (OpenAI, Anthropic)
├── agentloop/       # Go-native agent execution loop (API mode)
├── mcp/             # MCP tool server & transport (remote agents)
├── events/          # Activity feed (modified for dual-write)
└── config/          # Configuration (modified for provider types)
```

### Dependency Graph

```
config ──────────────────────────────────────────────┐
  │                                                   │
  ├── nostr (types, signer, client, publisher, spool) │
  │     │                                             │
  │     ├── events/nostr.go (dual-write bridge)       │
  │     ├── lifecycle.go (heartbeats)                 │
  │     ├── convoy.go, issues.go (state mirrors)      │
  │     ├── protocol.go (machine signals)             │
  │     ├── dm.go, channels.go (chat)                 │
  │     ├── workqueue.go, definitions.go              │
  │     ├── blossom.go (blob storage)                 │
  │     ├── health.go (monitoring)                    │
  │     └── commands.go (DM command router)           │
  │                                                   │
  ├── llm (client interface + implementations)        │
  │     │                                             │
  │     └── agentloop (think→act→observe loop)        │
  │           │                                       │
  │           └── mcp/server.go (tool server)         │
  │                                                   │
  └── mcp/transport.go, discovery.go (client side)    │
```

---

## Package: `internal/nostr`

The core Nostr package. All Nostr event construction, signing, relay management, and spooling flows through this package.

### Files

| File | Purpose | Key Types |
|------|---------|-----------|
| `types.go` | Event kind constants, tag builders, protocol constants | `Correlations`, `BaseTags()`, `SchemaVersion()` |
| `signer.go` | Event signing abstraction | `Signer` (interface), `NIP46Signer`, `LocalSigner` |
| `client.go` | Relay connection pool | `RelayPool` |
| `publisher.go` | High-level sign→broadcast→spool API | `Publisher` |
| `spool.go` | Local event store for offline resilience | `Spool`, `SpoolEntry` |
| `event.go` | Event construction helpers | `NewLogStatusEvent()`, `NewLifecycleEvent()`, etc. |
| `identity.go` | Per-agent keypair provisioning | `IdentityManager` |
| `registry.go` | Agent identity registry | `Registry` |
| `lifecycle.go` | Agent heartbeat publishing | `HeartbeatPublisher`, `PublishDeath()` |
| `convoy.go` | Convoy state publishing (kind 30318) | `ConvoyStateContent`, `PublishConvoyState()` |
| `issues.go` | Issue mirroring (kind 30319) | `BeadsIssueContent`, `PublishIssueMirror()` |
| `protocol.go` | Machine-to-machine signals (kind 30320) | `PublishProtocolEvent()`, `ProtocolEventRouter` |
| `workqueue.go` | Work queue items (kind 30325) | `PublishWorkItem()`, `ClaimWorkItem()` |
| `definitions.go` | Group/queue/channel definitions (30321-30323) | `PublishGroupDef()`, `PublishQueueDef()` |
| `dm.go` | NIP-17 encrypted DMs | `DMSender`, `DMListener` |
| `channels.go` | NIP-28 public channels | `CreateTownChannels()`, `PostChannelMessage()` |
| `commands.go` | DM command router | `CommandRouter`, `RegisterMayorCommands()` |
| `blossom.go` | Content-addressed blob uploads | `BlobUploader` |
| `health.go` | Health checks and sunset flags | `CheckHealth()`, `SunsetFlags` |

### Key Abstractions

#### Signer Interface

```go
type Signer interface {
    Sign(ctx context.Context, event *nostr.Event) error
    GetPublicKey() string
    Close() error
}
```

Two implementations:
- **`NIP46Signer`** — Connects to an external NIP-46 bunker. No private keys on disk. Production path.
- **`LocalSigner`** — In-memory private key. Development/testing only.

#### Publisher

The central publishing API. All Nostr event publishing should go through `Publisher`:

```go
publisher := nostr.NewPublisher(ctx, cfg, signer, runtimeDir)

// Sign → broadcast to relays → spool on failure
publisher.Publish(ctx, event)

// For NIP-33 replaceable events (requires "d" tag)
publisher.PublishReplaceable(ctx, event)

// Periodic spool drain (called by deacon)
sent, failed, err := publisher.DrainSpool(ctx)
```

The publish flow:
1. **Sign** the event using the Signer
2. **Broadcast** to all write relays
3. If ALL relays fail → **spool** locally for later retry
4. Return nil unless both publishing AND spooling fail

#### RelayPool

Manages connections to read and write relays:

```go
pool := nostr.NewRelayPool(ctx, cfg)

// Publishing (to write relays)
pool.Publish(ctx, event)

// Subscribing (from read relays)
subs := pool.Subscribe(ctx, filters)

// Health monitoring
pool.Reconnect(ctx)  // auto-reconnect disconnected relays
pool.HealthCheck()    // log connection status
```

#### Spool

Local JSONL event store for offline resilience:

```go
spool := nostr.NewSpool(runtimeDir)

// Events are spooled automatically by Publisher on relay failure
spool.Enqueue(event, targetRelays)

// Drain: attempt to send all spooled events
sent, failed, err := spool.Drain(ctx, pool)

// Maintenance
spool.ArchiveOld(24 * time.Hour)
spool.Count()
```

Exponential backoff: 30s → 60s → 120s → 300s cap.

### Event Construction

Each event kind has a constructor in `event.go`:

```go
// Kind 30315 - Activity feed
event, _ := nostr.NewLogStatusEvent(rig, role, actor, eventType, visibility, payload)

// Kind 30316 - Lifecycle
event, _ := nostr.NewLifecycleEvent(rig, role, actor, instanceID, action, payload)

// Kind 30318 - Convoy state
event, _ := nostr.NewConvoyStateEvent(rig, role, actor, convoyID, state)

// Kind 30319 - Issue mirror
event, _ := nostr.NewBeadsIssueStateEvent(rig, role, actor, issueID, issueData)

// Kind 30320 - Protocol event
event, _ := nostr.NewProtocolEvent(rig, role, actor, protocolType, payload)

// Kind 30325 - Work item
event, _ := nostr.NewWorkItemEvent(rig, role, actor, queueName, workItem)
```

Higher-level helpers exist in specialized files (e.g., `PublishConvoyState()`, `PublishIssueMirror()`) that handle both event construction and publishing.

### Correlations

Cross-reference data that becomes Nostr tags for filtering:

```go
type Correlations struct {
    IssueID   string  // → tag ["t", issueID]
    ConvoyID  string  // → tag ["convoy", convoyID]
    BeadID    string  // → tag ["bead", beadID]
    SessionID string  // → tag ["session", sessionID]
    Branch    string  // → tag ["branch", branch]
    MergeReq  string  // → tag ["mr", mrID]
}
```

Canonical definition in `types.go`. Used by `events/nostr.go` for dual-write correlation extraction and by `protocol.go` for protocol event tags.

---

## Package: `internal/llm`

Unified interface for calling language models. Decouples agent execution from specific LLM providers.

### Files

| File | Purpose |
|------|---------|
| `client.go` | `Client` interface, request/response types |
| `factory.go` | `NewClient()` factory dispatching by api_type |
| `openai.go` | OpenAI Chat Completions implementation |
| `anthropic.go` | Anthropic Messages API implementation |

### Client Interface

```go
type Client interface {
    Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
    Stream(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error)
    ModelInfo() *ModelInfo
    Ping(ctx context.Context) error
    Close() error
}
```

### Factory

```go
client, err := llm.NewClient(&config.APIConfig{
    APIType:   "anthropic",
    Model:     "claude-sonnet-4-20250514",
    APIKey:    "$ANTHROPIC_API_KEY",
    // BaseURL optional for Anthropic (defaults to api.anthropic.com)
})
```

Supports:
- **`openai`** — OpenAI-compatible (Ollama, vLLM, OpenAI, Azure). Requires `base_url`.
- **`anthropic`** — Anthropic Messages API. `base_url` optional.

API keys prefixed with `$` are resolved from environment variables.

### Wire Format Differences

The LLM package abstracts away provider-specific format differences:

| Concept | OpenAI | Anthropic |
|---------|--------|-----------|
| System message | In messages array | Top-level `system` field |
| Tool results | `role: "tool"` | `role: "user"` + `tool_result` content block |
| Tool calls | `tool_calls` array | `tool_use` content blocks |
| Stop reason | `finish_reason: "stop"` | `stop_reason: "end_turn"` |
| Auth header | `Authorization: Bearer` | `x-api-key` |

---

## Package: `internal/agentloop`

Go-native agent execution loop for API-mode agents. Replaces the tmux-based session management for agents that call LLM APIs directly.

### Files

| File | Purpose |
|------|---------|
| `loop.go` | Think→act→observe state machine |
| `executor.go` | Tool call execution with path sandboxing |
| `tools.go` | GT tool definitions (16 tools) |
| `context.go` | Context window management |

### Agent Loop

The core loop runs a think→act→observe cycle:

```go
loop := agentloop.NewAgentLoop(client, executor, &agentloop.AgentLoopConfig{
    SystemPrompt:     "You are a Gas Town polecat...",
    MaxIterations:    50,
    MaxTokensPerTask: 200000,
    IdleTimeout:      5 * time.Minute,
    ToolTimeout:      120 * time.Second,
    Role:             "polecat",
    RigName:          "my-rig",
    Actor:             "my-rig/polecats/Toast",
    OnHeartbeat:      heartbeatCallback,
    OnTaskComplete:   completionCallback,
})

loop.Start(ctx)          // Blocks; runs until stopped or context cancelled
loop.AssignWork(task)     // Queue work (replaces tmux NudgeSession)
loop.Stop()              // Graceful shutdown
```

State machine:
```
  ┌──── idle ◄───────────┐
  │       │               │
  │   AssignWork()        │
  │       │               │
  │       ▼               │
  │    working ───────────┘
  │       │            (task complete)
  │       │
  │   Stop()/ctx.Done()
  │       │
  └──► stopped
```

### Executor

Handles tool calls with path sandboxing:

```go
executor := agentloop.NewExecutor(
    workDir,   // git worktree path (sandbox root)
    rigName,
    rigPath,
    townRoot,
    actor,     // e.g., "my-rig/polecats/Toast"
    role,      // e.g., "polecat"
)

result, err := executor.Execute(ctx, toolCall)
```

**Security**: All file operations are restricted to `workDir` via `safePath()`:
- Resolves symlinks via `filepath.EvalSymlinks`
- Verifies containment via `filepath.Rel` (not string prefix matching)
- Rejects paths that escape (`..` prefix in relative path)

### Tool Definitions

16 tools available to agents:

| Tool | Category | Description |
|------|----------|-------------|
| `gt_prime` | GT | Get work assignment |
| `gt_done` | GT | Complete current work |
| `bd_show` | Beads | Show issue details |
| `bd_list` | Beads | List issues |
| `bd_update` | Beads | Update issue status |
| `git_diff` | Git | Show changes |
| `git_status` | Git | Show working tree status |
| `git_commit` | Git | Stage and commit |
| `file_read` | File | Read file contents |
| `file_write` | File | Create/overwrite file |
| `file_edit` | File | Search and replace in file |
| `file_list` | File | List directory contents |
| `file_search` | File | Grep for pattern |
| `shell_exec` | Shell | Execute arbitrary command |
| `gt_mail_send` | Mail | Send mail message |
| `gt_mail_read` | Mail | Read mailbox |

### Context Window Management

The `ContextManager` handles token budget:

```go
cm := agentloop.NewContextManager(contextWindow)

if cm.NeedsTruncation(messages) {
    messages = cm.Truncate(messages)
}
```

Truncation strategy:
1. Keep the system message (always first)
2. Keep the last 6 messages (recent context)
3. Summarize middle messages into a single condensed message
4. Token estimation: ~0.28 tokens per character

---

## Package: `internal/mcp`

MCP (Model Context Protocol) server and client transport for remote agent execution.

### Files

| File | Purpose |
|------|---------|
| `server.go` | HTTP server exposing GT tools |
| `transport.go` | Client transport abstraction (SSE) |
| `discovery.go` | LAN service discovery |

### MCP Server

Exposes GT tools to remote agents over HTTP:

```go
server := mcp.NewServer("127.0.0.1:9500", executor, authToken)
server.RegisterGTTools()  // Register all 16 GT tools
server.Start(ctx)
```

Endpoints:
| Endpoint | Method | Description |
|----------|--------|-------------|
| `/mcp/tools/list` | GET/POST | List available tools |
| `/mcp/tools/call` | POST | Execute a tool call |
| `/mcp/health` | GET | Server health status |
| `/mcp/sse` | GET | SSE stream (heartbeats) |

Authentication: Bearer token in `Authorization` header.

### Transport Client

Connect to a remote MCP server:

```go
transport := mcp.NewSSETransport(baseURL, authToken)
transport.Connect(ctx)

tools, _ := transport.ListTools(ctx)
result, _ := transport.CallTool(ctx, "file_read", argsJSON)

transport.Close()
```

### LAN Discovery

Discover MCP servers on the local network:

```go
discovery := &mcp.Discovery{}

// Probe a known host
info, _ := discovery.Probe(ctx, "gpu-server.local", "9500")

// Scan subnet
services, _ := discovery.ScanSubnet(ctx, "192.168.1", "9500")

// Check well-known locations
services, _ := discovery.ProbeKnownHosts(ctx)
```

---

## Package: `internal/events` (Modified)

### Dual-Write Bridge

`events/nostr.go` bridges the existing event system to Nostr:

```go
// In events.go write():
eventCopy := event
if event.Payload != nil {
    eventCopy.Payload = make(map[string]interface{}, len(event.Payload))
    for k, v := range event.Payload {
        eventCopy.Payload[k] = v
    }
}
go publishToNostr(eventCopy)  // async, non-blocking
```

Key design decisions:
- **Deep copy** of Payload map before spawning goroutine (prevents data race)
- **Global publisher singleton** initialized via `sync.Once`
- **Best-effort**: errors are logged, never returned to callers
- **Correlation extraction**: maps event types to Nostr tags for filtering
- **Actor parsing**: splits addresses like `"rig/polecats/Toast"` into (rig, role, actor) tuples

---

## Package: `internal/config` (Modified)

### Provider Types

```go
type ProviderType string

const (
    ProviderCLI ProviderType = "cli"  // default
    ProviderAPI ProviderType = "api"
    ProviderMCP ProviderType = "mcp"
)
```

### API & MCP Configuration

```go
type APIConfig struct {
    APIType        string            `json:"api_type"`
    BaseURL        string            `json:"base_url"`
    Model          string            `json:"model"`
    APIKey         string            `json:"api_key"`
    MaxTokens      int               `json:"max_tokens"`
    ContextWindow  int               `json:"context_window"`
    SupportsTools  bool              `json:"supports_tools"`
    SupportsVision bool              `json:"supports_vision"`
    Headers        map[string]string `json:"headers"`
    TimeoutSeconds int               `json:"timeout_seconds"`
}

type MCPConfig struct {
    Transport    string   `json:"transport"`     // "sse", "stdio", "ws"
    URL          string   `json:"url"`
    AuthToken    string   `json:"auth_token"`
    ExposedTools []string `json:"exposed_tools"` // tool whitelist
}
```

Both `AgentPresetInfo` and `RuntimeConfig` carry these fields for the full config lifecycle:
1. Preset defines defaults (e.g., "claude-api" preset with Anthropic config)
2. Agent config optionally overrides preset values
3. `RuntimeConfigFromPreset()` merges them into the final runtime config
4. `MergeWithPreset()` inherits provider type from preset if not set

---

## Adding New Event Kinds

To add a new Nostr event kind:

1. **Add the kind constant** to `internal/nostr/types.go`:
   ```go
   KindMyNewEvent = 30326
   ```

2. **Add an event constructor** to `internal/nostr/event.go`:
   ```go
   func NewMyEvent(rig, role, actor string, payload interface{}) (*nostr.Event, error) {
       tags := BaseTags(rig, role, actor)
       content, _ := json.Marshal(map[string]interface{}{
           "schema": SchemaVersion("my_event", 1),
           "data":   payload,
       })
       return &nostr.Event{
           CreatedAt: nostr.Timestamp(time.Now().Unix()),
           Kind:      KindMyNewEvent,
           Tags:      tags,
           Content:   string(content),
       }, nil
   }
   ```

3. **Add a publish helper** in a new or existing file:
   ```go
   func PublishMyEvent(ctx context.Context, publisher *Publisher, ...) error {
       event, err := NewMyEvent(...)
       if err != nil { return err }
       return publisher.Publish(ctx, event)
   }
   ```

4. **Update the protocol spec** in `docs/design/nostr-protocol.md`.

---

## Testing

### Running Tests

```bash
# Config package tests (Phase 1)
go test ./internal/config/ -v

# All packages
go test ./internal/... -v
```

### Test Utilities

- `events.ResetPublisherForTesting()` — Resets the Nostr publisher singleton
- `nostr.NewLocalSigner(privkeyHex)` — Creates a test signer without a bunker
- `nostr.NewSpool(tempDir)` — Creates a spool in a temp directory

### Integration Testing

For integration tests that publish to real relays:
1. Set up a local relay (e.g., [nostr-relay](https://github.com/fiatjaf/relay))
2. Create a test config pointing to `ws://localhost:7447`
3. Use `LocalSigner` for test signing

---

## Future Work

- **Full NIP-17 implementation**: NIP-44 encryption for DMs (currently falls back to plaintext kind 4)
- **Streaming support**: Anthropic SSE streaming in `llm/anthropic.go`
- **Session manager wiring**: Branch on `ProviderType` in `Start()` to use agent loop vs tmux
- **Test coverage**: Unit tests for all new packages
- **Flotilla extension**: Standalone Svelte/TypeScript extension for Nostr-powered dashboard
- **Cross-process spool locking**: File-level locks for multi-process spool access
