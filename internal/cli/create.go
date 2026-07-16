package cli

import "github.com/spf13/cobra"

// newCreateCmd creates a sandbox without launching the agent (spec §6.2).
func newCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create [flags] AGENT PATH [PATH...]",
		Short: "Create a sandbox without launching the agent",
		RunE:  notImplemented,
	}
}
