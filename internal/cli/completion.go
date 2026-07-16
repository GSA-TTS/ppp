package cli

import (
	"github.com/spf13/cobra"
)

// newCompletionCmd generates shell completion scripts (spec §6.14).
//
// It wraps Cobra's built-in completion generators for bash, zsh, fish, and
// powershell rather than reimplementing them.
func newCompletionCmd(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:                   "completion [bash|zsh|fish|powershell]",
		Short:                 "Generate shell completion script",
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			switch args[0] {
			case "bash":
				return root.GenBashCompletionV2(out, true)
			case "zsh":
				return root.GenZshCompletion(out)
			case "fish":
				return root.GenFishCompletion(out, true)
			case "powershell":
				return root.GenPowerShellCompletionWithDesc(out)
			default:
				// Unreachable: Args validation restricts to ValidArgs.
				return cmd.Usage()
			}
		},
	}
}
