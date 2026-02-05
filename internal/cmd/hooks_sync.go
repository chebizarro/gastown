package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	hooksSyncDryRun bool
	hooksSyncForce  bool
)

var hooksSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Regenerate all .claude/settings.json files",
	Long: `Regenerate all .claude/settings.json files from the base config and overrides.

For each target (mayor, deacon, rig/crew, rig/witness, etc.):
1. Load base config
2. Apply role override (if exists)
3. Apply rig+role override (if exists)
4. Merge hooks section into existing settings.json (preserving non-hooks fields)
5. Write updated settings.json

Examples:
  gt hooks sync             # Regenerate all settings.json files
  gt hooks sync --dry-run   # Show what would change without writing
  gt hooks sync --force     # Overwrite even if local modifications detected`,
	RunE: runHooksSync,
}

func init() {
	hooksCmd.AddCommand(hooksSyncCmd)
	hooksSyncCmd.Flags().BoolVar(&hooksSyncDryRun, "dry-run", false, "Show what would change without writing")
	hooksSyncCmd.Flags().BoolVar(&hooksSyncForce, "force", false, "Overwrite even if local modifications detected")
}

func runHooksSync(cmd *cobra.Command, args []string) error {
	// TODO: Implement sync logic (gt-pga9m)
	// 1. DiscoverTargets(townRoot) to find all settings.json locations
	// 2. For each target: load base, apply overrides, merge, write
	return fmt.Errorf("not yet implemented - see gt-pga9m")
}
