# Gas Town Docker / Compose Guide

This guide describes running Gas Town in Docker with a **rig-rooted town root**:

- `GT_TOWN_ROOT=/gt/<rig>`

The compose stack runs three Gas Town containers:

1. **Deacon daemon**: `gt daemon run`
2. **API-mode agent loop**: `gt agentloop run` (Go-native loop)
3. **MCP server**: `gt mcp serve` (HTTP + SSE)

The bundled relay and Blossom services are development-only and run with the
`dev` profile.

## Host directory layout (required)

This compose setup bind-mounts a rig root and persists runtime spool state:

```text
rigs/<rig>/
  settings/
    nostr.json
    agents.json
runtime/<rig>/
  # created by GT: nostr-spool.jsonl, archives, etc.
```

- `rigs/<rig>` is mounted to `/gt/<rig>`
- `runtime/<rig>` is mounted to `/gt/<rig>/.runtime`

## Configuration files

### Nostr config

Compose expects:

- `${GT_TOWN_ROOT}/settings/nostr.json` inside the container
- `GT_NOSTR_CONFIG=/gt/<rig>/settings/nostr.json`

This file follows `internal/config.NostrConfig`.

### Agents config

Compose expects:

- `${GT_TOWN_ROOT}/settings/agents.json`

The entrypoint creates a default file with both:

- `claude-cli`, which runs the installed `claude` command in tmux for
  interactive Deacon, Witness, and Refinery roles
- `claude-api`, for the Go-native agent loop

Pass `ANTHROPIC_API_KEY` through Compose for either runtime. Existing
`agents.json` files are not overwritten; add this CLI preset if needed:

```json
{
  "name": "claude-cli",
  "provider_type": "cli",
  "command": "claude",
  "args": ["--dangerously-skip-permissions"],
  "process_names": ["claude"],
  "prompt_mode": "arg",
  "ready_delay_ms": 5000,
  "instructions_file": "CLAUDE.md"
}
```

## Running

```bash
cp .env.example .env
# Edit .env, ensure GT_MCP_TOKEN is set and keys are present if needed.

mkdir -p rigs/${GT_RIG}/settings runtime/${GT_RIG}
# Place rigs/${GT_RIG}/settings/nostr.json and agents.json

docker compose --profile dev up --build
```

The image starts as root only long enough to initialize and `chown` the rig
bind mounts, then uses `gosu` to run `gt` as the existing `gastown` user
(UID 10001).

## MCP server

- Exposed on: `http://localhost:9500`
- Health endpoint: `GET /mcp/health`
- SSE endpoint: `GET /mcp/sse` (requires Authorization when GT_MCP_TOKEN is set)

### Auth

If `GT_MCP_TOKEN` is non-empty, clients must send:

```
Authorization: Bearer <GT_MCP_TOKEN>
```

MCP publishes to host loopback by default
(`GT_MCP_BIND_HOST=127.0.0.1`). If you deliberately expose it by setting
`GT_MCP_BIND_HOST=0.0.0.0`, set a non-empty `GT_MCP_TOKEN`; unauthenticated
network exposure is unsupported.

## Fleet Nostr endpoints

The production defaults are:

- `GT_NOSTR_READ_RELAYS=wss://relay.sharegap.net`
- `GT_NOSTR_WRITE_RELAYS=wss://relay.sharegap.net`
- `GT_NOSTR_BLOSSOM_SERVERS=https://blossom.sharegap.net` (edge-01)

Override these variables for another deployment. Production does not depend on
the bundled services. For local development, `--profile dev` starts
`nostr-rs-relay` and `blossom-server`, and the development override selects
their Compose DNS endpoints.

You must also supply a reachable:

- **NIP-46 bunker** + bunker relay (WSS) for signing (production path)

Ensure containers can reach these over the network (outbound WSS/HTTPS).

## Dolt

The image includes the `dolt` binary. The Deacon defaults to its local Dolt
server at `127.0.0.1:3307`; the agentloop and MCP containers reach it through
the `deacon` Compose hostname. Set `GT_DOLT_HOST` and `GT_DOLT_PORT` to use an
external Dolt SQL server from every service.

When Dolt is managed remotely, disable the daemon's bundled Dolt-server patrol
in the rig's daemon settings:

```json
{
  "patrols": {
    "dolt_server": {
      "enabled": false
    }
  }
}
```

This is remote-Dolt operation, not a no-Dolt mode; Gastown still requires a
reachable Dolt server.

## Notes on agentloop behavior

The API-mode agent loop idles until it receives work via:

- `--task \"...\"` (seed an initial task), and/or
- `--prime-interval 30s` (periodically runs `gt prime` and assigns the returned task text if non-empty)

Without one of these, the agentloop container will run but remain idle.
