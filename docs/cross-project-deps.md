# Cross-Project Dependencies

> Design for tracking dependencies across project boundaries without coupling to
> specific orchestrators.

## Problem Statement

When working on Gas Town, we frequently hit dependencies on Beads features (and
vice versa). Currently there's no formal mechanism to:

1. Declare "I need capability X from project Y"
2. Signal "capability X is now available"
3. Park work waiting on external dependencies
4. Resume work when dependencies are satisfied

## Design Principles

1. **Beads-native**: The core mechanism lives in Beads, not Gas Town
2. **Orchestrator-agnostic**: Works with Gas Town, other orchestrators, or none
3. **Capability-based**: Reference published capabilities, not internal issue IDs
4. **Semaphore pattern**: Producer signals, consumers wait, no coordinator required

## The Mechanism

### Provider Side: Shipping Capabilities

Projects declare and ship capabilities using labels:

```bash
# Declare intent (optional, for visibility)
bd create --title="Add --assignee to mol run" \
  --add-label=export:mol-run-assignee

# ... do work, close issue ...

# Ship the capability (adds provides: label)
bd ship mol-run-assignee
```

The `bd ship` command:
- Finds issue with `export:mol-run-assignee` label
- Validates issue is closed (or `--force`)
- Adds `provides:mol-run-assignee` label
- Optionally notifies downstream (future)

**Protected namespace**: `provides:*` labels can only be added via `bd ship`.

### Consumer Side: Declaring Dependencies

Projects declare external dependencies on capabilities:

```bash
# At creation
bd create --title="Use --assignee in spawn.go" \
  --blocked-by="external:beads:mol-run-assignee"

# Or later
bd update gt-xyz --blocked-by="external:beads:mol-run-assignee"
```

The `external:project:capability` syntax:
- `external:` - prefix indicating cross-project reference
- `project` - name from `external_projects` config
- `capability` - the capability name (matches `provides:X` label)

### Configuration

Projects configure paths to external projects:

```yaml
# .beads/config.yaml
external_projects:
  beads: ../beads
  gastown: ../gastown
  # Can also use absolute paths
  other: /Users/steve/projects/other
```

### Resolution

`bd ready` checks external dependencies:

```bash
bd ready
# gt-xyz: blocked by external:beads:mol-run-assignee (not provided)
# gt-abc: ready

# After beads ships the capability:
bd ready
# gt-xyz: ready
# gt-abc: ready
```

## Molecule Integration

### Parking a Molecule

When a polecat hits an external dependency mid-molecule:

```bash
# Polecat discovers external dep not satisfied
gt park --step=gt-mol.3 --waiting="beads:mol-run-assignee"
```

This command:
1. Adds `blocked_by: external:beads:mol-run-assignee` to the step
2. Clears assignee on the step and molecule root
3. Sends handoff mail to self with context
4. Shuts down the polecat

**"Parked" is a derived state**, not a new status:
- Molecule status: `in_progress`
- Molecule assignee: `null`
- Has step with unsatisfied external `blocked_by`

### Querying Parked Work

```bash
# Find parked molecules
bd list --status=in_progress --no-assignee --type=molecule

# See what's blocked and on what
bd list --has-external-block
```

### Resuming Parked Work

**Manual (launch):**
```bash
gt sling gt-mol-root
# Spawns polecat, which reads handoff mail and continues
```

**Automated (future):**
Deacon patrol checks parked molecules:
```yaml
- step: check-parked-molecules
  action: |
    For each molecule with status=in_progress, no assignee:
      Check if external deps are satisfied
      If yes: spawn polecat to resume
```

## Implementation Plan

### Phase 1: Beads Core (bd-* issues)

1. **bd ship command**: Add `provides:` label, protect namespace
2. **external: blocked_by**: Parse and store external references
3. **external_projects config**: Add to config schema
4. **bd ready resolution**: Check external deps via configured paths

### Phase 2: Gas Town Integration (gt-* issues)

1. **gt park command**: Set blocked_by, clear assignee, handoff, shutdown
2. **gt sling**: Resume parked molecule
3. **Patrol step**: Check parked molecules for unblocked

### Phase 3: Automation (future)

1. Push notifications on `bd ship`
2. Auto-resume via patrol
3. Cross-rig visibility in `gt status`

## Examples

### Full Flow: Adding --assignee to bd mol run

**In beads repo:**
```bash
# Dave creates the issue
bd create --title="Add --assignee flag to bd mol run" \
  --type=feature \
  --add-label=export:mol-run-assignee

# Dave implements, tests, closes
bd close bd-xyz

# Dave ships the capability
bd ship mol-run-assignee
# Output: Shipped mol-run-assignee (bd-xyz)
```

**In gastown repo:**
```bash
# Earlier: Joe created dependent issue
bd create --title="Use --assignee in spawn.go" \
  --blocked-by="external:beads:mol-run-assignee"

# bd ready showed it as blocked
bd ready
# gt-abc: blocked by external:beads:mol-run-assignee

# After Dave ships:
bd ready
# gt-abc: ready

# Joe picks it up
bd update gt-abc --status=in_progress --assignee=gastown/joe
```

### Parking Mid-Molecule

```bash
# Polecat working on molecule, hits step 3
# Step 3 needs beads:mol-run-assignee which isn't shipped

gt park --step=gt-mol.3 --waiting="beads:mol-run-assignee"
# Setting blocked_by on gt-mol.3...
# Clearing assignee on gt-mol.3...
# Clearing assignee on gt-mol-root...
# Sending handoff mail...
# Polecat shutting down.

# Later, after beads ships:
gt sling gt-mol-root
# Resuming molecule gt-mol-root...
# Reading handoff context...
# Continuing from step gt-mol.3
```

## Design Decisions

### Why capability labels, not issue references?

Referencing `beads:bd-xyz` couples consumer to producer's internal tracking.
Referencing `beads:mol-run-assignee` couples to a published interface.

The producer can refactor, split, or reimplement bd-xyz without breaking consumers.

### Why "parked" as derived state?

Adding a new status creates migration burden and complicates the state machine.
Deriving from existing fields (in_progress + no assignee + blocked) is simpler.

### Why handoff via mail?

Mail already handles context preservation. Parking is just a handoff to future-self
(or future-polecat). No new mechanism needed.

### Why config-based project resolution?

Alternatives considered:
- Git remote queries (complex, requires network)
- Hardcoded paths (inflexible)
- Central registry (single point of failure)

Config is simple, explicit, and works offline.
