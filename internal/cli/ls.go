package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/GSA-TTS/ppp/internal/sandbox"
)

// newLsCmd lists sandboxes read from $PPP_DATA/sandboxes/*/sandbox.json
// (spec §6.3). It is state-only: it does not query the live Podman Machine
// list (that enrichment lands with the host-only runner in T13), so STATUS
// reflects the persisted sandbox.json record.
func newLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List sandboxes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			boxes, err := loadAllSandboxes()
			if err != nil {
				return err
			}
			if len(boxes) == 0 {
				return outln(cmd.OutOrStdout(), "no sandboxes")
			}
			return renderSandboxTable(cmd, boxes)
		},
	}
}

// renderSandboxTable writes an aligned NAME/AGENT/STATUS/PORT/WORKSPACE table.
func renderSandboxTable(cmd *cobra.Command, boxes []sandbox.Sandbox) error {
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tAGENT\tSTATUS\tPORT\tWORKSPACE") //nolint:errcheck // tabwriter buffers; Flush reports the write error
	for _, b := range boxes {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n", b.Name, b.Agent, b.Status, b.Port, b.Workspace) //nolint:errcheck // see above
	}
	return tw.Flush()
}

// loadAllSandboxes reads every sandbox.json under $PPP_DATA/sandboxes/, sorted
// by name. A missing sandboxes directory yields an empty slice (not an error):
// a fresh install simply has no sandboxes.
func loadAllSandboxes() ([]sandbox.Sandbox, error) {
	dataDir, err := sandbox.ResolveDataDir()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(dataDir, "sandboxes")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading sandboxes dir %q: %w", root, err)
	}
	var boxes []sandbox.Sandbox
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b, err := sandbox.Load(e.Name())
		if err != nil {
			// A dir without a valid sandbox.json is skipped rather than
			// failing the whole listing; surface it on stderr for visibility.
			fmt.Fprintf(os.Stderr, "ppp: skipping %q: %v\n", e.Name(), err)
			continue
		}
		boxes = append(boxes, b)
	}
	sort.Slice(boxes, func(i, j int) bool { return boxes[i].Name < boxes[j].Name })
	return boxes, nil
}
