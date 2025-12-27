# Gas Town Account Management Design

## Problem Statement

Claude Code users with multiple accounts (e.g., personal, work, team) need to:
1. Switch between accounts easily when quotas are exhausted
2. Set a global default account for all agents
3. Override per-spawn or per-role as needed

## Current State

- Claude Code stores config in `~/.claude/` and `~/.claude.json`
- Auth tokens stored in system keychain
- `CLAUDE_CONFIG_DIR` env var controls config location
- `/login` command switches accounts but overwrites in-place
- No built-in profile/multi-account support

## Proposed Solution

### Core Mechanism

Each registered account gets its own config directory:

```
~/.claude-accounts/
├── yegge/              # steve.yegge@gmail.com
│   ├── .claude/        # Full Claude Code config
│   └── .claude.json    # Root config file
├── ghosttrack/         # steve@ghosttrack.com
│   ├── .claude/
│   └── .claude.json
└── default -> ghosttrack/  # Symlink to current default
```

### Town Configuration

File: `~/gt/mayor/accounts.json`

This follows the existing pattern where town-level config lives in `mayor/`.

```json
{
  "version": 1,
  "accounts": {
    "yegge": {
      "email": "steve.yegge@gmail.com",
      "description": "Personal/Gmail account",
      "config_dir": "~/.claude-accounts/yegge"
    },
    "ghosttrack": {
      "email": "steve@ghosttrack.com",
      "description": "Ghost Track business account",
      "config_dir": "~/.claude-accounts/ghosttrack"
    }
  },
  "default": "ghosttrack"
}
```

### Environment Variable: GT_ACCOUNT

Highest priority override. Set this to use a specific account:

```bash
export GT_ACCOUNT=yegge
gt sling <bead> gastown  # Uses yegge account
```

### Command Interface

#### Account Management

```bash
# List registered accounts
gt account list
# Output:
#   yegge       steve.yegge@gmail.com     (personal)
# * ghosttrack  steve@ghosttrack.com      (default)

# Add new account (creates config dir, prompts login)
gt account add <handle> [email]
gt account add work steve@company.com

# Set default
gt account default <handle>
gt account default ghosttrack

# Remove account (keeps config dir by default)
gt account remove <handle>
gt account remove yegge --delete-config

# Check current/status
gt account status
# Output:
#   Current: ghosttrack (steve@ghosttrack.com)
#   GT_ACCOUNT env: not set
```

#### Spawn/Attach with Account Override

```bash
# Override for a specific spawn
gt sling <bead> gastown --account=yegge

# Override for crew attach
gt crew at --account=ghosttrack max

# With env var (highest precedence)
GT_ACCOUNT=yegge gt sling <bead> gastown
```

### Implementation Details

#### Account Resolution Order

1. `GT_ACCOUNT` environment variable (highest)
2. `--account` flag on command
3. `default` in accounts.yaml (lowest)

#### How Spawning Works

When `gt sling` or `gt crew at` runs Claude Code:

```go
func resolveAccountConfigDir() string {
    // Check env var first
    if handle := os.Getenv("GT_ACCOUNT"); handle != "" {
        return getAccountConfigDir(handle)
    }

    // Check flag
    if handle := flags.Account; handle != "" {
        return getAccountConfigDir(handle)
    }

    // Use default from config
    return getAccountConfigDir(config.Default)
}

func spawnClaudeCode(workdir string, account string) {
    configDir := resolveAccountConfigDir()

    cmd := exec.Command("claude", args...)
    cmd.Env = append(os.Environ(),
        fmt.Sprintf("CLAUDE_CONFIG_DIR=%s", configDir),
    )
    // ...
}
```

#### Account Login Flow

```bash
gt account add ghosttrack steve@ghosttrack.com
```

1. Creates `~/.claude-accounts/ghosttrack/`
2. Sets `CLAUDE_CONFIG_DIR` and runs `claude`
3. User completes `/login` with their account
4. Adds entry to `accounts.yaml`

### Security Considerations

- **No secrets in accounts.yaml** - Only handles and email addresses
- **Auth tokens in keychain** - Claude Code handles this per-config-dir
- **Config dir permissions** - Should be user-readable only

### Future Extensions

1. **Usage tracking** - `gt account status --usage` to show quota info
2. **Auto-switching** - When one account hits limits, prompt to switch
3. **Per-role defaults** - Different accounts for different roles:
   ```yaml
   role_defaults:
     witness: yegge     # Long-running patrol uses less quota
     refinery: ghosttrack
   ```

4. **API key accounts** - For when we support direct API access:
   ```yaml
   accounts:
     api-team:
       type: api_key
       key_ref: GT_API_KEY  # Env var containing key
   ```

## Migration Path

### Immediate (Manual)

Users can start using separate config dirs today:

```bash
# Set up account directories
mkdir -p ~/.claude-accounts/ghosttrack
export CLAUDE_CONFIG_DIR=~/.claude-accounts/ghosttrack
claude  # Login as ghosttrack
```

### Phase 1: Basic Support

- Add `accounts.json` parsing
- Add `gt account` subcommands
- Wire up `GT_ACCOUNT` env var in spawn

### Phase 2: Full Integration

- Add `--account` flags to all relevant commands
- Add status/usage tracking
- Add per-role defaults

## Testing Plan

1. Create test accounts config
2. Verify spawn uses correct config dir
3. Test override precedence
4. Test `gt account add` login flow
