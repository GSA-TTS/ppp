package cli

import "github.com/spf13/cobra"

// newDiagnoseCmd collects host and sandbox diagnostics (spec §6.11).
func newDiagnoseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diagnose [flags]",
		Short: "Collect host and sandbox diagnostics",
		RunE:  notImplemented,
	}
}
