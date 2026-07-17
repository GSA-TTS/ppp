package cli

import (
	"github.com/spf13/cobra"
)

// newRunCmd creates a sandbox and launches an agent inside it (spec §6.1).
//
// T12 wires the state-only bring-up: allocate name + port + inner IP, init and
// start the machine through the injected PodmanRunner, and persist sandbox.json
// in the running state. Launching the agent container (and full provisioning)
// requires a live VM and is host-only (T13); this command surfaces that clearly
// rather than pretending the agent ran.
func newRunCmd(d deps) *cobra.Command {
	opts := createOptions{}
	cmd := &cobra.Command{
		Use:   "run [flags] AGENT PATH",
		Short: "Create a sandbox and run an agent in it",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.agent = args[0]
			opts.workspace = args[1]
			box, err := provisionSandbox(d.newRunner(), opts, true)
			if err != nil {
				return err
			}
			if err := outf(cmd.OutOrStdout(),
				"started sandbox %s (port %d, inner IP %s)\n", box.Name, box.Port, box.InnerIP); err != nil {
				return err
			}
			return outln(cmd.OutOrStdout(),
				"note: launching the agent inside the VM requires a live host and is not implemented on this host yet (T13)")
		},
	}
	addCreateFlags(cmd, &opts, nil)
	return cmd
}
