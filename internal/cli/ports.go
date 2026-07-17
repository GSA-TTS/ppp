package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/GSA-TTS/ppp/internal/proxy/portpool"
)

// newPortsCmd lists port allocations (spec §6.8).
//
// T12 wires the state-only listing: it reports the WireGuard port + inner IP
// allocations from the port registry, optionally filtered to one sandbox.
// Publishing/unpublishing host ports drives a live VM and is host-only (T13).
func newPortsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ports [SANDBOX]",
		Short: "List WireGuard port allocations",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			regPath, err := portRegistryPath()
			if err != nil {
				return err
			}
			pool, err := portpool.New(regPath)
			if err != nil {
				return err
			}
			return renderPorts(cmd, pool.Allocations(), firstArg(args))
		},
	}
}

// renderPorts writes an aligned SANDBOX/PORT/INNER_IP table, filtered to scope
// when non-empty.
func renderPorts(cmd *cobra.Command, allocs []portpool.Allocation, scope string) error {
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "SANDBOX\tPORT\tINNER_IP") //nolint:errcheck // tabwriter buffers; Flush reports the write error
	shown := 0
	for _, a := range allocs {
		if scope != "" && a.Sandbox != scope {
			continue
		}
		fmt.Fprintf(tw, "%s\t%d\t%s\n", a.Sandbox, a.Port, a.InnerIP) //nolint:errcheck // see above
		shown++
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if shown == 0 {
		return outln(cmd.OutOrStdout(), "no port allocations")
	}
	return nil
}
