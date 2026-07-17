package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/GSA-TTS/ppp/internal/policy"
)

// newPolicyCmd manages the network policy rules engine (spec §6.17).
func newPolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Manage network policy rules",
	}
	cmd.AddCommand(
		newPolicyInitCmd(),
		newPolicyAllowCmd(),
		newPolicyDenyCmd(),
		newPolicyCheckCmd(),
		newPolicyLsCmd(),
		newPolicyInspectCmd(),
		newPolicyResetCmd(),
		newPolicyRmCmd(),
		&cobra.Command{Use: "log [SANDBOX]", Short: "Show the flow log", RunE: notImplemented},
	)
	return cmd
}

// newPolicyInitCmd writes a preset policy to $PPP_CONFIG/policies.yaml.
func newPolicyInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:       "init <allow-all|balanced|deny-all>",
		Short:     "Initialize the global policy",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"allow-all", "balanced", "deny-all"},
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := globalPolicyPath()
			if err != nil {
				return err
			}
			if err := writePreset(path, args[0]); err != nil {
				return err
			}
			return outf(cmd.OutOrStdout(), "initialized %s policy at %s\n", args[0], path)
		},
	}
}

// newPolicyAllowCmd appends allow rules to the effective policy.
func newPolicyAllowCmd() *cobra.Command {
	return newPolicyMutateCmd("allow", "Add allow rules")
}

// newPolicyDenyCmd appends deny rules to the effective policy.
func newPolicyDenyCmd() *cobra.Command {
	return newPolicyMutateCmd("deny", "Add deny rules")
}

// newPolicyMutateCmd builds the shared `allow network`/`deny network` command.
func newPolicyMutateCmd(decision, short string) *cobra.Command {
	var sandboxName string
	cmd := &cobra.Command{
		Use:   decision + " network RESOURCES...",
		Short: short,
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] != "network" {
				return fmt.Errorf("unsupported rule type %q (only \"network\" is supported)", args[0])
			}
			resources, err := splitResources(args[1:])
			if err != nil {
				return err
			}
			path, err := policyPathFor(sandboxName)
			if err != nil {
				return err
			}
			added, err := appendRules(path, decision, resources)
			if err != nil {
				return err
			}
			for _, id := range added {
				if err := outf(cmd.OutOrStdout(), "added %s rule %s\n", decision, id); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&sandboxName, "sandbox", "", "scope the rule to a sandbox (default: global)")
	return cmd
}

// newPolicyCheckCmd is the demonstrable vertical thread: load the effective
// policy, parse the target, evaluate, and print the decision plus the matched
// rule (or "default"). It never mutates state and needs no VM or daemon.
func newPolicyCheckCmd() *cobra.Command {
	var sandboxName string
	cmd := &cobra.Command{
		Use:   "check network TARGET",
		Short: "Evaluate the rules against a target",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] != "network" {
				return fmt.Errorf("unsupported check type %q (only \"network\" is supported)", args[0])
			}
			return runPolicyCheck(cmd, sandboxName, args[1])
		},
	}
	cmd.Flags().StringVar(&sandboxName, "sandbox", "", "check against a sandbox policy (default: global)")
	return cmd
}

// runPolicyCheck loads the effective policy, evaluates the target, and prints
// the outcome. A missing policy file fails closed (deny-all), consistent with
// policy.LoadFile.
func runPolicyCheck(cmd *cobra.Command, sandboxName, targetArg string) error {
	target, err := policy.ParseTarget(targetArg)
	if err != nil {
		return err
	}
	pol, err := loadEffectivePolicy(sandboxName)
	if err != nil {
		return err
	}
	result := pol.Evaluate(target)
	verdict := "ALLOWED"
	if result.Decision == policy.Deny {
		verdict = "DENIED"
	}
	if result.Matched != nil {
		return outf(cmd.OutOrStdout(), "%s %s (matched rule %s: %s %s)\n",
			verdict, targetArg, result.Matched.ID, result.Matched.Decision, result.Matched.Resource)
	}
	return outf(cmd.OutOrStdout(), "%s %s (default: %s)\n", verdict, targetArg, pol.Default())
}

// loadEffectivePolicy loads the policy for the given scope. A missing global
// policy is treated as an uninitialized deny-all so `check` still answers
// safely; a missing per-sandbox policy is an error (the sandbox should exist).
func loadEffectivePolicy(sandboxName string) (policy.Policy, error) {
	path, err := policyPathFor(sandboxName)
	if err != nil {
		return policy.Policy{}, err
	}
	if !fileExists(path) {
		if sandboxName != "" {
			return policy.Policy{}, fmt.Errorf("no policy for sandbox %q at %s", sandboxName, path)
		}
		// Uninitialized global policy: fail closed to deny-all.
		return policy.NewPolicy(nil, policy.Deny)
	}
	return policy.LoadFile(path)
}

// newPolicyLsCmd lists the active rules in a policy.
func newPolicyLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls [SANDBOX]",
		Short: "List active rules",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pol, err := loadEffectivePolicy(firstArg(args))
			if err != nil {
				return err
			}
			rules := pol.Rules()
			if len(rules) == 0 {
				return outf(cmd.OutOrStdout(), "no rules (default: %s)\n", pol.Default())
			}
			if err := outf(cmd.OutOrStdout(), "default: %s\n", pol.Default()); err != nil {
				return err
			}
			for _, r := range rules {
				if err := outf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", r.ID, r.Decision, r.Resource, r.Source); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

// newPolicyInspectCmd shows full detail for a rule by ID or resource.
func newPolicyInspectCmd() *cobra.Command {
	var sandboxName string
	cmd := &cobra.Command{
		Use:   "inspect <rule-id-or-resource>",
		Short: "Show rule detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pol, err := loadEffectivePolicy(sandboxName)
			if err != nil {
				return err
			}
			for _, r := range pol.Rules() {
				if r.ID == args[0] || r.Resource == args[0] {
					return outf(cmd.OutOrStdout(),
						"id: %s\ndecision: %s\nresource: %s\ncreated_at: %s\nsource: %s\n",
						r.ID, r.Decision, r.Resource, r.CreatedAt, r.Source)
				}
			}
			return fmt.Errorf("no rule matching %q", args[0])
		},
	}
	cmd.Flags().StringVar(&sandboxName, "sandbox", "", "inspect a sandbox policy (default: global)")
	return cmd
}

// newPolicyResetCmd removes all custom (local) rules from a policy, keeping the
// default decision (spec §6.17).
func newPolicyResetCmd() *cobra.Command {
	var sandboxName string
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Remove all custom rules",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := policyPathFor(sandboxName)
			if err != nil {
				return err
			}
			doc, err := readPolicyDoc(path)
			if err != nil {
				return err
			}
			kept := doc.Rules[:0]
			for _, r := range doc.Rules {
				if r.Source != string(policy.SourceLocal) {
					kept = append(kept, r)
				}
			}
			removed := len(doc.Rules) - len(kept)
			doc.Rules = kept
			if err := writePolicyDoc(path, doc); err != nil {
				return err
			}
			return outf(cmd.OutOrStdout(), "removed %d custom rule(s)\n", removed)
		},
	}
	cmd.Flags().StringVar(&sandboxName, "sandbox", "", "reset a sandbox policy (default: global)")
	return cmd
}

// newPolicyRmCmd removes rules by id or resource match (spec §6.17).
func newPolicyRmCmd() *cobra.Command {
	var (
		sandboxName string
		byID        string
		byResource  string
	)
	cmd := &cobra.Command{
		Use:   "rm network",
		Short: "Remove rules by id or resource",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && args[0] != "network" {
				return fmt.Errorf("unsupported rule type %q (only \"network\" is supported)", args[0])
			}
			if byID == "" && byResource == "" {
				return fmt.Errorf("specify --id or --resource")
			}
			path, err := policyPathFor(sandboxName)
			if err != nil {
				return err
			}
			doc, err := readPolicyDoc(path)
			if err != nil {
				return err
			}
			kept := doc.Rules[:0]
			for _, r := range doc.Rules {
				if (byID != "" && r.ID == byID) || (byResource != "" && r.Resource == byResource) {
					continue
				}
				kept = append(kept, r)
			}
			removed := len(doc.Rules) - len(kept)
			doc.Rules = kept
			if err := writePolicyDoc(path, doc); err != nil {
				return err
			}
			return outf(cmd.OutOrStdout(), "removed %d rule(s)\n", removed)
		},
	}
	cmd.Flags().StringVar(&sandboxName, "sandbox", "", "scope to a sandbox (default: global)")
	cmd.Flags().StringVar(&byID, "id", "", "remove the rule with this id")
	cmd.Flags().StringVar(&byResource, "resource", "", "remove rules with this resource")
	return cmd
}

// firstArg returns args[0] or "" when args is empty.
func firstArg(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return ""
}
