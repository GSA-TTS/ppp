package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// version is the ppp CLI version. It defaults to "dev" and is intended to be
// overridden at build time via -ldflags "-X ...cli.version=<v>".
var version = "dev"

// newVersionCmd prints the ppp CLI version (spec §6.13).
//
// The bare `ppp version` output is exactly "ppp <version>\n" — a stable
// contract other tooling (and TestVersionCmdPrintsVersion) depends on. Richer
// detail (platform, provider, and — once the host-only runner lands in T13 —
// podman/mitmdump versions) is gated behind --verbose so it never perturbs the
// default line.
func newVersionCmd() *cobra.Command {
	var verbose bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the ppp version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "ppp %s\n", version); err != nil {
				return err
			}
			if verbose {
				return outf(cmd.OutOrStdout(), "platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "print platform and provider detail")
	return cmd
}
