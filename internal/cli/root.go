// Package cli wires up the ppp Cobra command tree.
//
// This is the CLI/orchestrator layer: the root command plus one file per
// top-level command (spec §6/§7). At this scaffolding stage most commands are
// stubs that report "not implemented yet"; they establish the v1 subcommand
// surface so `ppp --help` reflects the intended shape of the tool.
package cli

import (
	"github.com/spf13/cobra"
)

// debug is toggled by the persistent --debug/-D flag. Reserved for future
// verbose logging wiring; commands may read it once real behavior lands.
var debug bool

// NewRootCmd builds the root `ppp` command with all subcommands attached,
// wired over the production dependency set (real PodmanRunner/secret Store/proxy
// Supervisor). Tests build a root via newRootCmd(deps) with injected fakes.
func NewRootCmd() *cobra.Command {
	return newRootCmd(defaultDeps())
}

// newRootCmd builds the root command over the given dependency set. It is the
// single seam through which tests inject a podman.Fake, an in-memory secret
// Store, and a fake Supervisor, so no command touches a real VM, keychain, or
// proxy process during tests.
func newRootCmd(d deps) *cobra.Command {
	root := &cobra.Command{
		Use:   "ppp",
		Short: "Podman Plus Proxy — run AI coding agents in isolated sandboxes",
		Long: `ppp (Podman Plus Proxy) runs AI coding agents inside isolated,
policy-controlled sandboxes. Each sandbox is a dedicated Podman Machine microVM
whose egress is transparently tunneled through a single host-side mitmproxy that
enforces network policy and injects secrets so credentials never enter the
sandbox.`,
		SilenceUsage:  true,
		SilenceErrors: false,
	}

	root.PersistentFlags().BoolVarP(&debug, "debug", "D", false, "enable debug logging")

	addCommands(root, d)
	return root
}

// addCommands attaches every v1 top-level command to root.
func addCommands(root *cobra.Command, d deps) {
	root.AddCommand(
		newRunCmd(d),
		newCreateCmd(d),
		newLsCmd(),
		newStopCmd(d),
		newRmCmd(d),
		newExecCmd(d),
		newCpCmd(),
		newPortsCmd(),
		newSetupCmd(),
		newResetCmd(),
		newDiagnoseCmd(),
		newTuiCmd(),
		newVersionCmd(),
		newDaemonCmd(d),
		newKitCmd(),
		newPolicyCmd(),
		newSecretCmd(d),
		newTemplateCmd(),
		newCompletionCmd(root),
	)
}

// Execute builds the root command and runs it.
func Execute() error {
	return NewRootCmd().Execute()
}
