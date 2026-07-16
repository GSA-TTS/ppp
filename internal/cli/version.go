package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// version is the ppp CLI version. It defaults to "dev" and is intended to be
// overridden at build time via -ldflags "-X ...cli.version=<v>".
var version = "dev"

// newVersionCmd prints the ppp CLI version (spec §6.13).
//
// The full spec also reports podman/mitmdump versions and the detected
// platform/provider; those depend on the podman/proxy packages that are not
// implemented yet, so this stub prints just the CLI version for now.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the ppp version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "ppp %s\n", version)
			return err
		},
	}
}
