package cli

import "github.com/spf13/cobra"

// newRmCmd removes sandboxes and their VMs (spec §6.5).
func newRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm [SANDBOX...] [flags]",
		Short: "Remove one or more sandboxes",
		RunE:  notImplemented,
	}
}
