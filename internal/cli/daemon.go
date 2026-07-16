package cli

import "github.com/spf13/cobra"

// newDaemonCmd manages the single mitmdump process (spec §6.15).
func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the single mitmdump proxy process",
	}
	cmd.AddCommand(
		&cobra.Command{Use: "start", Short: "Start the proxy daemon", RunE: notImplemented},
		&cobra.Command{Use: "stop", Short: "Stop the proxy daemon", RunE: notImplemented},
		&cobra.Command{Use: "status", Short: "Show proxy daemon status", RunE: notImplemented},
		&cobra.Command{Use: "log-level [target] [level]", Short: "Get or set log levels", RunE: notImplemented},
	)
	return cmd
}
