package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/GSA-TTS/ppp/internal/podman"
	"github.com/GSA-TTS/ppp/internal/proxy/portpool"
	"github.com/GSA-TTS/ppp/internal/sandbox"
)

// newRmCmd removes sandboxes and their VMs (spec §6.5).
//
// State + runner path is fully wired: it calls the PodmanRunner's Rm (Fake in
// tests records the argv), frees the WireGuard port, and removes the sandbox's
// state directory. Removing a live/unresponsive VM is the runner's concern
// (host-only in T13).
func newRmCmd(d deps) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "rm SANDBOX [SANDBOX...]",
		Short: "Remove one or more sandboxes",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runner := d.newRunner()
			for _, name := range args {
				if err := rmSandbox(runner, cmd, name, force); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "skip confirmation; remove even if the VM is unresponsive")
	return cmd
}

// rmSandbox removes one sandbox: runner Rm, free its port, delete its state
// dir — all under the state lock.
func rmSandbox(runner podman.PodmanRunner, cmd *cobra.Command, name string, force bool) error {
	if err := validateSandboxName(name); err != nil {
		return err
	}
	return sandbox.WithLock(func() error {
		if _, err := sandbox.Load(name); err != nil {
			return err
		}
		if err := runner.Rm(context.Background(), name, force); err != nil {
			return fmt.Errorf("podman machine rm: %w", err)
		}
		if err := freePort(name); err != nil {
			return err
		}
		dir, err := sandbox.SandboxDir(name)
		if err != nil {
			return err
		}
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("removing sandbox dir %q: %w", dir, err)
		}
		return outf(cmd.OutOrStdout(), "removed sandbox %s\n", name)
	})
}

// freePort releases the sandbox's WireGuard port from the registry. A sandbox
// that holds no port (e.g. registry already cleaned) is not an error.
func freePort(name string) error {
	regPath, err := portRegistryPath()
	if err != nil {
		return err
	}
	pool, err := portpool.New(regPath)
	if err != nil {
		return err
	}
	if err := pool.Free(name); err != nil {
		// Not fatal: the sandbox may not hold a port. Surface on stderr.
		fmt.Fprintf(os.Stderr, "ppp: freeing port for %q: %v\n", name, err)
	}
	return nil
}
