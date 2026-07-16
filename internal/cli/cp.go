package cli

import "github.com/spf13/cobra"

// newCpCmd copies files between host and sandbox (spec §6.7).
func newCpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cp [flags] SRC DST",
		Short: "Copy files between host and a sandbox",
		RunE:  notImplemented,
	}
}
