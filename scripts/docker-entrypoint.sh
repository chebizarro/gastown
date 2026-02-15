#!/bin/bash
set -e

# Ensure the workspace directories exist and are writable.
# Bind-mounted host directories may be owned by the host user,
# so we create subdirs as root before exec'ing gt.

GT_RIG="${GT_RIG:-gastown}"
GT_ROOT="/gt/${GT_RIG}"

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
  "default_agent": "claude"
}
EOF
fi

# Create default agents.json if missing (agentloop needs this)
if [ ! -f "${GT_ROOT}/settings/agents.json" ]; then
  cat > "${GT_ROOT}/settings/agents.json" <<EOF
{
  "version": 1,
  "agents": {
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

# Hand off to gt with whatever args were passed (e.g., "daemon run")
exec gt "$@"
