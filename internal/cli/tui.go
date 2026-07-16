package cli

import "github.com/spf13/cobra"

// newTuiCmd launches the Bubbletea dashboard (spec §6.12).
func newTuiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Launch the interactive dashboard",
		RunE:  notImplemented,
	}
}
