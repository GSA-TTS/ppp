package cli

import (
	"strings"
	"testing"
)

func TestPolicyCheck_VerticalThread(t *testing.T) {
	testEnv(t)
	d, _ := newHarness()

	if out, err := run(t, d, "", "policy", "init", "deny-all"); err != nil {
		t.Fatalf("policy init: %v (out=%q)", err, out)
	}
	if out, err := run(t, d, "", "policy", "allow", "network", "api.anthropic.com"); err != nil {
		t.Fatalf("policy allow: %v (out=%q)", err, out)
	}

	// Allowed by the explicit rule.
	out, err := run(t, d, "", "policy", "check", "network", "api.anthropic.com")
	if err != nil {
		t.Fatalf("policy check (allow): %v", err)
	}
	if !strings.Contains(out, "ALLOWED") || !strings.Contains(out, "matched rule") {
		t.Errorf("expected ALLOWED with matched rule, got %q", out)
	}

	// Denied by the deny-all default.
	out, err = run(t, d, "", "policy", "check", "network", "evil.com")
	if err != nil {
		t.Fatalf("policy check (deny): %v", err)
	}
	if !strings.Contains(out, "DENIED") || !strings.Contains(out, "default") {
		t.Errorf("expected DENIED by default, got %q", out)
	}
}

func TestPolicyCheck_GlobAndBlockAll(t *testing.T) {
	testEnv(t)
	d, _ := newHarness()

	if _, err := run(t, d, "", "policy", "init", "allow-all"); err != nil {
		t.Fatalf("policy init: %v", err)
	}
	// A glob deny (subdomain) should win over the allow-all default.
	if _, err := run(t, d, "", "policy", "deny", "network", "*.evil.com"); err != nil {
		t.Fatalf("policy deny glob: %v", err)
	}
	out, err := run(t, d, "", "policy", "check", "network", "api.evil.com")
	if err != nil {
		t.Fatalf("policy check glob: %v", err)
	}
	if !strings.Contains(out, "DENIED") {
		t.Errorf("expected glob deny to match subdomain, got %q", out)
	}
	// The apex is not matched by *.evil.com, so allow-all default applies.
	out, err = run(t, d, "", "policy", "check", "network", "evil.com")
	if err != nil {
		t.Fatalf("policy check apex: %v", err)
	}
	if !strings.Contains(out, "ALLOWED") {
		t.Errorf("expected apex to fall through to allow-all default, got %q", out)
	}

	// A block-all deny rule denies everything.
	if _, err := run(t, d, "", "policy", "deny", "network", "**"); err != nil {
		t.Fatalf("policy deny **: %v", err)
	}
	out, err = run(t, d, "", "policy", "check", "network", "anything.example.org")
	if err != nil {
		t.Fatalf("policy check block-all: %v", err)
	}
	if !strings.Contains(out, "DENIED") {
		t.Errorf("expected ** to deny all hosts, got %q", out)
	}
}

func TestPolicyCheck_UninitializedFailsClosed(t *testing.T) {
	testEnv(t)
	d, _ := newHarness()
	// No policy init: global policy is absent, so check must fail closed.
	out, err := run(t, d, "", "policy", "check", "network", "api.anthropic.com")
	if err != nil {
		t.Fatalf("policy check on empty policy: %v", err)
	}
	if !strings.Contains(out, "DENIED") {
		t.Errorf("expected uninitialized policy to deny (fail closed), got %q", out)
	}
}

func TestPolicyLsAndRmAndReset(t *testing.T) {
	testEnv(t)
	d, _ := newHarness()

	if _, err := run(t, d, "", "policy", "init", "deny-all"); err != nil {
		t.Fatalf("policy init: %v", err)
	}
	if _, err := run(t, d, "", "policy", "allow", "network", "a.example.com,b.example.com"); err != nil {
		t.Fatalf("policy allow multi: %v", err)
	}
	out, err := run(t, d, "", "policy", "ls")
	if err != nil {
		t.Fatalf("policy ls: %v", err)
	}
	if !strings.Contains(out, "a.example.com") || !strings.Contains(out, "b.example.com") {
		t.Errorf("expected both rules listed, got %q", out)
	}

	// rm by resource removes only the matching rule.
	if _, err := run(t, d, "", "policy", "rm", "network", "--resource", "a.example.com"); err != nil {
		t.Fatalf("policy rm: %v", err)
	}
	out, _ = run(t, d, "", "policy", "ls")
	if strings.Contains(out, "a.example.com") {
		t.Errorf("expected a.example.com removed, got %q", out)
	}
	if !strings.Contains(out, "b.example.com") {
		t.Errorf("expected b.example.com to remain, got %q", out)
	}

	// reset removes all remaining local rules.
	if _, err := run(t, d, "", "policy", "reset"); err != nil {
		t.Fatalf("policy reset: %v", err)
	}
	out, _ = run(t, d, "", "policy", "ls")
	if strings.Contains(out, "b.example.com") {
		t.Errorf("expected reset to clear custom rules, got %q", out)
	}
}

func TestPolicyAllowRejectsMalformedResource(t *testing.T) {
	testEnv(t)
	d, _ := newHarness()
	if _, err := run(t, d, "", "policy", "init", "deny-all"); err != nil {
		t.Fatalf("policy init: %v", err)
	}
	// An out-of-range port is malformed; the rule must be rejected (fail closed).
	if _, err := run(t, d, "", "policy", "allow", "network", "host:99999"); err == nil {
		t.Error("expected malformed resource to error")
	}
}
