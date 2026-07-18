package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

// newRunCmd creates a sandbox and launches an agent inside it (spec §6.1).
//
// State bring-up (name + port + inner IP allocation, machine init/start,
// sandbox.json) always runs. When a live proxy daemon and VM are available,
// it then provisions the guest (WireGuard, CA, IPv6) and launches the agent
// container; on a host without those, provisioning surfaces a clear error
// (this is the host-only path exercised by the T14 e2e).
func newRunCmd(d deps) *cobra.Command {
	opts := createOptions{}
	var agentArgs []string
	cmd := &cobra.Command{
		Use:   "run [flags] AGENT PATH [-- AGENT_ARGS...]",
		Short: "Create a sandbox and run an agent in it",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.agent = args[0]
			opts.workspace = args[1]
			// Anything after the cobra `--` terminator is passed to the agent
			// verbatim (spec §6.1 AGENT_ARGS).
			if lenAfterDoubleDash := cmd.ArgsLenAtDash(); lenAfterDoubleDash >= 0 {
				agentArgs = args[lenAfterDoubleDash:]
			}

			runner := d.newRunner()
			box, err := provisionSandbox(runner, opts, true)
			if err != nil {
				return err
			}
			if err := outf(cmd.OutOrStdout(),
				"started sandbox %s (port %d, inner IP %s)\n", box.Name, box.Port, box.InnerIP); err != nil {
				return err
			}
			// Lazily ensure the proxy daemon is running (spec §9.4) so its
			// captured client configs exist before we provision the guest.
			if err := d.newSupervisor().Start(); err != nil {
				return fmt.Errorf("starting proxy daemon: %w", err)
			}
			err = provisionAndRun(context.Background(), runner, box, true, agentArgs)
			if errors.Is(err, errDaemonNotReady) {
				// The sandbox is created + started; provisioning just needs the
				// daemon. Surface a hint, not a hard failure.
				return outln(cmd.OutOrStdout(),
					"note: sandbox created but not provisioned — "+err.Error())
			}
			return err
		},
	}
	addCreateFlags(cmd, &opts, nil)
	return cmd
}
