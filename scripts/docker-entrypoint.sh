#!/bin/bash
set -euo pipefail

# Ensure the workspace directories exist and are writable.
# Bind-mounted host directories may be owned by the host user,
# so we create subdirs as root before exec'ing gt.

GT_RIG="${GT_RIG:-gastown}"
GT_ROOT="/gt/${GT_RIG}"

if [ "$(id -u)" -ne 0 ]; then
  exec gt "$@"
fi

# Create required subdirectories if they don't exist
mkdir -p \
  "${GT_ROOT}/mayor" \
  "${GT_ROOT}/daemon" \
  "${GT_ROOT}/settings" \
  "${GT_ROOT}/.runtime"

# Create minimal workspace marker if missing
if [ ! -f "${GT_ROOT}/mayor/town.json" ]; then
  cat > "${GT_ROOT}/mayor/town.json" <<EOF
{
  "type": "town-settings",
  "version": 1,
  "town_name": "${GT_RIG}",
  "default_agent": "claude-cli",
  "role_agents": {
    "deacon": "claude-cli",
    "witness": "claude-cli",
    "refinery": "claude-cli"
  }
}
EOF
fi

# Create default agents.json if missing (agentloop needs this)
if [ ! -f "${GT_ROOT}/settings/agents.json" ]; then
  cat > "${GT_ROOT}/settings/agents.json" <<EOF
{
  "version": 1,
  "agents": {
    "claude-cli": {
      "name": "claude-cli",
      "provider_type": "cli",
      "command": "claude",
      "args": ["--dangerously-skip-permissions"],
      "process_names": ["claude"],
      "prompt_mode": "arg",
      "ready_delay_ms": 5000,
      "instructions_file": "CLAUDE.md"
    },
    "claude-api": {
      "name": "claude-api",
      "provider_type": "api",
      "api": {
        "api_type": "anthropic",
        "model": "claude-sonnet-4-20250514",
        "api_key": "\$ANTHROPIC_API_KEY",
        "max_tokens": 8192
      }
    }
  }
}
EOF
fi

# Bind mounts are initialized as root, then all runtime work runs as UID 10001.
chown -R gastown:gastown "${GT_ROOT}"

# Hand off to gt with whatever args were passed (e.g., "daemon run").
exec gosu gastown gt "$@"
