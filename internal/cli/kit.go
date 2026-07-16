package cli

import "github.com/spf13/cobra"

// newKitCmd manages declarative kit artifacts (spec §6.16, experimental).
func newKitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kit",
		Short: "Manage declarative kit artifacts (experimental)",
	}
	cmd.AddCommand(
		&cobra.Command{Use: "add SANDBOX REFERENCE", Short: "Apply a kit to a sandbox", RunE: notImplemented},
		&cobra.Command{Use: "inspect REFERENCE", Short: "Display kit metadata", RunE: notImplemented},
		&cobra.Command{Use: "pack DIRECTORY", Short: "Pack a kit directory into a zip", RunE: notImplemented},
		&cobra.Command{Use: "pull REFERENCE", Short: "Pull a kit from an OCI registry", RunE: notImplemented},
		&cobra.Command{Use: "push DIRECTORY REFERENCE", Short: "Pack and push a kit to a registry", RunE: notImplemented},
		&cobra.Command{Use: "validate REFERENCE", Short: "Schema-validate a kit", RunE: notImplemented},
	)
	return cmd
}
