package cli

import "github.com/spf13/cobra"

// newRunCmd creates a sandbox and launches an agent inside it (spec §6.1).
func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run [flags] AGENT PATH [PATH...] [-- AGENT_ARGS...]",
		Short: "Create a sandbox and run an agent in it",
		RunE:  notImplemented,
	}
}
