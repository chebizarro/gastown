# Gas Town Docker / Compose Guide

This guide describes running Gas Town in Docker with a **rig-rooted town root**:

- `GT_TOWN_ROOT=/gt/<rig>`

The compose stack runs three containers:

1. **Deacon daemon**: `gt daemon run`
2. **API-mode agent loop**: `gt agentloop run` (Go-native loop)
3. **MCP server**: `gt mcp serve` (HTTP + SSE)

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

### Agents config (API-mode)

Compose expects:

- `${GT_TOWN_ROOT}/settings/agents.json`

This file follows the schema shown in `docs/examples/agents-api.json`:

- `version`
- `agents` map
  - each agent has `provider_type: \"api\"` and an `api` block
  - `api.api_key` can reference an env var via `$OPENAI_API_KEY`, `$ANTHROPIC_API_KEY`, etc.

## Running

```bash
cp .env.example .env
# Edit .env, ensure GT_MCP_TOKEN is set and keys are present if needed.

mkdir -p rigs/${GT_RIG}/settings runtime/${GT_RIG}
# Place rigs/${GT_RIG}/settings/nostr.json and agents.json

docker compose up --build
```

## MCP server

- Exposed on: `http://localhost:9500`
- Health endpoint: `GET /mcp/health`
- SSE endpoint: `GET /mcp/sse` (requires Authorization when GT_MCP_TOKEN is set)

### Auth

If `GT_MCP_TOKEN` is non-empty, clients must send:

```
Authorization: Bearer <GT_MCP_TOKEN>
```

If it is empty, MCP allows all requests (development mode).

## Nostr dependencies (external)

This compose file does **not** run relays, bunkers, or Blossom servers. You must supply reachable endpoints:

- **Nostr relays** (WSS) for read/write
- **NIP-46 bunker** + bunker relay (WSS) for signing (production path)
- **Blossom servers** (HTTPS) for blob offloading (optional)

Ensure containers can reach these over the network (outbound WSS/HTTPS).

## Notes on agentloop behavior

The API-mode agent loop idles until it receives work via:

- `--task \"...\"` (seed an initial task), and/or
- `--prime-interval 30s` (periodically runs `gt prime` and assigns the returned task text if non-empty)

Without one of these, the agentloop container will run but remain idle.
