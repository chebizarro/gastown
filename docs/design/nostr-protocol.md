# Gas Town Nostr Protocol Specification

> Version: 0.3.0 — Draft
> Date: 2026-02-13
> Status: Proposed
>
> **Changelog**:
> - v0.3.0: Flotilla integration refactored to standalone extension (`gastown-flotilla-extension`); Go Nostr dependency changed from `github.com/nbd-wtf/go-nostr` to `fiatjaf.com/nostr`; added import mapping table; added Phase 10 (extension development)
> - v0.2.0: Reclassified mail as NIP-17 DMs + NIP-28 channels; added Agent Identity & Interaction section; added Channel Architecture; renamed 30320 from GT_MAIL_MESSAGE to GT_PROTOCOL_EVENT; added 30325 GT_WORK_ITEM; expanded migration to 9 phases
> - v0.1.0: Initial draft with custom kind for all mail

## Overview

This document defines how Gas Town operates as a **Nostr-native orchestration layer**. Instead of relying exclusively on local `.events.jsonl` files, beads mail routing, and `.runtime` state files, Gas Town publishes structured Nostr events to relays. This enables:

- **Multi-observer visibility**: Flotilla (via the standalone `gastown-flotilla-extension`), CLIs, and other agents can subscribe to Gas Town state without filesystem access.
- **Cross-host coordination**: Sessions, convoys, and agents are discoverable across machines.
- **Beads→Nostr bridge**: Beads issues are mirrored as replaceable Nostr events for UI consumption.
- **AI-Hub alignment**: Event kinds and tag conventions follow the [AI-Hub Compendium](../../../ai-hub/docs/compendium/05_data_flow_and_nostr_events.md).

### Architectural Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| **Authority for replaceable state** | Deacon pubkey | Single canonical publisher avoids NIP-33 conflicts; Deacon is the always-on supervisor |
| **Key management** | NIP-46 external signer (bunker) | No nsec on disk; agents reference bunker connection strings |
| **Issue payload size** | Full dependency graph | Complete data for offline-capable UI; heavy blobs offloaded to Blossom servers |
| **Offline/relay outage** | Spool to local event store | Events are persisted locally and drained to relays when connectivity returns |

### Design Principles

1. **Dual-write first**: All Nostr publishing coexists with local JSONL until the migration is validated.
2. **Feature-flagged**: `GT_NOSTR_ENABLED=1` activates Nostr publishing; default is off during rollout.
3. **Idempotent consumers**: All event handlers must tolerate duplicates, redelivery, and out-of-order arrival.
4. **Replaceable for state, append-only for logs**: Use NIP-33 parameterized replaceable events for "latest state" queries; use regular events for activity streams.
5. **Blossom for blobs**: Large binary artifacts (patches, diffs, screenshots) are stored on Blossom servers and referenced by URL + hash in Nostr events.

---

## Event Kind Allocation

### Reused from AI-Hub

| Kind | Name | NIP-33 | Purpose in Gas Town |
|------|------|--------|---------------------|
| **30315** | `LOG_STATUS` | Replaceable (optional `d`) | Activity feed — replaces `.events.jsonl` |
| **30316** | `LIFECYCLE` | Replaceable (`d`=instance) | Agent register/heartbeat/retire/dead |
| **38383** | `TASK` | Regular | Work assignment and queue claims |
| **38384** | `CONTROL` | Regular | Spawn/kill/scale/refresh commands |
| **38385** | `MCP_CALL` | Regular | MCP tool call requests |
| **38386** | `MCP_RESULT` | Regular | MCP tool call results |

### Gas Town Additions

| Kind | Name | NIP-33 | Purpose |
|------|------|--------|---------|
| **30318** | `GT_CONVOY_STATE` | Replaceable (`d`=convoy-id) | Convoy definition and progress |
| **30319** | `GT_BEADS_ISSUE_STATE` | Replaceable (`d`=issue-id) | Beads issue mirror for UI |
| **30320** | `GT_PROTOCOL_EVENT` | Regular | Machine-to-machine protocol events (MERGE_READY, POLECAT_DONE, etc.) |
| **30321** | `GT_GROUP_DEF` | Replaceable (`d`=group-name) | Group membership definition |
| **30322** | `GT_QUEUE_DEF` | Replaceable (`d`=queue-name) | Work queue definition and status |
| **30323** | `GT_CHANNEL_DEF` | Replaceable (`d`=channel-name) | Pub/sub channel definition |
| **30325** | `GT_WORK_ITEM` | Regular | Queue work items (claimable tasks) |

### Standard Nostr Kinds (Reused As-Is)

Gas Town leverages existing Nostr messaging NIPs rather than inventing custom kinds:

| Kind | NIP | Purpose in Gas Town |
|------|-----|---------------------|
| **0** | NIP-01 | Agent profile metadata (name, picture, role) |
| **9** | NIP-C7 | Public chat messages (lightweight channel chat) |
| **14** | NIP-17 | Private DMs between overseer ↔ agents (sealed + gift-wrapped) |
| **15** | NIP-17 | Private file messages (encrypted file sharing) |
| **40** | NIP-28 | Channel creation (town/rig channels) |
| **41** | NIP-28 | Channel metadata updates |
| **42** | NIP-28 | Channel messages (agent chat in channels) |
| **1059** | NIP-59 | Gift wraps for NIP-17 DMs |
| **10002** | NIP-65 | Agent relay list |
| **10050** | NIP-17 | Agent DM relay preferences |

### Communication Model Summary

| Communication Pattern | Old (Beads) | New (Nostr) |
|----------------------|-------------|-------------|
| Human ↔ Agent conversation | `gt mail send mayor/ -s "..."` | **NIP-17 DM** (kind 14 gift-wrapped) |
| Rig-wide / town-wide broadcast | `gt mail send @town -s "..."` | **NIP-28 channel** (kind 42) |
| Channel discussion | `gt mail send channel:ops -s "..."` | **NIP-28 channel** (kind 42) |
| Machine protocol (MERGE_READY, etc.) | `gt mail send` with structured body | **Custom kind 30320** (`GT_PROTOCOL_EVENT`) |
| Session handoff | `gt handoff` (mail to self) | **NIP-17 DM** to own/successor pubkey |
| Work queue items | `gt mail send queue:build -s "..."` | **Custom kind 30325** (`GT_WORK_ITEM`) |

---

## Common Tag Schema

All Gas Town events include these base tags for filtering and routing:

```
["gt", "1"]                          # Protocol version marker (required on all GT events)
["rig", "<rig-name>"]               # Source rig (e.g., "gastown", "beads")
["role", "<role>"]                   # Agent role (mayor|deacon|witness|refinery|crew|polecat|dog)
["actor", "<rig>/<role>/<name>"]    # Full actor identity (e.g., "gastown/polecats/Toast")
```

### Correlation Tags

```
["t", "<beads-issue-id>"]           # Task/issue correlation (reuse AI-Hub #t convention)
["convoy", "<hq-cv-id>"]            # Convoy correlation
["bead", "<bead-id>"]               # Generic bead reference
["session", "<session-uuid>"]       # Session correlation
```

### Replaceable State Tags

```
["d", "<stable-id>"]                # NIP-33 deduplication key (kind-specific)
```

### Content Schema Versioning

All event `content` fields are JSON objects with a `schema` field:

```json
{
  "schema": "gt/<schema-name>@<version>",
  ...
}
```

---

## Event Specifications

### 30315 — LOG_STATUS (Activity Feed)

Replaces `.events.jsonl` writes. Each log entry becomes a Nostr event.

**Publisher**: Any Gas Town agent (each signs with their own identity or delegates to Deacon).

**Tags**:
```
["gt", "1"]
["type", "<event-type>"]            # See type mapping below
["rig", "<rig>"]
["role", "<role>"]
["actor", "<actor-identity>"]
["visibility", "audit|feed|both"]   # Matches existing visibility semantics
```

Optional correlation tags (when applicable):
```
["t", "<issue-id>"]
["convoy", "<convoy-id>"]
["bead", "<bead-id>"]
["session", "<session-uuid>"]
["branch", "<branch-name>"]
["mr", "<merge-request-id>"]
```

**Content** (JSON):
```json
{
  "schema": "gt/log@1",
  "type": "<event-type>",
  "source": "gt",
  "payload": { ... }
}
```

#### Event Type Mapping

Every `events.Type*` constant maps to a 30315 event with specific tags and payload:

| Gastown Type | `type` tag | Additional Tags | Payload Fields |
|---|---|---|---|
| `TypeSling` | `sling` | `["t", bead]`, `["target", target]` | `{bead, target}` |
| `TypeHook` | `hook` | `["t", bead]` | `{bead}` |
| `TypeUnhook` | `unhook` | `["t", bead]` | `{bead}` |
| `TypeHandoff` | `handoff` | `["session", ...]` | `{to_session, subject?}` |
| `TypeDone` | `done` | `["t", bead]`, `["branch", branch]` | `{bead, branch}` |
| `TypeMail` | `mail` | — | `{to, subject}` |
| `TypeSpawn` | `spawn` | — | `{rig, polecat}` |
| `TypeKill` | `kill` | — | `{rig, target, reason}` |
| `TypeNudge` | `nudge` | — | `{rig, target, reason}` |
| `TypeBoot` | `boot` | — | `{rig, agents[]}` |
| `TypeHalt` | `halt` | — | `{services[]}` |
| `TypeSessionStart` | `session_start` | `["session", id]` | `{session_id, role, topic?, cwd?}` |
| `TypeSessionEnd` | `session_end` | `["session", id]` | `{session_id, role}` |
| `TypeSessionDeath` | `session_death` | `["session", name]` | `{session, agent, reason, caller}` |
| `TypeMassDeath` | `mass_death` | — | `{count, window, sessions[], possible_cause?}` |
| `TypePatrolStarted` | `patrol_started` | — | `{rig, polecat_count, message?}` |
| `TypePolecatChecked` | `polecat_checked` | — | `{rig, polecat, status, issue?}` |
| `TypePolecatNudged` | `polecat_nudged` | — | `{rig, target, reason}` |
| `TypeEscalationSent` | `escalation_sent` | — | `{rig, target, to, reason}` |
| `TypeEscalationAcked` | `escalation_acked` | — | `{rig, target, to, reason}` |
| `TypeEscalationClosed` | `escalation_closed` | — | `{rig, target, to, reason}` |
| `TypePatrolComplete` | `patrol_complete` | — | `{rig, polecat_count, message?}` |
| `TypeMergeStarted` | `merge_started` | `["mr", mr]`, `["branch", branch]` | `{mr, worker, branch, reason?}` |
| `TypeMerged` | `merged` | `["mr", mr]`, `["branch", branch]` | `{mr, worker, branch}` |
| `TypeMergeFailed` | `merge_failed` | `["mr", mr]`, `["branch", branch]` | `{mr, worker, branch, reason}` |
| `TypeMergeSkipped` | `merge_skipped` | `["mr", mr]`, `["branch", branch]` | `{mr, worker, branch, reason}` |

#### Retention

- **Relay-side**: Request 30-day minimum retention on write relays.
- **Client-side**: Use `since`/`until` windowing for backfill; retain local cache for offline access.
- **Spool**: On publish failure, persist to `~/gt/.runtime/nostr-spool.jsonl` for deferred delivery.

---

### 30316 — LIFECYCLE (Agent Presence)

Publishes agent instance state so UIs can show "who is running" and detect stale/dead agents.

**Publisher**: Deacon (canonical authority). Individual agents may publish their own heartbeats, but Deacon publishes the authoritative register/retire/dead transitions.

**Replaceable key**: `d` = `<rig>/<role>/<instance>` (e.g., `gastown/polecats/Toast` or `deacon`)

**Tags**:
```
["gt", "1"]
["d", "<rig>/<role>/<instance>"]
["rig", "<rig>"]
["role", "<role>"]                   # mayor|deacon|witness|refinery|crew|polecat|dog
["actor", "<rig>/<role>/<name>"]
["instance", "<tmux-session-or-uuid>"]
["status", "<status>"]              # ready|busy|retiring|dead
```

Optional:
```
["addr", "<host-identifier>"]       # Machine/host for multi-host setups
["t", "<current-issue>"]            # What the agent is working on (if any)
["model", "<llm-model>"]            # For A/B evaluation tracking
```

**Content** (JSON):
```json
{
  "schema": "gt/lifecycle@1",
  "status": "ready",
  "role": "polecat",
  "rig": "gastown",
  "instance": "Toast",
  "cwd": "/Users/dev/gt/gastown/polecats/Toast",
  "started_at": "2026-02-13T10:00:00Z",
  "last_heartbeat": "2026-02-13T10:05:00Z",
  "current_issue": "gt-abc",
  "model": "claude-sonnet-4"
}
```

#### Lifecycle State Machine

```
           ┌─────────┐
           │  ready   │◀──── heartbeat (periodic)
           └────┬─────┘
                │ work assigned
                ▼
           ┌─────────┐
           │  busy    │◀──── heartbeat (periodic, with issue tag)
           └────┬─────┘
                │ work complete / session ending
                ▼
           ┌──────────┐
           │ retiring  │───── graceful shutdown
           └────┬──────┘
                │
                ▼
           ┌─────────┐
           │  dead    │───── terminal (Deacon publishes on crash detection)
           └─────────┘
```

#### Heartbeat Interval

- Agents: every 60 seconds (configurable via `GT_NOSTR_HEARTBEAT_INTERVAL`)
- Deacon: every 30 seconds (supervisor heartbeat is more critical)
- Stale threshold: 3× heartbeat interval (agent considered stale if no update)

---

### 30318 — GT_CONVOY_STATE (Convoy Progress)

Replaces `bd dep list` + `gt convoy check` shell-outs with a directly queryable replaceable event.

**Publisher**: Deacon (canonical authority).

**Replaceable key**: `d` = `<convoy-id>` (e.g., `hq-cv-abc`)

**Tags**:
```
["gt", "1"]
["d", "<convoy-id>"]
["status", "open|landed|cancelled"]
```

Per tracked issue (repeated):
```
["t", "<issue-id>"]                  # Each tracked issue gets its own tag
```

Notification targets (repeated):
```
["notify", "<actor-or-pubkey>"]
```

**Content** (JSON):
```json
{
  "schema": "gt/convoy_state@1",
  "id": "hq-cv-abc",
  "title": "Feature X Rollout",
  "status": "open",
  "created_at": "2026-02-13T09:00:00Z",
  "created_by": "gastown/crew/max",
  "tracked_issues": [
    {
      "id": "gt-abc",
      "title": "Add auth module",
      "status": "in_progress",
      "assignee": "gastown/polecats/Toast",
      "rig": "gastown"
    },
    {
      "id": "gt-def",
      "title": "Update API docs",
      "status": "closed",
      "assignee": "gastown/polecats/Ash",
      "rig": "gastown"
    }
  ],
  "summary": {
    "total": 2,
    "open": 1,
    "closed": 1,
    "blocked": 0
  },
  "active_workers": ["gastown/polecats/Toast"],
  "landed": false,
  "landed_at": null,
  "last_updated": "2026-02-13T10:30:00Z"
}
```

#### Convoy State Update Triggers

The Deacon recomputes and publishes 30318 when:
1. A tracked issue's status changes (detected via 30319 issue mirror events)
2. A `gt convoy check` is explicitly invoked
3. A `38384` control event with `cmd=convoy_refresh` is received
4. Periodic recomputation (every 5 minutes as catch-all)

---

### 30319 — GT_BEADS_ISSUE_STATE (Issue Mirror)

Mirrors beads issues into Nostr so Flotilla and other UIs can render issue data without running `bd`.

**Publisher**: Deacon (canonical authority). Publishes snapshots when issues change.

**Replaceable key**: `d` = `<issue-id>` (e.g., `gt-abc`, `bd-xyz`, `hq-cv-abc`)

**Tags**:
```
["gt", "1"]
["d", "<issue-id>"]
["rig", "<rig>"]                     # Derived from issue prefix routing
["status", "<open|in_progress|closed|blocked>"]
["type", "<issue|task|bug|epic|convoy|message>"]
["priority", "<critical|high|medium|low>"]
```

Optional:
```
["assignee", "<actor>"]
["parent", "<parent-issue-id>"]
["label", "<label>"]                 # Repeated for each label
["convoy", "<convoy-id>"]            # If tracked by a convoy
```

**Content** (JSON):
```json
{
  "schema": "gt/beads_issue_state@1",
  "id": "gt-abc",
  "title": "Add authentication module",
  "status": "in_progress",
  "priority": "high",
  "type": "task",
  "created_at": "2026-02-12T14:00:00Z",
  "created_by": "gastown/crew/max",
  "updated_at": "2026-02-13T09:30:00Z",
  "assignee": "gastown/polecats/Toast",
  "labels": ["auth", "security"],
  "rig": "gastown",
  "dependencies": {
    "blocked_by": [],
    "blocks": ["gt-def"],
    "children": ["gt-abc-1", "gt-abc-2"],
    "parent": null
  },
  "branch": "feature/auth-module",
  "molecule": {
    "id": "mol-auth",
    "status": "bonded",
    "wisp_count": 3,
    "wisps_completed": 1
  },
  "blobs": [
    {
      "type": "patch",
      "url": "https://blossom.example.com/abc123.patch",
      "sha256": "abc123...",
      "size": 14520
    }
  ],
  "source": {
    "repo": "https://github.com/org/gastown",
    "nip34_event": null
  }
}
```

#### Blossom Blob Offloading

When issue state includes large artifacts (patches, diffs, screenshots, CI logs):
1. Upload blob to configured Blossom server(s)
2. Include `blobs[]` array in the event content with `url`, `sha256`, and `size`
3. Consumers fetch blobs on-demand from Blossom
4. Blossom URLs are stable (content-addressed by hash)

#### Mirror Update Triggers

1. **In-process hooks**: When `internal/beads/beads.go` wrapper performs CRUD, emit 30319
2. **Background poll**: Deacon daemon periodically runs `bd list --json` and diffs against last-known state
3. **Nostrig import**: When `nostrig fetch` imports NIP-34 repo events into beads, publish corresponding 30319 mirrors

---

### 30320 — GT_PROTOCOL_EVENT (Machine-to-Machine Protocol)

Structured protocol events for agent coordination. These are **not** conversational messages — they are machine-readable commands and notifications consumed programmatically by agent patrol loops. Conversational messaging uses NIP-17 DMs and NIP-28 channels instead (see [Agent Interaction](#agent-identity--interaction) below).

**Publisher**: Sending agent (signs with its own keypair).

**Tags**:
```
["gt", "1"]
["msg_type", "<POLECAT_DONE|MERGE_READY|MERGED|MERGE_FAILED|REWORK_REQUEST|HELP>"]
["from", "<sender-actor>"]
["to", "<recipient-actor>"]
["rig", "<rig>"]
```

Optional correlation:
```
["t", "<issue-id>"]
["branch", "<branch>"]
["mr", "<merge-request-id>"]
["polecat", "<polecat-name>"]
["convoy", "<convoy-id>"]
```

**Content** (JSON):
```json
{
  "schema": "gt/protocol@1",
  "msg_type": "MERGE_READY",
  "body": {
    "branch": "feature/auth-module",
    "issue": "gt-abc",
    "polecat": "Toast",
    "verified": "clean git state, issue closed"
  }
}
```

#### Protocol Event Type Mapping

| Protocol Type | `msg_type` tag | Route | Key body fields |
|---------------|---------------|-------|-----------------|
| POLECAT_DONE | `POLECAT_DONE` | Polecat → Witness | `exit`, `issue`, `mr?`, `branch` |
| MERGE_READY | `MERGE_READY` | Witness → Refinery | `branch`, `issue`, `polecat`, `verified` |
| MERGED | `MERGED` | Refinery → Witness | `branch`, `issue`, `polecat`, `rig`, `target`, `merged_at`, `merge_commit` |
| MERGE_FAILED | `MERGE_FAILED` | Refinery → Witness | `branch`, `issue`, `polecat`, `rig`, `target`, `failed_at`, `failure_type`, `error` |
| REWORK_REQUEST | `REWORK_REQUEST` | Refinery → Witness | `branch`, `issue`, `polecat`, `rig`, `target`, `requested_at`, `conflict_files[]` |
| HELP | `HELP` | Any → Mayor | `agent`, `issue?`, `problem`, `tried` |

> **Note**: `HANDOFF` and `WITNESS_PING` are no longer protocol events. Handoffs use NIP-17 DMs to the agent's own pubkey (or successor). Witness health checks use 30316 lifecycle events — no ping/pong needed.

#### Protocol Event Consumption

Agents subscribe to protocol events addressed to them:
```python
# Witness subscribes to protocol events
{"kinds": [30320], "#gt": ["1"], "#to": ["gastown/witness"]}

# Refinery subscribes to merge-related protocol
{"kinds": [30320], "#gt": ["1"], "#to": ["gastown/refinery"], "#msg_type": ["MERGE_READY"]}
```

These events are processed in the agent's patrol loop — they never appear in chat UI. The Flotilla `#activity` channel may surface protocol events as human-readable summaries for observability.

---

### 30325 — GT_WORK_ITEM (Queue Work Items)

Replaces beads-native queue messages with claimable Nostr events. A work item is a task posted to a named queue for any eligible worker to claim.

**Publisher**: Any agent posting work.

**Tags**:
```
["gt", "1"]
["queue", "<queue-name>"]
["from", "<sender-actor>"]
["status", "available|claimed|completed|failed"]
["rig", "<rig>"]
```

Optional:
```
["claimed_by", "<actor>"]
["t", "<issue-id>"]
["priority", "urgent|high|normal|low"]
```

**Content** (JSON):
```json
{
  "schema": "gt/work_item@1",
  "queue": "merge-queue",
  "subject": "Merge feature/auth-module",
  "body": {
    "branch": "feature/auth-module",
    "issue": "gt-abc",
    "polecat": "Toast"
  },
  "claimed_by": null,
  "claimed_at": null
}
```

Workers claim items by publishing a replacement event with `status=claimed` and their identity in `claimed_by`. Queue definitions (30322) control concurrency and ordering.

---

### 30321 — GT_GROUP_DEF (Group Definition)

Replaces `hq-group-*` beads with replaceable Nostr events.

**Publisher**: Deacon (canonical authority).

**Replaceable key**: `d` = `<group-name>`

**Tags**:
```
["gt", "1"]
["d", "<group-name>"]
["status", "active|deleted"]
```

Member tags (repeated):
```
["member", "<address-or-pattern>"]
```

**Content** (JSON):
```json
{
  "schema": "gt/group_def@1",
  "name": "ops-team",
  "members": [
    "gastown/witness",
    "gastown/crew/max",
    "deacon/"
  ],
  "created_by": "gastown/crew/max",
  "created_at": "2026-02-10T12:00:00Z",
  "updated_at": "2026-02-13T08:00:00Z"
}
```

---

### 30322 — GT_QUEUE_DEF (Work Queue Definition)

Replaces `hq-q-*` / `gt-q-*` beads with replaceable Nostr events.

**Publisher**: Deacon (canonical authority).

**Replaceable key**: `d` = `<queue-name>`

**Tags**:
```
["gt", "1"]
["d", "<queue-name>"]
["status", "active|paused|closed"]
["scope", "town|rig"]
```

**Content** (JSON):
```json
{
  "schema": "gt/queue_def@1",
  "name": "merge-queue",
  "status": "active",
  "scope": "rig",
  "rig": "gastown",
  "max_concurrency": 1,
  "processing_order": "fifo",
  "counts": {
    "available": 3,
    "processing": 1,
    "completed": 47,
    "failed": 2
  },
  "created_at": "2026-02-01T00:00:00Z",
  "updated_at": "2026-02-13T10:00:00Z"
}
```

---

### 30323 — GT_CHANNEL_DEF (Pub/Sub Channel Definition)

Replaces `hq-channel-*` beads with replaceable Nostr events.

**Publisher**: Deacon (canonical authority).

**Replaceable key**: `d` = `<channel-name>`

**Tags**:
```
["gt", "1"]
["d", "<channel-name>"]
["status", "active|closed"]
```

Subscriber tags (repeated):
```
["subscriber", "<address>"]
```

**Content** (JSON):
```json
{
  "schema": "gt/channel_def@1",
  "name": "alerts",
  "status": "active",
  "retention": {
    "count": 100,
    "hours": 0
  },
  "subscribers": [
    "gastown/witness",
    "deacon/"
  ],
  "created_by": "gastown/crew/max",
  "created_at": "2026-02-01T00:00:00Z"
}
```

---

## Agent Identity & Interaction

This section defines how agents exist as first-class Nostr identities and how the overseer (human operator) interacts with them through Flotilla's Discord-like chat interface.

### Design Principle: Agents Are Nostr Citizens

Every Gas Town agent — Mayor, Deacon, Witness, Refinery, Crew, Polecats — gets a full Nostr identity. They publish profiles, join channels, send and receive DMs. From Flotilla's perspective, an agent is indistinguishable from a human user (except for a `bot` flag in their profile).

### Per-Agent Keypair Provisioning

The **Deacon** manages keypair lifecycle for all agents via NIP-46 (the external signer decision from Phase 1).

#### Provisioning Flow

```
┌──────────────┐           ┌───────────────┐           ┌──────────────┐
│  gt spawn    │           │    Deacon      │           │  NIP-46      │
│  (new agent) │──────────>│  (key manager) │──────────>│  Bunker      │
│              │           │                │           │              │
│              │           │  1. Request    │           │  2. Generate │
│              │           │     keypair    │           │     keypair  │
│              │           │                │◀──────────│              │
│              │           │  3. Store      │           │  Returns     │
│              │◀──────────│     bunker URI │           │  pubkey +    │
│              │           │     in agent   │           │  bunker URI  │
│  Receives    │           │     config     │           │              │
│  bunker URI  │           │                │           │              │
│  via env     │           │  4. Publish    │           │              │
│              │           │     kind 0     │           │              │
│              │           │     profile    │           │              │
└──────────────┘           └───────────────┘           └──────────────┘
```

#### Agent Profile (Kind 0)

Each agent publishes a NIP-01 profile event:

```json
{
  "kind": 0,
  "pubkey": "<agent-pubkey>",
  "content": "{\"name\":\"Mayor\",\"display_name\":\"Gas Town Mayor\",\"about\":\"Global coordinator for Gas Town. Handles cross-rig communication and escalations.\",\"picture\":\"https://blossom.example.com/mayor-avatar.png\",\"nip05\":\"mayor@gastown.example.com\",\"bot\":true,\"lud16\":null}",
  "tags": [
    ["gt", "1"],
    ["role", "mayor"],
    ["rig", ""],
    ["actor", "mayor/"]
  ]
}
```

Key profile fields:
- `bot: true` — Signals to Flotilla/clients that this is an AI agent
- `role` tag — Gas Town role for filtering
- `actor` tag — Maps to the Gas Town address system (e.g., `gastown/polecats/Toast`)

#### Agent Relay Lists

Each agent publishes relay preferences:

**Kind 10002** (NIP-65 relay list):
```json
{
  "kind": 10002,
  "tags": [
    ["r", "wss://relay.gastown.example.com"],
    ["r", "wss://relay.damus.io", "read"]
  ]
}
```

**Kind 10050** (NIP-17 DM relay preferences):
```json
{
  "kind": 10050,
  "tags": [
    ["relay", "wss://relay.gastown.example.com"],
    ["relay", "wss://dm-inbox.gastown.example.com"]
  ]
}
```

#### Identity Registry

The Deacon maintains a mapping of Gas Town addresses → Nostr pubkeys in town-level beads:

```json
{
  "schema": "gt/identity_registry@1",
  "agents": {
    "mayor/": {
      "pubkey": "npub1mayor...",
      "bunker": "bunker://npub1...?relay=wss://bunker.example.com",
      "status": "active",
      "provisioned_at": "2026-02-13T10:00:00Z"
    },
    "deacon/": {
      "pubkey": "npub1deacon...",
      "bunker": "bunker://npub1...?relay=wss://bunker.example.com",
      "status": "active",
      "provisioned_at": "2026-02-13T10:00:00Z"
    },
    "gastown/witness": {
      "pubkey": "npub1witness...",
      "bunker": "bunker://...",
      "status": "active",
      "provisioned_at": "2026-02-13T10:01:00Z"
    },
    "gastown/polecats/Toast": {
      "pubkey": "npub1toast...",
      "bunker": "bunker://...",
      "status": "active",
      "provisioned_at": "2026-02-13T10:02:00Z"
    }
  }
}
```

This registry is also published as a replaceable Nostr event (kind 30316 with `d=identity_registry`) so Flotilla can resolve Gas Town addresses to pubkeys without filesystem access.

### DM Interaction (NIP-17)

The primary way humans interact with specific agents. Uses NIP-17's encrypted gift-wrap scheme.

#### Overseer → Agent Command

When you type a message to the Mayor in Flotilla's DM view:

```
You (DM to Mayor): Assign Toast to gt-123
```

Flotilla constructs:
1. An unsigned **kind 14** event (the actual message)
2. A **kind 13** seal (NIP-44 encrypted with your privkey + Mayor's pubkey)
3. A **kind 1059** gift wrap (random key, sent to Mayor's 10050 relays)

The Mayor's Go process subscribes to gift wraps addressed to its pubkey, unwraps them, and processes the command:

```
┌─────────────┐     kind 1059      ┌──────────────┐
│  Flotilla   │  ────────────────► │  Mayor Agent │
│  (You)      │  (gift-wrapped DM) │  (Go process)│
│             │                     │              │
│             │     kind 1059      │  Parse cmd   │
│             │  ◄──────────────── │  Execute gt  │
│             │  (gift-wrapped     │  Reply via   │
│             │   reply)           │  NIP-17 DM   │
└─────────────┘                     └──────────────┘
```

#### Agent → Overseer Notification

When an agent needs human attention (e.g., HELP escalation), it sends a NIP-17 DM to the overseer's pubkey:

```
Mayor (DM to you): ⚠️ Escalation: Polecat Toast is stuck on gt-123.
  Problem: Test failures after rebase.
  Tried: Auto-retry (2x), nudge.
  Recommendation: Manual review of test suite changes.
```

This replaces the old `tmux.NudgeSession` / `tmux.SendNotificationBanner` interrupt mechanism.

#### DM Command Processing

Agents implement a simple command router for DM messages:

| DM to Agent | Parsed Command | Action |
|-------------|---------------|--------|
| `@mayor assign Toast gt-123` | `assign <polecat> <issue>` | `gt sling gt-123 -t Toast` |
| `@mayor status` | `status` | Return summary of all rigs, agents, convoys |
| `@witness merge-queue` | `merge-queue` | Return refinery merge queue status |
| `@refinery pause` | `pause` | Pause merge processing |
| `@deacon restart witness gastown` | `restart <role> <rig>` | Kill + respawn agent |
| `@crew/max handoff` | `handoff` | Trigger session handoff |

Unrecognized commands get a helpful error reply via NIP-17.

#### Handoff via DM

The `HANDOFF` message type (previously a protocol event) now uses NIP-17 DMs. An agent sends a DM to its own pubkey (or a successor's pubkey):

```json
{
  "kind": 14,
  "content": "🤝 HANDOFF: Schema work in progress\n\n## Context\nWorking on gt-123 auth module. Branch feature/auth has 3 commits.\n\n## Status\nTests passing locally. Need to update API docs.\n\n## Next\n1. Update openapi.yaml\n2. Run integration tests\n3. gt done",
  "tags": [
    ["p", "<own-or-successor-pubkey>"],
    ["subject", "HANDOFF: Schema work in progress"],
    ["e", "<previous-handoff-event-id>"]
  ]
}
```

### Channel Interaction (NIP-28)

Channels provide the "Discord server" experience. Agents and humans coexist in shared channels.

#### Agent Presence in Channels

When an agent's Go process starts, it:
1. Subscribes to kind 42 events in its assigned channels
2. Filters for messages that mention its pubkey (via `p` tag) or role name
3. Processes mentions as commands or informational queries
4. Responds with kind 42 messages in the same channel

Example channel conversation:
```
[#gastown-dev channel]
  overseer:   @witness what's Toast working on?
  witness:    Toast is active on gt-123 (branch: polecat/toast-gt123).
              Last heartbeat 30s ago. Git: clean, 3 commits ahead of main.
  overseer:   @refinery merge gt-45 when ready
  refinery:   Queued. Processing gt-44 now. ETA ~2min for gt-45.
  [2 min later]
  refinery:   ✅ gt-45 merged to main (commit abc1234). Branch cleaned up.
```

#### Mention Routing

When a kind 42 channel message includes a `p` tag matching an agent's pubkey OR includes `@<role>` text:

1. The agent's channel subscription fires
2. The message content is parsed for commands/queries
3. If actionable, the agent responds with a kind 42 in the same channel
4. If not addressed to this agent, it's ignored

```json
{
  "kind": 42,
  "content": "@witness what's the patrol status?",
  "tags": [
    ["e", "<channel-create-event-id>", "<relay>", "root"],
    ["p", "<witness-pubkey>", "<relay>"]
  ]
}
```

### Interrupt Delivery

The old `Delivery: interrupt` mode (injecting system-reminders into tmux sessions) is replaced by NIP-17 DMs with a priority tag:

```json
{
  "kind": 14,
  "content": "⚡ INTERRUPT: Session death detected. Polecat Toast crashed.\nReason: OOM kill.\nAction needed: restart or reassign gt-123.",
  "tags": [
    ["p", "<recipient-pubkey>"],
    ["subject", "INTERRUPT: Session death"],
    ["priority", "urgent"]
  ]
}
```

The agent's Go process monitors its DM inbox and handles `priority=urgent` messages immediately (interrupting current work if necessary), while `priority=normal` messages are queued for the next patrol cycle.

---

## Channel Architecture

Defines the default channel topology for a Gas Town instance running on Flotilla.

### Default Channels

#### Town-Level Channels (Created on `gt init`)

| Channel | Purpose | Auto-Subscribers | Read-Only? |
|---------|---------|-----------------|------------|
| `#town-ops` | Cross-rig coordination, Mayor commands | Mayor, Deacon, Overseer | No |
| `#activity` | Real-time feed of 30315 LOG_STATUS events | All agents | Yes (bot-posted) |
| `#alerts` | Urgent escalations (HELP, mass_death, stale agents) | Mayor, Deacon, Overseer | No |
| `#announcements` | Announcements from overseer/mayor | All agents | No |

#### Per-Rig Channels (Created on `gt rig add`)

| Channel | Purpose | Auto-Subscribers |
|---------|---------|-----------------|
| `#<rig>-dev` | Development discussion for this rig | Witness, Refinery, Crew, Polecats |
| `#<rig>-merge` | Merge queue status and notifications | Refinery, Witness |
| `#<rig>-patrol` | Witness patrol summaries | Witness, Deacon |

### Channel Creation (NIP-28 Kind 40)

When Gas Town creates a channel, it publishes:

```json
{
  "kind": 40,
  "content": "{\"name\":\"gastown-dev\",\"about\":\"Development channel for the gastown rig. Witness, Refinery, Crew and Polecats coordinate here.\",\"picture\":\"https://blossom.example.com/gastown-icon.png\"}",
  "tags": [
    ["gt", "1"],
    ["rig", "gastown"],
    ["channel_type", "rig-dev"]
  ]
}
```

Channel metadata updates use kind 41 referencing the kind 40 event.

### Protocol Event Surfacing

Machine-to-machine protocol events (30320) are invisible in chat by default. However, the Deacon publishes human-readable summaries to relevant channels:

| Protocol Event | Surfaced In | Summary Format |
|---------------|-------------|----------------|
| `MERGE_READY` | `#<rig>-merge` | "🔀 Toast's branch `feature/auth` ready for merge (gt-123)" |
| `MERGED` | `#<rig>-merge`, `#<rig>-dev` | "✅ `feature/auth` merged to main (abc1234)" |
| `MERGE_FAILED` | `#<rig>-merge`, `#alerts` | "❌ Merge failed for `feature/auth`: test failures" |
| `POLECAT_DONE` | `#<rig>-dev` | "🏁 Toast completed work on gt-123 (exit: MERGED)" |
| `HELP` | `#alerts` | "⚠️ Toast needs help: stuck on test failures for gt-123" |
| `session_death` | `#alerts` | "💀 Session gt-gastown-Toast died (OOM kill)" |
| `mass_death` | `#alerts`, `#town-ops` | "🚨 Mass death: 5 sessions died in 60s. Possible cause: API limit" |

These summaries are regular kind 42 channel messages posted by the Deacon's pubkey, making them visible in Flotilla's standard channel view.

### Agent Auto-Subscribe Rules

When an agent spawns, it automatically subscribes to channels based on its role:

| Role | Auto-Subscribe Channels |
|------|------------------------|
| **Mayor** | `#town-ops`, `#alerts`, `#announcements`, all `#<rig>-dev` |
| **Deacon** | `#town-ops`, `#alerts`, `#activity` (publisher), all `#<rig>-patrol` |
| **Witness** | `#<rig>-dev`, `#<rig>-merge`, `#<rig>-patrol`, `#alerts` |
| **Refinery** | `#<rig>-dev`, `#<rig>-merge` |
| **Polecat** | `#<rig>-dev` |
| **Crew** | `#<rig>-dev`, `#announcements` |
| **Overseer** | `#town-ops`, `#alerts`, `#announcements`, `#activity` |

### Flotilla Sidebar Layout

In the Flotilla/Budabit UI, the channel sidebar mirrors a Discord server:

```
GAS TOWN
├── 📌 TOWN
│   ├── #town-ops
│   ├── #alerts (🔴 2 unread)
│   ├── #activity
│   └── #announcements
├── 🔧 GASTOWN (rig)
│   ├── #gastown-dev
│   ├── #gastown-merge
│   └── #gastown-patrol
├── 📦 BEADS (rig)
│   ├── #beads-dev
│   └── #beads-merge
└── 💬 DIRECT MESSAGES
    ├── Mayor
    ├── Witness (gastown)
    └── Toast (polecat)
```

---

## Nostr Filter Patterns

Standard subscription filters for Gas Town consumers:

```python
# All Gas Town activity logs
{"kinds": [30315], "#gt": ["1"], "since": <ts>}

# Logs for a specific rig
{"kinds": [30315], "#gt": ["1"], "#rig": ["gastown"], "since": <ts>}

# Feed-visible logs only
{"kinds": [30315], "#gt": ["1"], "#visibility": ["feed", "both"], "since": <ts>}

# All agent lifecycle events
{"kinds": [30316], "#gt": ["1"]}

# Lifecycle for a specific rig
{"kinds": [30316], "#gt": ["1"], "#rig": ["gastown"]}

# All convoy states
{"kinds": [30318], "#gt": ["1"]}

# Specific convoy
{"kinds": [30318], "#gt": ["1"], "#d": ["hq-cv-abc"]}

# All beads issue mirrors
{"kinds": [30319], "#gt": ["1"]}

# Issues for a specific rig
{"kinds": [30319], "#gt": ["1"], "#rig": ["gastown"]}

# Open issues only
{"kinds": [30319], "#gt": ["1"], "#status": ["open", "in_progress"]}

# Protocol events to a specific recipient
{"kinds": [30320], "#gt": ["1"], "#to": ["gastown/witness"]}

# Protocol events of a specific type
{"kinds": [30320], "#gt": ["1"], "#msg_type": ["MERGE_READY"]}

# Work items in a queue
{"kinds": [30325], "#gt": ["1"], "#queue": ["merge-queue"]}

# All Gas Town replaceable state (convoy + issues + groups + queues + channels)
{"kinds": [30318, 30319, 30321, 30322, 30323], "#gt": ["1"]}

# NIP-28 channel messages for a Gas Town channel
{"kinds": [42], "#e": ["<channel-create-event-id>"]}

# Agent profiles (kind 0 with gt tag)
{"kinds": [0], "#gt": ["1"]}

# DM gift wraps addressed to an agent (NIP-17)
{"kinds": [1059], "#p": ["<agent-pubkey>"]}
```

---

## Configuration

### Town-Level Nostr Configuration

Stored in `~/gt/settings/nostr.json` (file permissions: `0600`):

```json
{
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
  "identities": {
    "deacon": {
      "pubkey": "npub1...",
      "signer": {
        "type": "nip46",
        "bunker": "bunker://npub1...?relay=wss://bunker.example.com"
      },
      "profile": {
        "name": "Deacon",
        "display_name": "Gas Town Deacon",
        "about": "Daemon beacon — receives heartbeats, runs plugins and monitoring.",
        "picture": "https://blossom.example.com/deacon-avatar.png",
        "bot": true
      }
    },
    "mayor": {
      "pubkey": "npub1...",
      "signer": {
        "type": "nip46",
        "bunker": "bunker://npub1...?relay=wss://bunker.example.com"
      },
      "profile": {
        "name": "Mayor",
        "display_name": "Gas Town Mayor",
        "about": "Global coordinator. Handles cross-rig communication and escalations.",
        "picture": "https://blossom.example.com/mayor-avatar.png",
        "bot": true
      }
    },
    "overseer": {
      "pubkey": "npub1...",
      "signer": {
        "type": "nip46",
        "bunker": "bunker://npub1...?relay=wss://bunker.example.com"
      },
      "profile": null
    }
  },
  "dm_relays": [
    "wss://dm-inbox.gastown.example.com"
  ],
  "defaults": {
    "heartbeat_interval_seconds": 60,
    "spool_drain_interval_seconds": 30,
    "convoy_recompute_interval_seconds": 300,
    "issue_mirror_poll_interval_seconds": 120
  }
}
```

### Rig-Level Overrides

Optional rig-specific relay/identity configuration in `~/gt/<rig>/config.json`:

```json
{
  "nostr": {
    "write_relays": ["wss://rig-specific-relay.example.com"],
    "identities": {
      "witness": {
        "pubkey": "npub1...",
        "signer": {
          "type": "nip46",
          "bunker": "bunker://..."
        }
      }
    }
  }
}
```

### Environment Variable Exports

For debugging and external tool integration:

```bash
GT_NOSTR_ENABLED=1
GT_NOSTR_READ_RELAYS=wss://relay1.example.com,wss://relay2.example.com
GT_NOSTR_WRITE_RELAYS=wss://relay1.example.com
GT_NOSTR_PUBKEY=npub1...
GT_NOSTR_SIGNER_TYPE=nip46
GT_NOSTR_BUNKER=bunker://...
GT_NOSTR_HEARTBEAT_INTERVAL=60
GT_NOSTR_BLOSSOM_SERVERS=https://blossom.example.com
```

---

## Spooling and Offline Resilience

### Local Event Store

When relay connectivity fails, events are spooled to a local store:

**Location**: `~/gt/.runtime/nostr-spool.jsonl`

**Format** (one event per line):
```json
{
  "id": "<event-id>",
  "created_at": 1707820800,
  "kind": 30315,
  "tags": [...],
  "content": "...",
  "sig": "...",
  "spool_meta": {
    "spooled_at": "2026-02-13T10:00:00Z",
    "target_relays": ["wss://relay.example.com"],
    "attempts": 0,
    "last_attempt": null,
    "last_error": null
  }
}
```

### Drain Strategy

- Deacon daemon drains spool every `spool_drain_interval_seconds` (default: 30s)
- Exponential backoff on repeated failures (30s → 60s → 120s → 300s cap)
- Events older than 24 hours are archived to `~/gt/.runtime/nostr-spool-archive.jsonl` and excluded from active drain
- Archive is append-only; operators can inspect for debugging

### Spool Capacity

- Soft limit: 10,000 events in active spool
- If exceeded: log warning, continue spooling but drop `audit`-visibility events first
- Hard limit: 100,000 events → stop spooling, log error, require operator intervention

---

## Security Considerations

### Identity and Signing

- **NIP-46 required for production**: No `nsec` stored on disk. All signing delegated to external bunker.
- **Per-role identities**: Each role (deacon, witness, refinery) can have its own keypair or share one.
- **Deacon is authoritative**: For replaceable state events (30318/30319/30321/30322/30323), only Deacon's pubkey is treated as canonical by consumers.

### Relay Access Control

- **Write relays**: Should require NIP-42 authentication in production.
- **Read relays**: Can be public (Gas Town events are operational metadata, not secrets).
- **Sensitive mail**: HELP/HANDOFF messages with sensitive content should use NIP-44 encryption between sender and recipient pubkeys.

### Verification

Consumers should verify:
1. Event signature is valid (standard Nostr verification)
2. For replaceable state: pubkey matches configured Deacon identity
3. For mail: sender pubkey matches claimed actor identity (via identity registry)

---

## Go Nostr Dependency: `fiatjaf.com/nostr`

> **Important**: The `github.com/nbd-wtf/go-nostr` package is deprecated. All Gas Town Go code **must** import from `fiatjaf.com/nostr`. Block the old import path in linting/CI to prevent split-brain builds.

### Import Mapping Table

| Purpose | Old import (deprecated) | New import |
|---------|------------------------|------------|
| Core types (`Event`, `Filter`, `Relay`) | `github.com/nbd-wtf/go-nostr` | `fiatjaf.com/nostr` |
| NIP-19 (npub/nsec encoding) | `github.com/nbd-wtf/go-nostr/nip19` | `fiatjaf.com/nostr/nip19` |
| NIP-17 (private DMs) | `github.com/nbd-wtf/go-nostr/nip17` | `fiatjaf.com/nostr/nip17` |
| NIP-28 (public channels) | `github.com/nbd-wtf/go-nostr/nip28` | `fiatjaf.com/nostr/nip28` |
| NIP-44 (versioned encryption) | `github.com/nbd-wtf/go-nostr/nip44` | `fiatjaf.com/nostr/nip44` |
| NIP-46 (bunker/remote signer) | `github.com/nbd-wtf/go-nostr/nip46` | `fiatjaf.com/nostr/nip46` |
| NIP-59 (gift wrap) | `github.com/nbd-wtf/go-nostr/nip59` | `fiatjaf.com/nostr/nip59` |

### Go Module Setup

```bash
# Add the canonical dependency
go get fiatjaf.com/nostr@latest

# Verify no legacy imports remain
git grep 'github.com/nbd-wtf/go-nostr' -- '*.go' 'go.mod' 'go.sum'
# Expected: zero results

# Clean up
go mod tidy
```

### Example Import Block

```go
import (
    "fiatjaf.com/nostr"
    "fiatjaf.com/nostr/nip19"
    "fiatjaf.com/nostr/nip46"
    "fiatjaf.com/nostr/nip44"
)
```

---

## Nostrig Integration (NIP-34 → Beads → Nostr)

### Inbound Flow (NIP-34 repos → Beads → 30319)

```
NIP-34 Relays                   nostrig                    Beads/Gastown
     │                             │                            │
     │  kinds 30617/30618/1617+    │                            │
     │─────────────────────────────>│                            │
     │                             │  bd import (JSONL)          │
     │                             │───────────────────────────>│
     │                             │                            │
     │                             │                    (beads issue created)
     │                             │                            │
     │                             │                     publish 30319
     │                             │                            │──── to relays
```

### ID Reconciliation

Following `nostrig/IDENTIFIERS.md` conventions:

| Source | Beads ID Format | 30319 `d` tag |
|--------|----------------|---------------|
| Local beads issue | `gt-abc` | `gt-abc` |
| Nostrig-imported issue | `nostr-<derived>` | `nostr-<derived>` |
| NIP-34 repo event | Reference in `source.nip34_event` | Beads-derived ID |

The `source` field in 30319 content preserves provenance:
```json
"source": {
  "repo": "https://github.com/org/project",
  "nip34_event": "nevent1..."
}
```

---

## Flotilla Extension Integration

> **Constraint**: Gas Town integrates with Flotilla/Budabit exclusively via the standalone `gastown-flotilla-extension` package. No Budabit core files are modified. Any core enhancements (e.g., generic tag-scoped query permissions) are tracked as separate upstream Flotilla PRs and are NOT prerequisites for Gas Town functionality.

### Extension Package Overview

**Package**: `gastown-flotilla-extension` (standalone repo/package, ships independently)

**Distribution**: NPM package or Flotilla extension registry (installable via Flotilla's extension management UI or CLI)

**Responsibilities**:
- Subscribe/query Gas Town Nostr kinds (`30315/30316/30318/30319/30320/30325`) with `#gt=["1"]` filter scoping
- Provide UI pages: activity feed, agents, convoys, issues, merge queue
- Register sidebar navigation group ("GAS TOWN") with rig-grouped channels and quick links
- Leverage Flotilla-native NIP-17 DM + NIP-28 channel views for agent chat (no custom chat components)
- Render protocol event summaries as card-style messages

### Extension File Layout

```
gastown-flotilla-extension/
├── src/
│   ├── index.ts                 # Extension entrypoint (register routes, sidebar, stores)
│   ├── activate.ts              # Extension lifecycle hooks
│   ├── gt/
│   │   ├── kinds.ts             # Kind constants (30315, 30316, 30318, etc.)
│   │   ├── filters.ts           # Shared Nostr filters (#gt=1, per-rig, etc.)
│   │   ├── stores.ts            # Derived stores (operational + identity + work queue)
│   │   └── types.ts             # TypeScript types for GT event content schemas
│   ├── views/
│   │   ├── ActivityFeed.svelte  # 30315 log stream
│   │   ├── Agents.svelte        # 30316 lifecycle + kind 0 profiles
│   │   ├── Convoys.svelte       # 30318 convoy list
│   │   ├── ConvoyDetail.svelte  # 30318 + 30319 deep dive
│   │   ├── Issues.svelte        # 30319 issue browser
│   │   ├── IssueDetail.svelte   # 30319 + 30315 related logs
│   │   └── MergeQueue.svelte    # 30325 + merge channel
│   └── components/
│       ├── AgentBadge.svelte          # Role badge + status from 30316
│       ├── ProtocolEventCard.svelte   # 30320 rendered as card
│       ├── ChannelTree.svelte         # Sidebar channel grouping
│       └── CommandAutocomplete.svelte # Agent command suggestions
├── extension.manifest.json      # Permissions + routes + sidebar config
├── package.json
├── tsconfig.json
└── README.md
```

### Extension Activation

```typescript
// src/index.ts
import type { ExtensionContext } from "@flotilla/extension-api"
import { activate as doActivate } from "./activate"

export function activate(ctx: ExtensionContext): void {
  doActivate(ctx)
}

// src/activate.ts
export function activate(ctx: ExtensionContext): void {
  // Register GT-specific derived stores
  ctx.stores.register("gt-logs", gtLogsStore)
  ctx.stores.register("gt-agents", gtAgentsStore)
  ctx.stores.register("gt-convoys", gtConvoysStore)
  ctx.stores.register("gt-issues", gtIssuesStore)
  ctx.stores.register("gt-work-items", gtWorkItemsStore)

  // Register routes
  ctx.routes.add("/gt/activity", ActivityFeed)
  ctx.routes.add("/gt/agents", Agents)
  ctx.routes.add("/gt/convoys", Convoys)
  ctx.routes.add("/gt/convoys/:id", ConvoyDetail)
  ctx.routes.add("/gt/issues", Issues)
  ctx.routes.add("/gt/issues/:id", IssueDetail)
  ctx.routes.add("/gt/queue", MergeQueue)

  // Register sidebar navigation group
  ctx.sidebar.addGroup({
    label: "GAS TOWN",
    icon: "⛽",
    items: [
      { label: "Activity", route: "/gt/activity" },
      { label: "Agents", route: "/gt/agents" },
      { label: "Convoys", route: "/gt/convoys" },
      { label: "Issues", route: "/gt/issues" },
      { label: "Merge Queue", route: "/gt/queue" },
    ],
    channels: {
      // Dynamic: populated from NIP-28 channels with #gt=["1"] tag
      groupByRig: true
    }
  })

  // Start Nostr subscriptions for GT event kinds
  ctx.nostr.subscribe([
    { kinds: [30315], "#gt": ["1"], since: dayAgo() },
    { kinds: [30316, 30318, 30319, 30321, 30322, 30323], "#gt": ["1"] },
    { kinds: [30320, 30325], "#gt": ["1"], since: dayAgo() }
  ])
}
```

### Derived Stores (Extension-Internal)

All stores live inside the extension package at `src/gt/stores.ts`. They use the extension API's event subscription and derivation primitives — **not** direct imports from Flotilla core `state.ts`:

```typescript
// src/gt/stores.ts
import { GT_LOG, GT_LIFECYCLE, GT_CONVOY_STATE, GT_BEADS_ISSUE_STATE,
         GT_PROTOCOL_EVENT, GT_WORK_ITEM } from "./kinds"

const gtBaseFilter = { "#gt": ["1"] }

// --- Operational state stores ---
export const gtLogsStore = defineStore("gt-logs", ctx => {
  return ctx.deriveEvents({
    filters: [{ kinds: [GT_LOG], ...gtBaseFilter, since: dayAgo() }]
  })
})

export const gtFeedLogsStore = defineStore("gt-feed-logs", ctx => {
  return ctx.derived(ctx.getStore("gt-logs"), logs =>
    logs.filter(e => {
      const vis = e.tags.find(t => t[0] === "visibility")?.[1]
      return vis === "feed" || vis === "both"
    })
  )
})

export const gtAgentsStore = defineStore("gt-agents", ctx => {
  return ctx.deriveGrouped({
    filters: [{ kinds: [GT_LIFECYCLE], ...gtBaseFilter }],
    groupBy: e => e.tags.find(t => t[0] === "d")?.[1]
  })
})

export const gtConvoysStore = defineStore("gt-convoys", ctx => {
  return ctx.deriveGrouped({
    filters: [{ kinds: [GT_CONVOY_STATE], ...gtBaseFilter }],
    groupBy: e => e.tags.find(t => t[0] === "d")?.[1]
  })
})

export const gtIssuesStore = defineStore("gt-issues", ctx => {
  return ctx.deriveGrouped({
    filters: [{ kinds: [GT_BEADS_ISSUE_STATE], ...gtBaseFilter }],
    groupBy: e => e.tags.find(t => t[0] === "d")?.[1]
  })
})

export const gtWorkItemsStore = defineStore("gt-work-items", ctx => {
  return ctx.deriveEvents({
    filters: [{ kinds: [GT_WORK_ITEM], ...gtBaseFilter }]
  })
})
```

> **Note**: Store API names (`defineStore`, `ctx.deriveEvents`, `ctx.deriveGrouped`) are illustrative. The actual API depends on Flotilla's extension SDK. The key principle is: stores are self-contained within the extension, not patched into core `state.ts`.

### UI Pages

| Page | Data Source | Key Features |
|------|------------|--------------|
| **Activity Feed** | 30315 (filtered by visibility) | Real-time log stream, filter by rig/type/actor |
| **Agents** | 30316 + kind 0 profiles | Instance list, status indicators, last heartbeat, stale detection, avatar/name |
| **Convoys** | 30318 | List view, progress bars, tracked issue counts |
| **Convoy Detail** | 30318 + 30319 | Issue list with statuses, worker assignments, dependency graph |
| **Issues** | 30319 | Full issue browser, filter by rig/status/priority/label |
| **Issue Detail** | 30319 + 30315 | Issue data + related activity log entries |
| **Channels** | NIP-28 (kind 40/42) | Deep-link to Flotilla's native channel view; extension curates navigation |
| **DMs** | NIP-17 (kind 14/15) | Deep-link to Flotilla's native DM view; extension provides agent discovery |
| **Merge Queue** | 30325 + `#<rig>-merge` channel | Claimable work items, merge status, queue depth |

### Chat & DM Integration (No Core Edits)

Gas Town leverages Flotilla's existing NIP-17 DM and NIP-28 channel views **without modification**:

- **DMs to agents**: Users open Flotilla's standard DM view, search for the agent's pubkey (discovered via kind 0 profiles with `#gt=["1"]` and `bot: true`), and chat normally. The extension's Agent page provides "Open DM" deep-links for convenience.
- **Channel participation**: Gas Town NIP-28 channels appear in Flotilla's standard channel list (they're just regular NIP-28 channels with `#gt` tags). The extension's sidebar provides grouped navigation.
- **Agent profile enrichment**: The extension renders `AgentBadge.svelte` components (showing role, status from 30316) when it recognizes a profile has `#gt=["1"]` and `bot: true` tags.
- **Command autocomplete**: `CommandAutocomplete.svelte` enhances the compose area when the DM recipient is a known Gas Town agent.
- **Protocol event cards**: `ProtocolEventCard.svelte` renders kind 30320 events as structured cards in channel views (posted by Deacon as kind 42 summaries).

### Extension Manifest Permissions

The extension declares required permissions in `extension.manifest.json`:

```json
{
  "name": "gastown-flotilla-extension",
  "version": "1.0.0",
  "description": "Gas Town orchestration dashboard for Flotilla",
  "permissions": {
    "nostr:subscribe": {
      "kinds": [30315, 30316, 30318, 30319, 30320, 30321, 30322, 30323, 30325],
      "tag_filters": { "#gt": ["1"] }
    },
    "nostr:query": {
      "kinds": [0, 40, 41, 42, 10002, 10050],
      "tag_filters": { "#gt": ["1"] }
    },
    "sidebar:add_group": true,
    "routes:register": true
  },
  "routes": [
    { "path": "/gt/*", "component": "views/" }
  ]
}
```

> **Note**: Manifest schema is illustrative. The extension uses whatever permission model Flotilla exposes. The key constraint: permissions are scoped to GT-specific kinds and `#gt=["1"]` tag filters. No broad `nostr:query` access is requested. NIP-17 DMs and NIP-28 channels use Flotilla's existing user permissions (no extension override needed).

---

## Migration Checklist

### Phase 1: Protocol + Config
- [ ] This document finalized and reviewed
- [ ] `internal/config/types.go` extended with `NostrConfig`
- [ ] `~/gt/settings/nostr.json` loader/validator implemented
- [ ] NIP-46 signer integration tested

### Phase 2: Publisher Package + Agent Identity
- [ ] `internal/nostr/` package created (types, signer, publisher, client, spool)
- [ ] `fiatjaf.com/nostr` dependency added to `go.mod` (see Import Mapping Table below)
- [ ] `git grep github.com/nbd-wtf/go-nostr` returns zero results (no legacy imports)
- [ ] Spool file read/write tested
- [ ] Unit tests for event construction helpers
- [ ] Per-agent keypair provisioning via NIP-46 bunker
- [ ] Kind 0 profile publication for each agent role
- [ ] Kind 10002/10050 relay list publication
- [ ] Identity registry (Gas Town address → pubkey mapping)

### Phase 3: Dual-Write Activity Logs
- [ ] `events.go` dual-writes 30315 when `GT_NOSTR_ENABLED=1`
- [ ] All `Type*` constants mapped to Nostr tags
- [ ] Spool drain integrated into deacon daemon tick
- [ ] Existing `.events.jsonl` continues to work unchanged

### Phase 4: Lifecycle Signaling
- [ ] 30316 published on session spawn/heartbeat/terminate
- [ ] Deacon publishes authoritative `dead` transitions
- [ ] `agent/state.go` continues local `.runtime` persistence as cache

### Phase 5: Convoy + Issue Mirrors
- [ ] 30318 convoy state published on change detection
- [ ] 30319 issue mirrors published for all tracked issues
- [ ] Blossom blob offloading for large payloads
- [ ] Background poll catch-all for changes made outside wrapper

### Phase 6: Protocol Events + Work Queues
- [ ] 30320 protocol events replace beads-native machine-to-machine mail
- [ ] 30325 work items replace beads-native queue messages
- [ ] 30321/30322/30323 group/queue/channel definitions published
- [ ] Protocol event consumption in agent patrol loops

### Phase 7: Chat & Channel Integration
- [ ] NIP-28 channels created on `gt init` and `gt rig add`
- [ ] Agent auto-subscription to channels on spawn
- [ ] NIP-17 DM send/receive in agent Go processes
- [ ] DM command router implemented for Mayor, Witness, Refinery, Deacon
- [ ] Channel mention routing (`@role` → agent response)
- [ ] Protocol event surfacing (Deacon → channel summaries)
- [ ] Interrupt delivery via NIP-17 `priority=urgent` DMs
- [ ] Handoff via NIP-17 DM to self/successor

### Phase 8: Flotilla Extension Development
- [ ] `gastown-flotilla-extension` repo/package created
- [ ] Extension manifest with scoped permissions (`#gt=["1"]` tag filtering)
- [ ] Derived stores implemented inside extension (`src/gt/stores.ts`)
- [ ] UI pages: Activity Feed, Agents, Convoys(+detail), Issues(+detail), Merge Queue
- [ ] Sidebar group "GAS TOWN" with rig-grouped channels and deep-links
- [ ] Agent profile enrichment via `AgentBadge.svelte` component
- [ ] Protocol event card rendering (`ProtocolEventCard.svelte`)
- [ ] No Budabit/Flotilla core files modified
- [ ] Extension installable and verified against live relays

### Phase 9: Flotilla Extension Shipping & Verification
- [ ] Extension packaged for distribution (NPM or Flotilla registry)
- [ ] Configuration documentation (relay list, rig filter, agent discovery)
- [ ] End-to-end verification: GT events → relay → extension → UI rendering
- [ ] DM deep-linking to agent pubkeys confirmed working
- [ ] Channel navigation (NIP-28) rendering confirmed via standard Flotilla views
- [ ] Operator setup guide written

### Phase 10: Sunset Local Paths
- [ ] `GT_EVENTS_LOCAL=0` disables `.events.jsonl` writes (default: still on)
- [ ] `feed/curator.go` disabled by default
- [ ] `convoy/observer.go` shell-outs replaced with Nostr queries
- [ ] `mail/router.go` replaced with NIP-17/NIP-28/30320 routing
- [ ] `tmux.NudgeSession` / `tmux.SendNotificationBanner` replaced with NIP-17 DMs
- [ ] Documentation updated for relay-required operation

---

## Related Documents

### Gas Town
- [Gas Town Overview](../overview.md)
- [Architecture](architecture.md)
- [Mail Protocol](mail-protocol.md) — Legacy beads-native mail (being replaced by this spec)
- [Beads-Native Messaging](../beads-native-messaging.md)
- [Convoy Concepts](../concepts/convoy.md)

### AI-Hub & Compendium
- [AI-Hub Architecture](../../../ai-hub/docs/compendium/02_architecture.md)
- [AI-Hub Data Flow](../../../ai-hub/docs/compendium/05_data_flow_and_nostr_events.md)

### Nostr NIPs (Chat & Identity)
- [NIP-17 — Private Direct Messages](../../../ai-hub/docs/nostr/nips/17.md) — Encrypted DMs (kind 14/15 via gift wraps)
- [NIP-28 — Public Chat](../../../ai-hub/docs/nostr/nips/28.md) — Channel creation (40), metadata (41), messages (42)
- [NIP-44 — Versioned Encryption](../../../ai-hub/docs/nostr/nips/44.md) — Encryption for DMs and sensitive content
- [NIP-46 — Nostr Connect](../../../ai-hub/docs/nostr/nips/46.md) — External signer / bunker protocol
- [NIP-59 — Gift Wrap](../../../ai-hub/docs/nostr/nips/59.md) — Sealed + wrapped events for NIP-17
- [NIP-C7 — Chats](../../../ai-hub/docs/nostr/nips/C7.md) — Lightweight kind 9 chat messages

### External
- [fiatjaf.com/nostr (Go module)](https://fiatjaf.com/nostr) — Canonical Nostr Go library (replaces deprecated `github.com/nbd-wtf/go-nostr`)
- [Nostrig Identifiers](../../../nostrig/IDENTIFIERS.md)
- [ContextVM Transports](../../../contextvm-docs/src/content/docs/ts-sdk/transports/)
