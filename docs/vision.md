# Gas Town: Ideas We're Exploring

> **Status**: These are ideas we're experimenting with, not proven solutions.
> We're sharing them in case others find them useful or want to explore together.

Gas Town is an experiment in multi-agent coordination. We use steam-age metaphors
to think about how work flows through a system of AI agents.

---

## Idea 1: The Steam Engine Metaphor

```
Claude       = Fire (the energy source)
Claude Code  = Steam Engine (harnesses the fire)
Gas Town     = Steam Train (coordinates engines on tracks)
Beads        = Railroad Tracks (the persistent ledger of work)
```

The engine does work and generates steam. Gas Town coordinates many engines on
a shared network, routing work to the right engines, tracking outcomes, and
ensuring nothing is lost.

| Component | Phase | Role |
|-----------|-------|------|
| **Proto** | Solid (crystal) | Frozen workflow templates |
| **Mol** | Liquid | Flowing durable work instances |
| **Wisp** | Vapor (gas) | Transient ephemeral traces |
| **Digest** | Distillate | Compressed permanent records |

---

## Idea 2: Village vs Hierarchy

We're exploring whether a "village" model works better than centralized monitoring.

**The pattern we're trying to avoid:**
```
Centralized Monitor → watches all workers → single point of failure
                   → fragile protocols → cascading failures
```

**The pattern we're experimenting with:**
```
Every worker → understands the whole → can help any neighbor
           → peek is encouraged → distributed awareness
           → ant colony without murder → self-healing system
```

### Aspiration: Antifragility

We're aiming for anti-fragility - a system that gets stronger from stress rather
than just surviving it. Whether we achieve this is an open question.

Properties we're trying to build:

- **Distributed awareness**: Every agent understands the system deeply
- **Mutual monitoring**: Any agent can peek at any other agent's health
- **Collective intervention**: If you see something stuck, you can help
- **No single point of failure**: The village survives individual failures
- **Organic healing**: Problems get fixed by whoever notices them first

This is an ant colony, except the ants don't kill defective members - they help
them recover. Workers who crash are respawned. Workers who get stuck are nudged.
Workers who need help receive it.

### Practical Implications

1. **Every patrol includes neighbor-checking**
   - Polecats peek at other polecats
   - Witness peeks at Refinery
   - Refinery peeks at Witness
   - Everyone can peek at the Deacon

2. **`gt peek` is universal vocabulary**
   - Any agent can check any other agent's health
   - Health states are shared vocabulary: idle, working, stuck, done

3. **Exit state enums are teaching tools**
   - COMPLETED, BLOCKED, REFACTOR, ESCALATE
   - Every agent learns these
   - When peeking neighbors, agents recognize states and can help

4. **Mail is the nervous system**
   - Asynchronous, persistent, auditable
   - Survives crashes and restarts
   - The village communicates through mail

---

## Idea 3: Rig, Cook, Run

Work in Gas Town flows through three phases:

```
RIG ────→ COOK ────→ RUN
```

| Phase | What Happens | Key Operator |
|-------|--------------|--------------|
| **Rig** | Compose formulas (source level) | extends, compose |
| **Cook** | Instantiate work (pour/wisp) | cook, pour, wisp |
| **Run** | Execute steps | Agent execution |

**Rig** is source-level composition (formula YAML).
**Bond** is artifact-level composition (protos, mols, wisps).

See [molecular-chemistry.md](molecular-chemistry.md) for the full specification.

### The Three Phases of Matter

Work artifacts exist in three phases:

| Phase | Name | State | Behavior |
|-------|------|-------|----------|
| **Solid** | Proto | Frozen template | Crystallized, immutable, reusable |
| **Liquid** | Mol | Flowing instance | Dynamic, adapting, persistent |
| **Vapor** | Wisp | Ephemeral trace | Transient, dissipates, operational |

### Phase Transition Operators

```
            ┌─────────────┐
            │    PROTO    │
            │   (solid)   │
            └──────┬──────┘
                   │
         ┌─────────┼─────────┐
         │         │         │
       pour      wisp     distill
         │         │         ↑
         ▼         ▼         │
    ┌─────────┐ ┌─────────┐  │
    │   MOL   │ │  WISP   │  │
    │(liquid) │ │ (vapor) │  │
    └────┬────┘ └────┬────┘  │
         │           │       │
      squash      squash     │
         │           │       │
         ▼           ▼       │
    ┌─────────┐ ┌─────────┐  │
    │ DIGEST  │ │evaporates│ │
    │(crystal)│ │ or burn  │ │
    └─────────┘ └──────────┘ │
         │                   │
         └───────────────────┘
            (experience crystallizes)
```

| Operator | From | To | Effect |
|----------|------|------|--------|
| `pour` | Proto | Mol | Instantiate as persistent liquid |
| `wisp` | Proto | Wisp | Instantiate as ephemeral vapor |
| `bond` | Any + Any | Compound | Polymorphic combination |
| `squash` | Mol/Wisp | Digest | Condense to permanent record |
| `burn` | Wisp | Nothing | Discard without record |
| `distill` | Mol | Proto | Extract reusable template |

### The Polymorphic Bond Operator

**Bond** is the artifact-level combiner (distinct from **rig**, which composes
source formulas). Bond adapts to its operands:

| bond | Proto | Mol | Wisp |
|------|-------|-----|------|
| **Proto** | Compound Proto | Pour + attach | Wisp + attach |
| **Mol** | Pour + attach | Link | Link |
| **Wisp** | Wisp + attach | Link | Link |

This enables patterns like:
- Patrol wisp discovers issue → bonds new work mol
- Feature work needs diagnostic → bonds vapor wisp
- Witness tracks polecats → bonds lease per polecat

---

## Idea 4: Beads as Data Plane

We use Beads (a separate project) as the persistence layer. It's Git + Issues
in one human-readable format.

**Properties we liked:**
- **Git-backed**: Uses existing infrastructure
- **Human-readable**: You can read `.beads/` files directly
- **Portable**: No vendor lock-in

See [github.com/steveyegge/beads](https://github.com/steveyegge/beads) for more.

### Git as Persistence

Everything persists through git:
- Issues are JSONL in `.beads/`
- Molecules are structured issues
- Mail is issues with labels

This gives us offline-first operation and no infrastructure requirements.

### Control Plane = Data Plane

Gas Town uses Beads as both control plane and data plane:

| Data Type | Beads Representation |
|-----------|---------------------|
| Work items | Issues (tasks, bugs, features) |
| Workflows | Molecules (type=molecule) |
| Messages | Mail beads (type=message) |
| Merge requests | Queue entries (type=merge-request) |
| Agent state | Status on assigned issues |

The control state IS data in Beads. Agents read Beads to know what to do next.
There is no separate orchestrator - Beads IS the orchestrator.

---

## Idea 5: Patrol Loops

Gas Town runs on continuous monitoring loops called **patrols**.

### Patrol Agents

| Agent | Role | Patrol Focus |
|-------|------|--------------|
| **Deacon** | Town-level daemon | Health of all agents, plugin execution |
| **Witness** | Per-rig polecat monitor | Polecat lifecycle, nudging, cleanup |
| **Refinery** | Per-rig merge processor | Merge queue, validation, integration |

### Patrol Wisps

Patrol agents run ephemeral wisps for their cycles:
- Wisp starts at cycle begin
- Steps complete as work progresses
- Wisp squashes to digest at cycle end
- New wisp created for next cycle

This prevents accumulation: patrol work is vapor that condenses to minimal
digests, not liquid that pools forever.

### The Witness Polecat-Tracking Wisp

The Witness maintains a rolling wisp with a **lease** per active polecat:

```
wisp-witness-patrol
├── lease: furiosa (boot → working → done)
├── lease: nux (working)
└── lease: slit (done, closed)
```

Each lease is a bonded vapor molecule tracking one polecat's lifecycle.
When a polecat exits, its lease closes. When all leases close, the wisp
squashes to a summary digest.

---

## Idea 6: Propulsion Over Protocol

We're experimenting with "pull-based" work where agents propel themselves
rather than waiting for explicit commands:

1. **Check hook/pin** - What's attached to me?
2. **Find next step** - What's ready in my molecule?
3. **Execute** - Do the work
4. **Advance** - Close step, find next
5. **Exit properly** - One of four exit types

The idea is pull-based work: the molecule provides instructions, the agent executes.

### Hooks and Pins

Agents have **hooks** where work hangs. Work gets **pinned** to hooks.

```
Agent (with hook)
    └── pinned mol (or wisp)
        ├── step 1 (done)
        ├── step 2 (in_progress)
        └── step 3 (pending)
```

This enables:
- **Crash recovery**: Agent restarts, reads pinned mol, continues
- **Context survival**: Mol state persists across sessions
- **Handoff**: New session reads predecessor's pinned work
- **Observability**: `bd hook` shows what an agent is working on

### The Four Exits

Every polecat converges on one of four exits:

| Exit | Meaning | Action |
|------|---------|--------|
| **COMPLETED** | Work finished | Submit to merge queue |
| **BLOCKED** | External dependency | File blocker, defer, notify |
| **REFACTOR** | Work too large | Break down, defer rest |
| **ESCALATE** | Need human judgment | Document, mail human, defer |

All exits pass through the exit-decision step. All exits end in request-shutdown.
The polecat never exits directly - it waits to be killed by the Witness.

---

## Idea 7: Nondeterministic Idempotence

An idea we're exploring for crash recovery:

- **Deterministic structure**: Molecule defines exactly what steps exist
- **Nondeterministic execution**: Any worker can execute any ready step
- **Idempotent progress**: Completed steps stay completed

```
Worker A picks up "design" step
Worker A completes "design"
Worker A crashes mid-"implement"
Worker B restarts, queries ready work
Worker B sees "implement" is ready (design done, implement pending)
Worker B continues from exactly where A left off
```

No work is lost. No state is in memory. Any worker can continue any molecule.

---

## The Agent Hierarchy

### Overseer (Human)
- Sets strategy and priorities
- Reviews and approves output
- Handles escalations
- Operates the system

### Mayor (AI - Town-wide)
- Dispatches work across rigs
- Coordinates cross-project dependencies
- Handles strategic decisions

### Deacon (AI - Town-level daemon)
- Ensures patrol agents are running
- Executes maintenance plugins
- Handles lifecycle requests

### Witness (AI - Per-rig)
- Manages polecat lifecycle
- Detects stuck workers
- Handles session cycling

### Refinery (AI - Per-rig)
- Processes merge queue
- Reviews and integrates code
- Maintains branch hygiene

### Polecat (AI - Ephemeral workers)
- Executes work molecules
- Files discovered issues
- Ephemeral - spawn, work, disappear

---

## The Steam Train in Action

Putting it all together:

```
1. Human files issue in Beads
2. Mayor dispatches: gt sling --issue <id>
3. Polecat created with:
   - Fresh worktree
   - mol-polecat-work pinned to hook
   - Work assignment in mail
4. Deacon/Witness notified: POLECAT_STARTED
5. Witness bonds lease to patrol wisp
6. Polecat:
   - Reads polecat.md, orients
   - Reads mail, gets assignment
   - Executes mol-polecat-work steps
   - Makes commits, runs tests
   - Submits to merge queue
   - Exits via request-shutdown
7. Witness:
   - Receives SHUTDOWN mail
   - Closes polecat's lease
   - Kills session, cleans up
8. Refinery:
   - Processes merge queue
   - Rebases, tests, merges
   - Pushes to main
9. Digest created: work outcome crystallized
10. Loop: new work, new polecats, new cycles
```

The flywheel spins. The village watches itself. The train keeps running.

---

## What We're Building Toward

We're interested in exploring AI as collaborator rather than just assistant.
Instead of AI suggesting code, what if AI could complete tasks while humans
review the outcomes?

This is early-stage experimentation. Some of it works, much of it doesn't yet.
We're sharing it in case the ideas are useful to others.

---

## Summary

Gas Town explores:
- Multi-agent coordination via "molecules" of work
- Git-backed persistence via Beads
- Distributed monitoring via the "village" model
- Crash recovery via nondeterministic idempotence

We don't know if these ideas will pan out. We invite you to explore with us.
