package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var hooksDiffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Show what sync would change",
	Long: `Show what 'gt hooks sync' would change without applying.

Compares the current .claude/settings.json files against what would
be generated from base + overrides. Uses color to highlight additions
and removals.

Exit codes:
  0 - No changes pending
  1 - Changes would be applied

Examples:
  gt hooks diff                    # Show all pending changes
  gt hooks diff gastown/crew       # Show changes for specific target`,
	RunE: runHooksDiff,
}

func init() {
	hooksCmd.AddCommand(hooksDiffCmd)
}

func runHooksDiff(cmd *cobra.Command, args []string) error {
	// TODO: Implement diff logic (gt-yvwbf)
	return fmt.Errorf("not yet implemented - see gt-yvwbf")
}
