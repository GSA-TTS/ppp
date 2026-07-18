package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/GSA-TTS/ppp/internal/sandbox"
)

// newExecCmd executes a command inside a sandbox (spec §6.6).
//
// Everything after SANDBOX is the command + args to run in the guest, passed
// through verbatim. Use `--` to stop ppp from interpreting flags meant for the
// guest command, e.g. `ppp exec my-sandbox -- ls -la /`. The command is
// forwarded as an argv slice to podman (never a shell string); to run a shell
// pipeline, invoke the shell explicitly: `ppp exec s -- bash -lc '<script>'`.
func newExecCmd(d deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exec [flags] SANDBOX -- COMMAND [ARG...]",
		Short: "Execute a command in a sandbox",
		Args:  cobra.MinimumNArgs(2),
		// Do not parse flags after the sandbox name as ppp's own; they belong to
		// the guest command.
		DisableFlagParsing: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := validateSandboxName(name); err != nil {
				return err
			}
			if _, err := sandbox.Load(name); err != nil {
				return fmt.Errorf("sandbox %q: %w", name, err)
			}
			guestCmd := args[1:]
			// Cobra may retain the `--` terminator as a literal arg; drop a
			// single leading one so it isn't forwarded to the guest (SSHArgs
			// adds its own `--` separator).
			if len(guestCmd) > 0 && guestCmd[0] == "--" {
				guestCmd = guestCmd[1:]
			}
			if len(guestCmd) == 0 {
				return fmt.Errorf("a command to run in %q is required", name)
			}
			out, err := d.newRunner().SSH(context.Background(), name, guestCmd...)
			// Always surface captured output; exec mirrors the guest command's
			// exit status via the returned error.
			if len(out) > 0 {
				_, _ = cmd.OutOrStdout().Write(out)
			}
			if err != nil {
				return fmt.Errorf("exec in %q: %w", name, err)
			}
			return nil
		},
	}
	// Treat unknown flags after the sandbox as positional args for the guest
	// command rather than erroring on them.
	cmd.Flags().SetInterspersed(false)
	return cmd
}
