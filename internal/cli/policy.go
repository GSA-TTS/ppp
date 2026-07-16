package cli

import "github.com/spf13/cobra"

// newPolicyCmd manages the network policy rules engine (spec §6.17).
func newPolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Manage network policy rules",
	}
	cmd.AddCommand(
		&cobra.Command{Use: "init <allow-all|balanced|deny-all>", Short: "Initialize the global policy", RunE: notImplemented},
		&cobra.Command{Use: "allow network RESOURCES", Short: "Add allow rules", RunE: notImplemented},
		&cobra.Command{Use: "deny network RESOURCES", Short: "Add deny rules", RunE: notImplemented},
		&cobra.Command{Use: "check network TARGET", Short: "Evaluate the rules against a target", RunE: notImplemented},
		&cobra.Command{Use: "ls [SANDBOX]", Short: "List active rules", RunE: notImplemented},
		&cobra.Command{Use: "inspect <policy-or-rule>", Short: "Show rule detail", RunE: notImplemented},
		&cobra.Command{Use: "log [SANDBOX]", Short: "Show the flow log", RunE: notImplemented},
		&cobra.Command{Use: "reset", Short: "Remove all custom rules", RunE: notImplemented},
		&cobra.Command{Use: "rm network", Short: "Remove rules", RunE: notImplemented},
	)
	return cmd
}
