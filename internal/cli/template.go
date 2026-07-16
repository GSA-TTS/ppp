package cli

import "github.com/spf13/cobra"

// newTemplateCmd manages saved VM disk templates (spec §6.19).
func newTemplateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "template",
		Short: "Manage saved VM disk templates",
	}
	cmd.AddCommand(
		&cobra.Command{Use: "save SANDBOX TAG", Short: "Snapshot a sandbox's disk as a template", RunE: notImplemented},
		&cobra.Command{Use: "load FILE", Short: "Import a saved template", RunE: notImplemented},
		&cobra.Command{Use: "ls", Short: "List registered templates", RunE: notImplemented},
		&cobra.Command{Use: "rm TAG|ID", Short: "Remove a template", RunE: notImplemented},
	)
	return cmd
}
