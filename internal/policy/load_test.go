package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) error {
	t.Helper()
	return os.WriteFile(path, []byte(content), 0o600)
}

func TestLoadValid(t *testing.T) {
	data := []byte(`
default: block
rules:
  - id: 11111111-1111-1111-1111-111111111111
    decision: allow
    type: network
    resource: "api.anthropic.com"
    created_at: "2026-01-01T00:00:00Z"
    source: local
  - id: 22222222-2222-2222-2222-222222222222
    decision: deny
    type: network
    resource: "**"
    created_at: "2026-01-01T00:00:00Z"
    source: kit
`)
	p, err := Load(data)
	if err != nil {
		t.Fatalf("Load valid policy: unexpected error: %v", err)
	}
	if p.Default() != Deny {
		t.Fatalf("Default() = %q, want %q", p.Default(), Deny)
	}
	if got := len(p.Rules()); got != 2 {
		t.Fatalf("len(Rules()) = %d, want 2", got)
	}
	// deny ** wins over the allow rule.
	res := p.Evaluate(Target{Host: "api.anthropic.com"})
	if res.Decision != Deny {
		t.Fatalf("Evaluate decision = %q, want %q", res.Decision, Deny)
	}
}

func TestLoadDefaults(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want Decision
	}{
		{name: "block default", yaml: "default: block\n", want: Deny},
		{name: "allow default", yaml: "default: allow\n", want: Allow},
		{name: "allow-all preset", yaml: "default: allow-all\n", want: Allow},
		{name: "omitted default is deny", yaml: "rules: []\n", want: Deny},
		{name: "empty document is deny", yaml: "", want: Deny},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := Load([]byte(tc.yaml))
			if err != nil {
				t.Fatalf("Load(%q): unexpected error: %v", tc.yaml, err)
			}
			if p.Default() != tc.want {
				t.Fatalf("Default() = %q, want %q", p.Default(), tc.want)
			}
		})
	}
}

func TestLoadFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{name: "malformed yaml", yaml: "default: block\nrules: [ this is : broken"},
		{name: "not a mapping", yaml: "- just\n- a\n- list\n"},
		{name: "invalid default", yaml: "default: sometimes\n"},
		{name: "unknown field", yaml: "defaultt: block\n"},
		{name: "invalid decision", yaml: "rules:\n  - id: x\n    decision: maybe\n    resource: example.com\n"},
		{name: "empty resource", yaml: "rules:\n  - id: x\n    decision: allow\n    resource: \"\"\n"},
		{name: "bad cidr", yaml: "rules:\n  - id: x\n    decision: deny\n    resource: 10.0.0.0/99\n"},
		{name: "bad port", yaml: "rules:\n  - id: x\n    decision: allow\n    resource: host:99999\n"},
		{name: "invalid regex", yaml: "rules:\n  - id: x\n    decision: deny\n    resource: \"api.[bad\"\n"},
		{name: "unsupported type", yaml: "rules:\n  - id: x\n    decision: allow\n    type: filesystem\n    resource: example.com\n"},
		{name: "invalid source", yaml: "rules:\n  - id: x\n    decision: allow\n    resource: example.com\n    source: internet\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := Load([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("Load(%q) = nil error, want error (fail closed)", tc.name)
			}
			// Fail-closed guarantee: the returned policy must deny everything.
			if res := p.Evaluate(Target{Host: "anything.example.com"}); res.Decision != Deny {
				t.Fatalf("on error, Evaluate decision = %q, want %q (fail closed)", res.Decision, Deny)
			}
		})
	}
}

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yaml")
	if err := writeFile(t, path, "default: allow\n"); err != nil {
		t.Fatalf("write temp policy: %v", err)
	}
	p, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: unexpected error: %v", err)
	}
	if p.Default() != Allow {
		t.Fatalf("Default() = %q, want %q", p.Default(), Allow)
	}
}

func TestLoadFileMissingFailsClosed(t *testing.T) {
	p, err := LoadFile(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err == nil {
		t.Fatal("LoadFile(missing) = nil error, want error")
	}
	if res := p.Evaluate(Target{Host: "example.com"}); res.Decision != Deny {
		t.Fatalf("on read error, Evaluate decision = %q, want %q (fail closed)", res.Decision, Deny)
	}
}
