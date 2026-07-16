package cli

import "github.com/spf13/cobra"

// newPortsCmd lists or manages published ports for a sandbox (spec §6.8).
func newPortsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ports SANDBOX [flags]",
		Short: "List or manage published ports for a sandbox",
		RunE:  notImplemented,
	}
}
