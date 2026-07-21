# Gas Town Nostr Protocol Specification

> Version: 0.4.0 — Draft
> Date: 2026-07-20
> Status: Canonical Cascadia alignment

Gas Town's Cascadia-facing Nostr surface is defined by the generated
`git.sharegap.net/cascadia/cascadia-go` bindings. This document is a local
implementation guide, not a separate event-kind registry. When this file and
`cascadia-go` disagree, `cascadia-go` wins.

The old Gas Town-only custom-kind block is retired. Do not introduce new
Gastown-local event kind literals for fleet-visible status, lifecycle,
capabilities, task state, queues, or mutations.

## Event Kinds

| Kind | Cascadia symbol | Purpose in Gas Town |
|---|---|---|
| `30315` | `NIP38_USER_STATUS` | Activity and operational status updates. |
| `30316` | `CAS_AGENT_HEARTBEAT` | Addressable latest-wins agent liveness. |
| `30317` | `CAS_AGENT_CAPABILITY` | Addressable agent capability descriptors. |
| `30900` | `CAS_CP_STATE` | Addressable control-plane state projections, including `task:<id>`. |
| `30000` | `NIP51_TASK_COLLECTION` | NIP-51 task queues and epic membership collections. |
| `25910` | `CAS_INTENT` | ContextVM JSON-RPC requests, responses, notifications, and task mutations. |

Standard Nostr kinds remain reused as-is for identity and chat:

| Kind | NIP | Purpose |
|---|---|---|
| `0` | NIP-01 | Agent profile metadata. |
| `9` | NIP-C7 | Lightweight public chat messages. |
| `14` / `15` | NIP-17 | Private direct messages and file messages. |
| `40` / `41` / `42` | NIP-28 | Channel create, metadata, and messages. |
| `1059` | NIP-59 | Gift wraps for encrypted payloads. |
| `10002` | NIP-65 | Relay lists. |
| `10050` | NIP-17 | DM relay preferences. |

## Common Tags

Gas Town preserves local routing metadata on canonical events:

```text
["gt", "1"]
["rig", "<rig-name>"]
["role", "<role>"]
["agent", "<agent-or-role>"]
["schema", "<payload-schema>"]
```

Optional correlation tags:

```text
["t", "<beads-issue-id>"]
["convoy", "<convoy-id>"]
["bead", "<bead-id>"]
["session", "<session-uuid>"]
```

Addressable events use a stable `d` tag. Task state uses the `task:<id>`
namespace under `30900`, while task collections use `queue:<id>` or `epic:<id>`
under NIP-51 `30000`.

## Status: 30315

`30315` publishes Gas Town activity updates using the canonical NIP-38 status
kind. The Go constructor is `internal/nostr.NewLogStatusEvent`.

Required local tags:

```text
["d", "cascadia:agent"]
["type", "<event-type>"]
["visibility", "audit|feed|both"]
```

Content is JSON:

```json
{
  "schema": "gt/log@1",
  "type": "hook",
  "source": "gt",
  "payload": {}
}
```

## Heartbeat: 30316

`30316` publishes agent liveness using
`cascadia.agent.heartbeat.v1`. The Go constructor is
`internal/nostr.NewAgentHeartbeatEvent`.

Required canonical tags:

```text
["d", "<rig>/<role>/<agent>"]
["status", "ready|working|busy|retiring|dead"]
["agent", "<role-or-agent>"]
["runtime", "gastown"]
["schema", "cascadia.agent.heartbeat.v1"]
```

## Capability: 30317

`30317` publishes one addressable record per agent capability. The Go
constructor is `internal/nostr.NewAgentCapabilityEvent`.

Required canonical tags:

```text
["d", "agent:<agent-id>:cap:<capability>"]
["agent", "<agent-id>"]
["cap", "<capability>"]
["runtime", "gastown"]
["schema", "cascadia.agent.capability.v1"]
```

## Task State: 30900

`30900` publishes complete current task state. The Go constructor is
`internal/nostr.NewTaskStateEvent`.

Required canonical tags:

```text
["d", "task:<task-id>"]
["domain", "task"]
["schema", "cascadia.task-state.v1"]
```

Content uses `cascadia.CascadiaTaskStateV1Payload`, including `id`, `title`,
`status`, and `priority`.

## Task Collections: NIP-51 30000

Task queues and epic membership use NIP-51 named lists rather than custom queue
or work-item kinds. The Go constructor is `internal/nostr.NewTaskQueueEvent`.

Required tags:

```text
["d", "queue:<queue-id>"]
["schema", "cascadia.task-queue.v1"]
["a", "30900:<task-author-pubkey>:task:<task-id>"]
```

If the collection belongs to a NIP-29 group, include:

```text
["h", "<group-id>"]
```

The `a` tag is the NIP-33 coordinate for the task state event.

## Task Mutations: 25910

Task mutations use ContextVM JSON-RPC envelopes on `25910`. The Go constructor
is `internal/nostr.NewContextVMIntentEvent`.

Required canonical tags:

```text
["p", "<recipient-pubkey>"]
["method", "<contextvm-method>"]
["schema", "contextvm.intent.v1"]
```

Content is a JSON-RPC 2.0 envelope:

```json
{
  "jsonrpc": "2.0",
  "id": "req-1",
  "method": "task/update",
  "params": { "id": "fp-106" }
}
```

## Filter Patterns

```json
{ "kinds": [30315], "#gt": ["1"], "since": 0 }
```

```json
{ "kinds": [30316], "#gt": ["1"] }
```

```json
{ "kinds": [30317], "#runtime": ["gastown"] }
```

```json
{ "kinds": [30900], "#domain": ["task"] }
```

```json
{ "kinds": [30000], "#d": ["queue:merge"] }
```

```json
{ "kinds": [25910], "#p": ["<recipient-pubkey>"] }
```

## Verification

Consumers should verify:

1. The Nostr event signature is valid.
2. The event kind is one of the generated `cascadia-go` constants for
   fleet-visible Gas Town state.
3. Required tags pass `cascadia.ValidateRequiredTags`.
4. JSON content validates against the matching generated payload validator.
5. NIP-51 `a` tags use valid `kind:pubkey:d-tag` coordinates.

## Migration Notes

Historical migration fixtures may still mention old proposals. Active source,
active design docs, and new examples must use the canonical table above.
