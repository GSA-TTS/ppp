package cli

import (
	"strings"
	"testing"

	"github.com/GSA-TTS/ppp/internal/sandbox"
)

func TestLsEmpty(t *testing.T) {
	testEnv(t)
	d, _ := newHarness()
	out, err := run(t, d, "", "ls")
	if err != nil {
		t.Fatalf("ls empty: %v", err)
	}
	if !strings.Contains(out, "no sandboxes") {
		t.Errorf("expected friendly empty message, got %q", out)
	}
}

func TestLsWithSandbox(t *testing.T) {
	testEnv(t)
	d, _ := newHarness()

	box := sandbox.Sandbox{
		Name:      "ppp-red-bird",
		Agent:     "opencode",
		Workspace: "/tmp/ws",
		Status:    sandbox.StatusRunning,
		Port:      51820,
		InnerIP:   "10.0.0.1",
	}
	if err := box.Save(); err != nil {
		t.Fatalf("saving sandbox: %v", err)
	}

	out, err := run(t, d, "", "ls")
	if err != nil {
		t.Fatalf("ls: %v", err)
	}
	for _, want := range []string{"NAME", "ppp-red-bird", "opencode", "running", "51820", "/tmp/ws"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected ls table to contain %q, got %q", want, out)
		}
	}
}
