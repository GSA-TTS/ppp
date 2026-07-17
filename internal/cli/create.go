package cli

import (
	"github.com/spf13/cobra"
)

// newCreateCmd creates a sandbox without launching the agent (spec §6.2).
//
// T12 wires the state-only path: it allocates a name + WireGuard port + inner
// IP, invokes the injected PodmanRunner to init the machine, and persists
// sandbox.json. The agent launch and full VM provisioning are host-only (T13).
func newCreateCmd(d deps) *cobra.Command {
	opts := createOptions{}
	var quiet bool
	cmd := &cobra.Command{
		Use:   "create [flags] AGENT PATH",
		Short: "Create a sandbox without launching the agent",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.agent = args[0]
			opts.workspace = args[1]
			box, err := provisionSandbox(d.newRunner(), opts, false)
			if err != nil {
				return err
			}
			if quiet {
				return outln(cmd.OutOrStdout(), box.Name)
			}
			return outf(cmd.OutOrStdout(), "created sandbox %s (port %d, inner IP %s)\n", box.Name, box.Port, box.InnerIP)
		},
	}
	addCreateFlags(cmd, &opts, &quiet)
	return cmd
}

// addCreateFlags registers the state-only creation flags shared by create/run.
func addCreateFlags(cmd *cobra.Command, opts *createOptions, quiet *bool) {
	cmd.Flags().StringVar(&opts.name, "name", "", "sandbox name (default: ppp-<adjective>-<noun>)")
	cmd.Flags().UintVar(&opts.cpus, "cpus", 0, "vCPU count (0 = podman default)")
	cmd.Flags().UintVarP(&opts.memoryMiB, "memory", "m", 0, "memory in MiB (0 = podman default)")
	if quiet != nil {
		cmd.Flags().BoolVarP(quiet, "quiet", "q", false, "print only the sandbox name")
	}
}
