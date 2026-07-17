package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/GSA-TTS/ppp/internal/podman"
	"github.com/GSA-TTS/ppp/internal/sandbox"
)

// newStopCmd stops running sandboxes (spec §6.4).
//
// State + runner path is fully wired: it transitions the sandbox to "stopped"
// and calls the PodmanRunner's Stop (Fake in tests records the argv). The
// WireGuard port stays allocated so `run --name` can reattach. Halting a real
// VM is the runner's concern (host-only in T13).
func newStopCmd(d deps) *cobra.Command {
	return &cobra.Command{
		Use:   "stop SANDBOX [SANDBOX...]",
		Short: "Stop one or more sandboxes",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runner := d.newRunner()
			for _, name := range args {
				if err := stopSandbox(runner, cmd, name); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

// stopSandbox transitions one sandbox to stopped and asks the runner to stop
// the machine, all under the state lock.
func stopSandbox(runner podman.PodmanRunner, cmd *cobra.Command, name string) error {
	if err := validateSandboxName(name); err != nil {
		return err
	}
	return sandbox.WithLock(func() error {
		box, err := sandbox.Load(name)
		if err != nil {
			return err
		}
		if box.Status == sandbox.StatusStopped {
			return outf(cmd.OutOrStdout(), "sandbox %s already stopped\n", name)
		}
		if err := box.Transition(sandbox.StatusStopped); err != nil {
			return err
		}
		if err := runner.Stop(context.Background(), name); err != nil {
			return fmt.Errorf("podman machine stop: %w", err)
		}
		if err := box.Save(); err != nil {
			return err
		}
		return outf(cmd.OutOrStdout(), "stopped sandbox %s\n", name)
	})
}
