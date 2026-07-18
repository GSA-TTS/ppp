package agent_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/GSA-TTS/ppp/internal/agent"
)

func TestLookupOpencode(t *testing.T) {
	a, err := agent.Lookup("opencode")
	if err != nil {
		t.Fatalf("Lookup(opencode): %v", err)
	}
	if a.Name != "opencode" {
		t.Errorf("Name = %q", a.Name)
	}
	if a.DefaultImage == "" {
		t.Error("DefaultImage is empty")
	}
	if a.Env["OPENCODE_SANDBOX"] != "1" {
		t.Errorf("expected OPENCODE_SANDBOX=1, got %v", a.Env)
	}
}

func TestLookupUnknown(t *testing.T) {
	_, err := agent.Lookup("claude")
	if !errors.Is(err, agent.ErrUnknownAgent) {
		t.Errorf("expected ErrUnknownAgent, got %v", err)
	}
}

func TestHeadlessArgsAppendsPassthroughVerbatim(t *testing.T) {
	a, _ := agent.Lookup("opencode")
	got := a.HeadlessArgs("fix the bug", []string{"--model", "anthropic/claude"})
	want := []string{"opencode", "run", "fix the bug", "--model", "anthropic/claude"}
	if !slices.Equal(got, want) {
		t.Errorf("HeadlessArgs = %v, want %v", got, want)
	}
}

func TestInteractiveArgs(t *testing.T) {
	a, _ := agent.Lookup("opencode")
	got := a.InteractiveArgs(nil)
	if !slices.Equal(got, []string{"opencode"}) {
		t.Errorf("InteractiveArgs = %v", got)
	}
}

func TestNames(t *testing.T) {
	if got := agent.Names(); !slices.Equal(got, []string{"opencode"}) {
		t.Errorf("Names = %v, want [opencode]", got)
	}
}

func TestLookupImageOverrideEnv(t *testing.T) {
	t.Setenv("PPP_OPENCODE_IMAGE", "ghcr.io/example/custom-opencode:test")
	a, err := agent.Lookup("opencode")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if a.DefaultImage != "ghcr.io/example/custom-opencode:test" {
		t.Errorf("image override not applied: %q", a.DefaultImage)
	}
}

func TestImageEnvVarName(t *testing.T) {
	// exercised indirectly, but assert the mapping for opencode via override.
	t.Setenv("PPP_OPENCODE_IMAGE", "x/y:z")
	a, _ := agent.Lookup("opencode")
	if a.DefaultImage != "x/y:z" {
		t.Errorf("expected PPP_OPENCODE_IMAGE to map, got %q", a.DefaultImage)
	}
}
