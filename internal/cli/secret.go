package cli

import "github.com/spf13/cobra"

// newSecretCmd manages keychain-backed secrets (spec §6.18).
func newSecretCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Manage keychain-backed secrets",
	}
	cmd.AddCommand(
		&cobra.Command{Use: "set [SERVICE]", Short: "Store a service secret", RunE: notImplemented},
		&cobra.Command{Use: "set-custom", Short: "Store a custom placeholder secret (experimental)", RunE: notImplemented},
		&cobra.Command{Use: "import [SERVICE]", Short: "Import secrets from host env vars", RunE: notImplemented},
		&cobra.Command{Use: "ls [SANDBOX]", Short: "List stored secrets (redacted)", RunE: notImplemented},
		&cobra.Command{Use: "rm [SERVICE]", Short: "Delete a secret", RunE: notImplemented},
	)
	return cmd
}
