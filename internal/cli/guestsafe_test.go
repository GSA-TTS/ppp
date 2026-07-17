package cli

import "testing"

func TestValidateWorkspacePath(t *testing.T) {
	ok := []string{
		"/tmp/ws",
		"/Users/dev/my-project",
		"/a/b_c/d.e",
		"/workspaces/ppp",
	}
	for _, p := range ok {
		if err := validateWorkspacePath(p); err != nil {
			t.Errorf("validateWorkspacePath(%q) unexpected error: %v", p, err)
		}
	}

	bad := []string{
		"",                         // empty
		"relative/path",            // not absolute
		"/tmp/x:/ws; touch /pwned", // command injection
		"/tmp/$(whoami)",           // command substitution
		"/tmp/`id`",                // backtick substitution
		"/tmp/a b",                 // whitespace (would word-split)
		"/tmp/a|b",                 // pipe
		"/tmp/a&b",                 // background
		"/tmp/a>b",                 // redirect
		"/tmp/a\nb",                // newline
		"/tmp/*",                   // glob
		"/tmp/a#b",                 // comment
	}
	for _, p := range bad {
		if err := validateWorkspacePath(p); err == nil {
			t.Errorf("validateWorkspacePath(%q) expected error, got nil", p)
		}
	}
}

func TestGuestArg(t *testing.T) {
	if err := guestArg("agent image", "ghcr.io/gsa-tts/ppp-opencode:latest"); err != nil {
		t.Errorf("valid image ref rejected: %v", err)
	}
	if err := guestArg("agent image", "img; rm -rf /"); err == nil {
		t.Error("injection in image ref not rejected")
	}
	if err := guestArg("agent arg", ""); err == nil {
		t.Error("empty arg not rejected")
	}
}

// TestRunRejectsInjectingWorkspace ensures a metacharacter-laden workspace path
// is rejected at ingress — before any PodmanRunner call — so it can never reach
// the guest shell (code review BLOCKER-1).
func TestRunRejectsInjectingWorkspace(t *testing.T) {
	testEnv(t)
	d, h := newHarness()
	_, err := run(t, d, "", "run", "opencode", "/tmp/x:/ws; touch /pwned", "--name", "ppp-red-bird")
	if err == nil {
		t.Fatal("expected run to reject an injecting workspace path")
	}
	if len(h.runner.Calls) != 0 {
		t.Errorf("no PodmanRunner call should have been made; got %d", len(h.runner.Calls))
	}
}
