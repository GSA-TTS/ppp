package policy

import "testing"

func mustRule(t *testing.T, id string, d Decision, resource string) Rule {
	t.Helper()
	r, err := NewRule(id, d, resource, "2026-01-01T00:00:00Z", SourceLocal)
	if err != nil {
		t.Fatalf("NewRule(%q, %q) unexpected error: %v", id, resource, err)
	}
	return r
}

func TestRuleMatches(t *testing.T) {
	tests := []struct {
		name     string
		resource string
		target   Target
		want     bool
	}{
		{name: "block-all matches any host", resource: "**", target: Target{Host: "anything.example.com"}, want: true},
		{name: "block-all matches ip", resource: "**", target: Target{Host: "10.0.0.1"}, want: true},

		{name: "exact host match", resource: "api.anthropic.com", target: Target{Host: "api.anthropic.com"}, want: true},
		{name: "exact host mismatch", resource: "api.anthropic.com", target: Target{Host: "api.openai.com"}, want: false},
		{name: "exact host case-insensitive", resource: "api.anthropic.com", target: Target{Host: "API.Anthropic.COM"}, want: true},

		{name: "wildcard subdomain match", resource: "*.example.com", target: Target{Host: "api.example.com"}, want: true},
		{name: "wildcard deep subdomain match", resource: "*.example.com", target: Target{Host: "a.b.example.com"}, want: true},
		{name: "wildcard does not match bare parent", resource: "*.example.com", target: Target{Host: "example.com"}, want: false},
		{name: "wildcard mismatch other domain", resource: "*.example.com", target: Target{Host: "example.org"}, want: false},
		{name: "single-char glob match", resource: "host?.example.com", target: Target{Host: "host1.example.com"}, want: true},
		{name: "single-char glob mismatch", resource: "host?.example.com", target: Target{Host: "host12.example.com"}, want: false},

		{name: "regex anchored match", resource: `^api\.(anthropic|openai)\.com$`, target: Target{Host: "api.openai.com"}, want: true},
		{name: "regex anchored mismatch", resource: `^api\.(anthropic|openai)\.com$`, target: Target{Host: "api.google.com"}, want: false},
		{name: "regex no substring match", resource: `example\.com`, target: Target{Host: "notexample.com"}, want: false},
		{name: "regex char class", resource: `host[0-9]\.example\.com`, target: Target{Host: "host7.example.com"}, want: true},

		{name: "cidr v4 in range", resource: "10.0.0.0/8", target: Target{Host: "10.1.2.3"}, want: true},
		{name: "cidr v4 out of range", resource: "10.0.0.0/8", target: Target{Host: "11.0.0.1"}, want: false},
		{name: "single ipv4 match", resource: "192.168.1.1", target: Target{Host: "192.168.1.1"}, want: true},
		{name: "single ipv4 mismatch", resource: "192.168.1.1", target: Target{Host: "192.168.1.2"}, want: false},
		{name: "cidr v6 in range", resource: "2001:db8::/32", target: Target{Host: "2001:db8::1"}, want: true},
		{name: "cidr v6 out of range", resource: "2001:db8::/32", target: Target{Host: "2001:dead::1"}, want: false},
		{name: "single ipv6 match", resource: "::1", target: Target{Host: "::1"}, want: true},
		{name: "cidr does not match non-ip host", resource: "10.0.0.0/8", target: Target{Host: "example.com"}, want: false},

		{name: "port suffix match", resource: "api.example.com:443", target: Target{Host: "api.example.com", Port: 443}, want: true},
		{name: "port suffix mismatch", resource: "api.example.com:443", target: Target{Host: "api.example.com", Port: 80}, want: false},
		{name: "port suffix requires target port", resource: "api.example.com:443", target: Target{Host: "api.example.com"}, want: false},
		{name: "no port suffix ignores target port", resource: "api.example.com", target: Target{Host: "api.example.com", Port: 8080}, want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := mustRule(t, "r1", Allow, tc.resource)
			if got := r.Matches(tc.target); got != tc.want {
				t.Fatalf("Rule(%q).Matches(%+v) = %v, want %v", tc.resource, tc.target, got, tc.want)
			}
		})
	}
}

func TestNewRuleErrors(t *testing.T) {
	tests := []struct {
		name     string
		decision Decision
		resource string
	}{
		{name: "empty resource", decision: Allow, resource: ""},
		{name: "whitespace resource", decision: Allow, resource: "   "},
		{name: "invalid decision", decision: Decision("maybe"), resource: "example.com"},
		{name: "bad port", decision: Allow, resource: "example.com:70000"},
		{name: "malformed cidr", decision: Deny, resource: "10.0.0.0/99"},
		{name: "invalid regex", decision: Deny, resource: "api.[example.com"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewRule("bad", tc.decision, tc.resource, "", SourceLocal); err == nil {
				t.Fatalf("NewRule(%q, %q) = nil error, want error", tc.decision, tc.resource)
			}
		})
	}
}
