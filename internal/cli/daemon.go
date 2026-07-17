package cli

import (
	"github.com/spf13/cobra"
)

// newDaemonCmd manages the single mitmdump process (spec §6.15). Commands are
// driven through the Supervisor seam so tests inject a fake and the real host
// supervisor (spawn/kill mitmdump) can land in T13. `status` is fully wired in
// T12 (reads $PPP_DATA/proxy.pid); `start`/`stop` go through the interface.
func newDaemonCmd(d deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the single mitmdump proxy process",
	}
	cmd.AddCommand(
		newDaemonStartCmd(d),
		newDaemonStopCmd(d),
		newDaemonStatusCmd(d),
		&cobra.Command{Use: "log-level [target] [level]", Short: "Get or set log levels", RunE: notImplemented},
	)
	return cmd
}

func newDaemonStartCmd(d deps) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the proxy daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := d.newSupervisor().Start(); err != nil {
				return err
			}
			return outln(cmd.OutOrStdout(), "proxy started")
		},
	}
}

func newDaemonStopCmd(d deps) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the proxy daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := d.newSupervisor().Stop(); err != nil {
				return err
			}
			return outln(cmd.OutOrStdout(), "proxy stopped")
		},
	}
}

func newDaemonStatusCmd(d deps) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show proxy daemon status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := d.newSupervisor().Status()
			if err != nil {
				return err
			}
			if st.Running {
				return outf(cmd.OutOrStdout(), "proxy running (pid %d)\n", st.PID)
			}
			return outln(cmd.OutOrStdout(), "proxy not running")
		},
	}
}
