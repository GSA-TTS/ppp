package cli

import "github.com/spf13/cobra"

// newSetupCmd walks the host through required configuration (spec §6.9).
func newSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Detect and fix host configuration",
		RunE:  notImplemented,
	}
}
