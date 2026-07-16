package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// notImplemented is the shared RunE for scaffolding stubs. It prints a clear
// message and returns cleanly (no error), so `ppp <cmd>` exits 0 during the
// scaffolding phase.
func notImplemented(cmd *cobra.Command, _ []string) error {
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s: not implemented yet\n", cmd.CommandPath())
	return err
}
