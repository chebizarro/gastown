package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var hooksListCmd = &cobra.Command{
	Use:   "list",
	Short: "Show all managed settings.json locations",
	Long: `Show all managed .claude/settings.json locations and their sync status.

Displays each target with its override chain and whether it is
currently in sync with the base + overrides configuration.

Examples:
  gt hooks list            # Show all managed locations
  gt hooks list --json     # Output as JSON`,
	RunE: runHooksListTargets,
}

var hooksListJSON bool

func init() {
	hooksCmd.AddCommand(hooksListCmd)
	hooksListCmd.Flags().BoolVar(&hooksListJSON, "json", false, "Output as JSON")
}

func runHooksListTargets(cmd *cobra.Command, args []string) error {
	// TODO: Implement list/targets logic (gt-yvwbf)
	return fmt.Errorf("not yet implemented - see gt-yvwbf")
}
