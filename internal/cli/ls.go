package cli

import "github.com/spf13/cobra"

// newLsCmd lists sandboxes (spec §6.3).
func newLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List sandboxes",
		RunE:  notImplemented,
	}
}
