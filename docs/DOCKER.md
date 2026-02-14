# Gas Town Docker Deployment Guide

> Deploy the Nostr-native Gas Town orchestrator as a Docker stack.

---

## Table of Contents

1. [Quick Start](#quick-start)
2. [Architecture](#architecture)
3. [Services](#services)
4. [Configuration](#configuration)
5. [Deployment Scenarios](#deployment-scenarios)
6. [Volume Management](#volume-management)
7. [Networking](#networking)
8. [GPU / Local LLM Setup](#gpu--local-llm-setup)
9. [Production Deployment](#production-deployment)
10. [Monitoring & Troubleshooting](#monitoring--troubleshooting)
11. [Upgrading](#upgrading)

---

## Quick Start

```bash
# 1. Clone the repo
git clone https://github.com/steveyegge/gastown.git
cd gastown

# 2. Create environment file
cp .env.example .env
# Edit .env — at minimum set GT_NOSTR_PUBKEY and GT_NOSTR_BUNKER

# 3. Create data directories
mkdir -p data/{gt,spool,keys}

# 4. Start core services (gastown + relay + blossom)
docker compose up -d

# 5. Verify
docker compose ps
docker compose logs -f gastown

# 6. (Optional) Add Flotilla web UI
docker compose --profile ui up -d
```

Open Flotilla at [http://localhost:1847](http://localhost:1847).

---

## Architecture

```
                    ┌─────────────────────────────────────────┐
                    │              Docker Network (gt_net)     │
                    │                                          │
┌──────────┐       │  ┌──────────┐   ┌─────────┐   ┌───────┐ │
│ Browser  │◄──────┼──┤ Flotilla │   │  Relay  │   │Blossom│ │
│ (Flotilla│       │  │ :1847    │──►│  :7000  │   │ :3000 │ │
│  UI)     │       │  └──────────┘   └────┬────┘   └───┬───┘ │
└──────────┘       │                      │             │     │
                    │              ┌───────┴─────────────┘     │
                    │              │                            │
                    │         ┌────┴─────┐                     │
                    │         │ Gas Town │    ┌────────┐       │
                    │         │  (gt)    │────│ Ollama │       │
                    │         │  :9500   │    │ :11434 │       │
                    │         └──────────┘    └────────┘       │
                    │              │                            │
                    └──────────────┼────────────────────────────┘
                                   │
                          ┌────────┴────────┐
                          │  Host Volumes   │
                          │  ./data/gt      │ ← rigs, beads, configs
                          │  ./data/spool   │ ← offline event buffer
                          │  ./data/keys    │ ← NIP-46 metadata
                          │  ~/.ssh         │ ← git SSH keys (ro)
                          └─────────────────┘
```

---

## Services

### Core (always started)

| Service | Image | Port | Description |
|---------|-------|------|-------------|
| `gastown` | Built from `Dockerfile` | 9500 (MCP) | GT daemon — orchestrator, dual-write publisher |
| `relay` | `scsibug/nostr-rs-relay` | 7777 | Local Nostr relay for GT events |
| `blossom` | `ghcr.io/hzrd149/blossom-server` | 8005 | Content-addressed blob storage |

### Optional Profiles

| Service | Profile | Port | Description |
|---------|---------|------|-------------|
| `flotilla` | `ui` | 1847 | Flotilla/Budabit web UI |
| `ollama` | `llm` | 11434 | Ollama with GPU support |
| `ollama-cpu` | `llm-cpu` | 11434 | Ollama without GPU (CPU-only) |

Enable profiles with `--profile`:
```bash
# Web UI only
docker compose --profile ui up -d

# Web UI + GPU LLM
docker compose --profile ui --profile llm up -d

# CPU-only LLM (no GPU)
docker compose --profile llm-cpu up -d
```

---

## Configuration

### Environment Variables

Copy `.env.example` to `.env` and configure:

```bash
cp .env.example .env
```

**Required variables:**

| Variable | Description | Example |
|----------|-------------|---------|
| `GT_NOSTR_PUBKEY` | Deacon's hex pubkey | `abc123def456...` |
| `GT_NOSTR_BUNKER` | NIP-46 bunker URI | `bunker://npub1...?relay=wss://...` |

**Optional variables:**

| Variable | Default | Description |
|----------|---------|-------------|
| `GT_NOSTR_ENABLED` | `1` | Enable Nostr publishing |
| `GT_HOME` | `./data/gt` | Host path for GT workspace |
| `RELAY_PORT` | `7777` | Host port for relay |
| `BLOSSOM_PORT` | `8005` | Host port for Blossom |
| `FLOTILLA_PORT` | `1847` | Host port for Flotilla |
| `OLLAMA_PORT` | `11434` | Host port for Ollama |
| `GT_MCP_PORT` | `9500` | Host port for MCP server |
| `GT_MCP_TOKEN` | _(empty)_ | Bearer token for MCP auth |

See `.env.example` for the complete list.

### Relay Configuration

For custom relay settings, edit `config/relay.toml` and set in `.env`:

```bash
RELAY_CONFIG=./config/relay.toml
```

Key settings:
- **`max_event_bytes`** — Increase if issue mirrors exceed 1MB
- **`pubkey_whitelist`** — Restrict writes to known Gas Town agent pubkeys
- **`nip42_auth`** — Enable NIP-42 authentication for writes

### Nostr Config File

The container expects `~/gt/.nostr.json` inside the mounted `GT_HOME` volume. Create it before starting:

```bash
cat > data/gt/.nostr.json << 'EOF'
{
  "version": 1,
  "enabled": true,
  "relays": {
    "read": ["ws://relay:7000"],
    "write": ["ws://relay:7000"]
  },
  "blossom_servers": ["http://blossom:3000"],
  "identities": {
    "deacon": {
      "pubkey": "YOUR_HEX_PUBKEY_HERE",
      "signer": {
        "type": "nip46",
        "bunker": "bunker://npub1...?relay=wss://relay.example.com"
      },
      "profile": {
        "name": "Deacon",
        "about": "Gas Town orchestrator",
        "bot": true
      }
    }
  },
  "defaults": {
    "heartbeat_interval": "30s",
    "spool_drain_interval": "10s",
    "convoy_recompute_interval": "60s",
    "issue_mirror_interval": "120s"
  }
}
EOF
```

> **Note:** Inside the Docker network, services reference each other by container name (`relay`, `blossom`), not `localhost`.

---

## Deployment Scenarios

### Scenario 1: Single Machine (Developer)

Everything on one machine, CLI agents running locally alongside Docker:

```bash
# Start infrastructure only
docker compose up -d relay blossom

# Run gt natively (not in Docker) — it connects to the relay
export GT_NOSTR_ENABLED=1
export GT_NOSTR_WRITE_RELAYS=ws://localhost:7777
export GT_NOSTR_BLOSSOM_SERVERS=http://localhost:8005
gt daemon start
```

### Scenario 2: Full Docker Stack

Everything in Docker, including the gt daemon:

```bash
docker compose up -d                      # Core
docker compose --profile ui up -d         # + Web UI
```

### Scenario 3: Multi-Machine with LAN LLM

Gas Town on Machine A, Ollama on Machine B (GPU server):

**Machine A** (orchestrator):
```bash
docker compose up -d relay blossom gastown
```

**Machine B** (GPU server):
```bash
# Run Ollama standalone
docker run -d --gpus all -p 11434:11434 ollama/ollama
ollama pull llama3.1:70b
```

**Machine A** — Configure Gas Town to use the LAN LLM:
```bash
# In data/gt/settings/agents.json
{
  "agents": {
    "lan-llama": {
      "provider_type": "api",
      "api_url": "http://192.168.1.50:11434/v1",
      "model": "llama3.1:70b"
    }
  }
}

# In data/gt/settings/config.json
{
  "role_agents": {
    "polecat": "lan-llama",
    "witness": "lan-llama"
  }
}
```

### Scenario 4: External Relays

Connect to public relays for cross-network visibility:

```bash
# .env
GT_NOSTR_READ_RELAYS=ws://relay:7000,wss://relay.damus.io,wss://nos.lol
GT_NOSTR_WRITE_RELAYS=ws://relay:7000,wss://relay.damus.io

# PUBLIC_RELAY_URL for Flotilla browser connections
PUBLIC_RELAY_URL=wss://your-domain.com
```

---

## Volume Management

### Data Directories

```
data/
├── gt/                  # Gas Town workspace (rigs, beads, configs)
│   ├── .nostr.json      # Nostr configuration
│   ├── settings/         # Town settings
│   ├── rigs/            # Project repositories
│   └── .beads/          # Town-level issue tracking
├── spool/               # Nostr event spool (offline buffer)
└── keys/                # NIP-46 identity metadata
```

### Backup

```bash
# Stop services
docker compose down

# Backup
tar czf gastown-backup-$(date +%Y%m%d).tar.gz data/

# Restore
tar xzf gastown-backup-20260213.tar.gz
docker compose up -d
```

### Spool Maintenance

The spool directory buffers events during relay outages. It drains automatically when relays reconnect. To check spool status:

```bash
# Inside the container
docker compose exec gastown gt nostr health

# Or check the spool file directly
wc -l data/spool/*.jsonl
```

To manually clear a drained spool:
```bash
rm -f data/spool/*.jsonl.drained
```

---

## Networking

### Internal DNS

Within the `gt_net` Docker network, services resolve by container name:

| Container | Internal URL |
|-----------|-------------|
| `gt_relay` | `ws://relay:7000` |
| `gt_blossom` | `http://blossom:3000` |
| `gt_ollama` | `http://ollama:11434` |
| `gastown` | `http://gastown:9500` |

### External Access

| Service | Host URL | Purpose |
|---------|----------|---------|
| Relay | `ws://localhost:7777` | Flotilla/external Nostr clients |
| Blossom | `http://localhost:8005` | Blob retrieval |
| Flotilla | `http://localhost:1847` | Web UI |
| MCP | `http://localhost:9500` | LAN agent tool calls |
| Ollama | `http://localhost:11434` | LLM API |

### Exposing to LAN

To make services accessible from other machines on your network:

```bash
# .env — bind to all interfaces
RELAY_PORT=0.0.0.0:7777:7000
FLOTILLA_PORT=0.0.0.0:1847:1847
GT_MCP_PORT=0.0.0.0:9500:9500
```

Or use the host machine's IP directly in client configs.

---

## GPU / Local LLM Setup

### With NVIDIA GPU

```bash
# Ensure nvidia-container-toolkit is installed
# See: https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/install-guide.html

# Start with GPU profile
docker compose --profile llm up -d

# Pull a model
docker compose exec ollama ollama pull llama3.1:70b

# Verify
docker compose exec ollama ollama list
curl http://localhost:11434/api/tags
```

### Without GPU (CPU-only)

```bash
docker compose --profile llm-cpu up -d
docker compose exec ollama-cpu ollama pull llama3.2:3b  # Use smaller models
```

### Configure Gas Town to Use Ollama

Add to `data/gt/settings/agents.json`:

```json
{
  "agents": {
    "ollama-local": {
      "provider_type": "api",
      "api_url": "http://ollama:11434/v1",
      "model": "llama3.1:70b",
      "supports_tools": true,
      "max_tokens": 8192
    }
  }
}
```

Then assign roles:
```json
{
  "role_agents": {
    "polecat": "ollama-local",
    "witness": "ollama-local",
    "mayor": "claude"
  }
}
```

---

## Production Deployment

### Using Production Overrides

```bash
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
```

Production changes:
- Uses pre-built images (no build step)
- Resource limits (CPU/memory)
- JSON file logging with rotation
- Sunset flags disabled (Nostr-only)
- Restart policy: `always`

### Building and Pushing Images

```bash
# Build with version tag
export GT_VERSION=$(git describe --tags --always)
docker compose build --build-arg GT_VERSION=$GT_VERSION

# Tag and push (if using a registry)
docker tag gastown:latest registry.example.com/gastown:$GT_VERSION
docker push registry.example.com/gastown:$GT_VERSION
```

### TLS / Reverse Proxy

For production, put a reverse proxy (nginx/caddy) in front:

```nginx
# /etc/nginx/conf.d/gastown.conf
server {
    listen 443 ssl;
    server_name relay.yourdomain.com;

    ssl_certificate /etc/letsencrypt/live/relay.yourdomain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/relay.yourdomain.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:7777;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_read_timeout 86400;
    }
}

server {
    listen 443 ssl;
    server_name gt.yourdomain.com;

    ssl_certificate /etc/letsencrypt/live/gt.yourdomain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/gt.yourdomain.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:1847;
        proxy_set_header Host $host;
    }
}
```

---

## Monitoring & Troubleshooting

### Health Checks

```bash
# Service status
docker compose ps

# Gas Town health
docker compose exec gastown gt doctor

# Nostr health
docker compose exec gastown gt nostr health

# Relay check
curl -s http://localhost:7777 | jq .

# Blossom check
curl -s http://localhost:8005/
```

### Logs

```bash
# All services
docker compose logs -f

# Single service
docker compose logs -f gastown
docker compose logs -f relay

# Last 100 lines
docker compose logs --tail=100 gastown
```

### Common Issues

#### "relay: connection refused"

The gastown container starts before the relay is ready. The `depends_on` with `service_healthy` should handle this, but if not:

```bash
docker compose restart gastown
```

#### "permission denied" on volumes

The gastown container runs as user `gt` (UID 1000). Ensure host directories are writable:

```bash
sudo chown -R 1000:1000 data/
```

#### Spool growing unboundedly

Check relay connectivity:
```bash
docker compose exec gastown gt nostr health
docker compose logs relay | tail -20
```

If the relay is down, the spool will buffer events. Fix the relay and the spool will drain automatically.

#### Out of disk space

```bash
# Check Docker disk usage
docker system df

# Clean up
docker system prune -f
docker volume prune -f  # CAUTION: removes unused volumes
```

---

## Upgrading

### Upgrade Gas Town

```bash
# Pull latest source
git pull origin main

# Rebuild
docker compose build gastown

# Restart (zero-downtime with relay/blossom staying up)
docker compose up -d gastown
```

### Upgrade Relay / Blossom

```bash
docker compose pull relay blossom
docker compose up -d relay blossom
```

### Upgrade Flotilla

```bash
cd ../flotilla-extensions/flotilla
git pull origin main
cd ../gastown
docker compose --profile ui build flotilla
docker compose --profile ui up -d flotilla
```

---

## Related Documentation

- [NOSTR.md](NOSTR.md) — Nostr integration configuration reference
- [INSTALLING.md](INSTALLING.md) — Native (non-Docker) installation
- [docs/design/nostr-protocol.md](design/nostr-protocol.md) — Protocol specification
- [docs/design/nostr-architecture.md](design/nostr-architecture.md) — Package architecture
- [docs/examples/](examples/) — Agent configuration examples
