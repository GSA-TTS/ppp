package cli

import "github.com/spf13/cobra"

// newResetCmd tears down all sandboxes and ppp state (spec §6.10).
func newResetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reset [flags]",
		Short: "Destructively reset all sandboxes and ppp state",
		RunE:  notImplemented,
	}
}
