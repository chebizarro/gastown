#!/usr/bin/env bash
# =============================================================================
# Gas Town Docker — Initial Setup Script
# =============================================================================
# Creates the directory structure and configuration files needed to run
# the Gas Town Docker stack.
#
# Usage:
#   ./scripts/docker-init.sh
#   ./scripts/docker-init.sh --with-relay-config
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

info()  { echo -e "${BLUE}[INFO]${NC} $*"; }
ok()    { echo -e "${GREEN}[OK]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }

cd "$PROJECT_DIR"

echo ""
echo "========================================="
echo "  Gas Town Docker Setup"
echo "========================================="
echo ""

# ---------------------------------------------------------------------------
# 1. Create data directories
# ---------------------------------------------------------------------------
info "Creating data directories..."

mkdir -p data/{gt,gt/settings,spool,keys}
chmod 700 data/keys

ok "Data directories created"

# ---------------------------------------------------------------------------
# 2. Create .env from example if not present
# ---------------------------------------------------------------------------
if [ ! -f .env ]; then
    info "Creating .env from .env.example..."
    cp .env.example .env
    ok ".env created — edit it to set your Nostr pubkey and bunker URI"
else
    ok ".env already exists"
fi

# ---------------------------------------------------------------------------
# 3. Create minimal nostr config if not present
# ---------------------------------------------------------------------------
NOSTR_CONFIG="data/gt/.nostr.json"
if [ ! -f "$NOSTR_CONFIG" ]; then
    info "Creating default Nostr config at $NOSTR_CONFIG..."
    cat > "$NOSTR_CONFIG" << 'NOSTR_EOF'
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
      "pubkey": "REPLACE_WITH_YOUR_HEX_PUBKEY",
      "signer": {
        "type": "nip46",
        "bunker": "REPLACE_WITH_BUNKER_URI"
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
NOSTR_EOF
    chmod 600 "$NOSTR_CONFIG"
    ok "Nostr config created — edit $NOSTR_CONFIG to set your pubkey and bunker URI"
else
    ok "Nostr config already exists at $NOSTR_CONFIG"
fi

# ---------------------------------------------------------------------------
# 4. Create relay config if requested
# ---------------------------------------------------------------------------
if [[ "${1:-}" == "--with-relay-config" ]]; then
    if [ ! -f config/relay.toml ]; then
        info "Relay config already provided at config/relay.toml"
        ok "Set RELAY_CONFIG=./config/relay.toml in .env to use it"
    else
        ok "Relay config exists at config/relay.toml"
    fi
fi

# ---------------------------------------------------------------------------
# 5. Generate MCP token if not set
# ---------------------------------------------------------------------------
if grep -q 'GT_MCP_TOKEN=$' .env 2>/dev/null || grep -q 'GT_MCP_TOKEN=""' .env 2>/dev/null; then
    TOKEN=$(openssl rand -hex 32 2>/dev/null || head -c 32 /dev/urandom | xxd -p -c 64)
    info "Generating MCP authentication token..."
    if [[ "$(uname)" == "Darwin" ]]; then
        sed -i '' "s/GT_MCP_TOKEN=$/GT_MCP_TOKEN=$TOKEN/" .env
        sed -i '' "s/GT_MCP_TOKEN=\"\"/GT_MCP_TOKEN=$TOKEN/" .env
    else
        sed -i "s/GT_MCP_TOKEN=$/GT_MCP_TOKEN=$TOKEN/" .env
        sed -i "s/GT_MCP_TOKEN=\"\"/GT_MCP_TOKEN=$TOKEN/" .env
    fi
    ok "MCP token generated and written to .env"
fi

# ---------------------------------------------------------------------------
# 6. Check Docker & Compose
# ---------------------------------------------------------------------------
info "Checking Docker..."

if ! command -v docker &> /dev/null; then
    error "Docker is not installed. See https://docs.docker.com/get-docker/"
    exit 1
fi

if ! docker info &> /dev/null; then
    error "Docker daemon is not running. Start Docker and try again."
    exit 1
fi

ok "Docker is available"

if docker compose version &> /dev/null; then
    ok "Docker Compose v2 is available"
else
    error "Docker Compose v2 not found. Update Docker or install the compose plugin."
    exit 1
fi

# ---------------------------------------------------------------------------
# 7. Check GPU availability (informational)
# ---------------------------------------------------------------------------
if command -v nvidia-smi &> /dev/null; then
    GPU_INFO=$(nvidia-smi --query-gpu=name --format=csv,noheader 2>/dev/null | head -1)
    ok "NVIDIA GPU detected: $GPU_INFO"
    info "Use 'docker compose --profile llm up -d' for GPU-accelerated LLM"
else
    info "No NVIDIA GPU detected. Use '--profile llm-cpu' for CPU-only Ollama"
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
echo "========================================="
echo "  Setup Complete!"
echo "========================================="
echo ""
echo "  Next steps:"
echo ""
echo "  1. Edit .env — set GT_NOSTR_PUBKEY and GT_NOSTR_BUNKER"
echo "  2. Edit data/gt/.nostr.json — replace placeholder pubkey/bunker"
echo "  3. Start the stack:"
echo ""
echo "     docker compose up -d"
echo ""
echo "  4. (Optional) Add Flotilla web UI:"
echo ""
echo "     docker compose --profile ui up -d"
echo ""
echo "  5. Check health:"
echo ""
echo "     docker compose ps"
echo "     docker compose logs -f gastown"
echo ""
echo "  Documentation: docs/DOCKER.md"
echo ""
