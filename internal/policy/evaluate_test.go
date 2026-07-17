package policy

import "testing"

func mustPolicy(t *testing.T, def Decision, rules ...Rule) Policy {
	t.Helper()
	p, err := NewPolicy(rules, def)
	if err != nil {
		t.Fatalf("NewPolicy(default=%q) unexpected error: %v", def, err)
	}
	return p
}

func TestEvaluatePrecedence(t *testing.T) {
	tests := []struct {
		name       string
		def        Decision
		rules      []Rule
		target     Target
		wantDecn   Decision
		wantRuleID string // "" means default (no matched rule)
	}{
		{
			name:     "no rules uses allow default",
			def:      Allow,
			target:   Target{Host: "api.example.com"},
			wantDecn: Allow,
		},
		{
			name:     "no rules uses deny default",
			def:      Deny,
			target:   Target{Host: "api.example.com"},
			wantDecn: Deny,
		},
		{
			name:       "allow rule matches over deny default",
			def:        Deny,
			rules:      []Rule{mustRule(t, "a1", Allow, "api.example.com")},
			target:     Target{Host: "api.example.com"},
			wantDecn:   Allow,
			wantRuleID: "a1",
		},
		{
			name: "deny wins over allow for same target",
			def:  Allow,
			rules: []Rule{
				mustRule(t, "a1", Allow, "api.example.com"),
				mustRule(t, "d1", Deny, "api.example.com"),
			},
			target:     Target{Host: "api.example.com"},
			wantDecn:   Deny,
			wantRuleID: "d1",
		},
		{
			name: "deny wins regardless of ordering (allow listed first)",
			def:  Deny,
			rules: []Rule{
				mustRule(t, "a1", Allow, "*.example.com"),
				mustRule(t, "d1", Deny, "secret.example.com"),
			},
			target:     Target{Host: "secret.example.com"},
			wantDecn:   Deny,
			wantRuleID: "d1",
		},
		{
			name: "block-all deny beats specific allow",
			def:  Allow,
			rules: []Rule{
				mustRule(t, "a1", Allow, "api.example.com"),
				mustRule(t, "d1", Deny, "**"),
			},
			target:     Target{Host: "api.example.com"},
			wantDecn:   Deny,
			wantRuleID: "d1",
		},
		{
			name: "first matching allow wins",
			def:  Deny,
			rules: []Rule{
				mustRule(t, "a1", Allow, "*.example.com"),
				mustRule(t, "a2", Allow, "api.example.com"),
			},
			target:     Target{Host: "api.example.com"},
			wantDecn:   Allow,
			wantRuleID: "a1",
		},
		{
			name: "first matching deny wins",
			def:  Allow,
			rules: []Rule{
				mustRule(t, "d1", Deny, "*.example.com"),
				mustRule(t, "d2", Deny, "api.example.com"),
			},
			target:     Target{Host: "api.example.com"},
			wantDecn:   Deny,
			wantRuleID: "d1",
		},
		{
			name: "no rule matches falls through to default",
			def:  Allow,
			rules: []Rule{
				mustRule(t, "d1", Deny, "blocked.example.com"),
			},
			target:   Target{Host: "other.example.com"},
			wantDecn: Allow,
		},
		{
			name: "port-scoped deny only applies to that port",
			def:  Allow,
			rules: []Rule{
				mustRule(t, "d1", Deny, "api.example.com:22"),
			},
			target:   Target{Host: "api.example.com", Port: 443},
			wantDecn: Allow,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := mustPolicy(t, tc.def, tc.rules...)
			got := p.Evaluate(tc.target)
			if got.Decision != tc.wantDecn {
				t.Fatalf("Evaluate(%+v).Decision = %q, want %q", tc.target, got.Decision, tc.wantDecn)
			}
			assertMatched(t, got, tc.wantRuleID)
		})
	}
}

func assertMatched(t *testing.T, got Result, wantRuleID string) {
	t.Helper()
	if wantRuleID == "" {
		if got.Matched != nil {
			t.Fatalf("Matched = %q, want nil (default)", got.Matched.ID)
		}
		return
	}
	if got.Matched == nil {
		t.Fatalf("Matched = nil, want rule %q", wantRuleID)
	}
	if got.Matched.ID != wantRuleID {
		t.Fatalf("Matched.ID = %q, want %q", got.Matched.ID, wantRuleID)
	}
}

func TestZeroPolicyFailsClosed(t *testing.T) {
	var p Policy
	got := p.Evaluate(Target{Host: "api.example.com"})
	if got.Decision != Deny {
		t.Fatalf("zero Policy.Evaluate().Decision = %q, want %q", got.Decision, Deny)
	}
}

func TestNewPolicyInvalidDefault(t *testing.T) {
	if _, err := NewPolicy(nil, Decision("maybe")); err == nil {
		t.Fatal("NewPolicy with invalid default = nil error, want error")
	}
}
