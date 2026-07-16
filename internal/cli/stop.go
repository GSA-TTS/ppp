package cli

import "github.com/spf13/cobra"

// newStopCmd stops running sandboxes (spec §6.4).
func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop SANDBOX [SANDBOX...]",
		Short: "Stop one or more sandboxes",
		RunE:  notImplemented,
	}
}
