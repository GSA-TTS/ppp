package policy

import "fmt"

func invalidDefaultError(d Decision) error {
	return fmt.Errorf("policy: invalid default decision %q (want %q or %q)", d, Allow, Deny)
}

// Policy is a compiled, evaluatable network policy: an ordered set of rules and
// a default decision applied when no rule matches. Construct it with NewPolicy
// so the default is validated. The zero Policy denies everything, so an
// uninitialized Policy fails closed.
type Policy struct {
	rules       []Rule
	defaultDecn Decision
}

// Result is the outcome of evaluating a target against a policy: the decision
// and the rule that produced it. Matched is nil when the default action was
// applied (no rule matched).
type Result struct {
	Decision Decision
	Matched  *Rule
}

// NewPolicy builds a Policy from compiled rules and a default decision. The
// default must be Allow or Deny; otherwise an error is returned so callers can
// fail closed rather than construct a policy with an undefined default.
func NewPolicy(rules []Rule, defaultDecn Decision) (Policy, error) {
	if defaultDecn != Allow && defaultDecn != Deny {
		return Policy{}, invalidDefaultError(defaultDecn)
	}
	return Policy{rules: rules, defaultDecn: defaultDecn}, nil
}

// Default returns the policy's default decision.
func (p Policy) Default() Decision { return p.defaultDecn }

// Rules returns a copy of the policy's rules in evaluation order.
func (p Policy) Rules() []Rule {
	out := make([]Rule, len(p.rules))
	copy(out, p.rules)
	return out
}

// Evaluate decides whether the target is allowed or denied under this policy,
// applying deny-wins precedence: deny > allow > default. Deny rules are checked
// first and the first matching deny wins; otherwise the first matching allow
// wins; otherwise the policy default applies. A zero-value Policy (no rules,
// empty default) denies, so evaluation always fails closed.
func (p Policy) Evaluate(t Target) Result {
	if r := firstMatch(p.rules, t, Deny); r != nil {
		return Result{Decision: Deny, Matched: r}
	}
	if r := firstMatch(p.rules, t, Allow); r != nil {
		return Result{Decision: Allow, Matched: r}
	}
	decn := p.defaultDecn
	// A zero-value Policy has an empty defaultDecn; normalize any non-Allow
	// default (including "") to Deny so evaluation always fails closed.
	if decn != Allow {
		decn = Deny
	}
	return Result{Decision: decn}
}

// firstMatch returns a pointer to the first rule with the given decision that
// matches the target, or nil if none match.
func firstMatch(rules []Rule, t Target, want Decision) *Rule {
	for i := range rules {
		if rules[i].Decision == want && rules[i].Matches(t) {
			return &rules[i]
		}
	}
	return nil
}
