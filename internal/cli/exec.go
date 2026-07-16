package cli

import "github.com/spf13/cobra"

// newExecCmd executes a command inside a sandbox (spec §6.6).
func newExecCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "exec [flags] SANDBOX COMMAND [ARG...]",
		Short: "Execute a command in a sandbox",
		RunE:  notImplemented,
	}
}
