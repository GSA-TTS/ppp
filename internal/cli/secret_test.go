package cli

import (
	"strings"
	"testing"
)

func TestSecretSetFromEnvAndPrecedence(t *testing.T) {
	testEnv(t)
	d, h := newHarness()

	// Global key.
	t.Setenv("PPP_TEST_SECRET", "FAKE-GLOBAL-VALUE")
	if _, err := run(t, d, "", "secret", "set", "anthropic", "--from-env", "PPP_TEST_SECRET"); err != nil {
		t.Fatalf("secret set global: %v", err)
	}
	// Per-sandbox key (takes precedence in the resolver).
	t.Setenv("PPP_TEST_SECRET_SB", "FAKE-SANDBOX-VALUE")
	if _, err := run(t, d, "", "secret", "set", "anthropic", "--sandbox", "ppp-red-bird", "--from-env", "PPP_TEST_SECRET_SB"); err != nil {
		t.Fatalf("secret set sandbox: %v", err)
	}

	if _, ok := h.store.data["ppp.anthropic"]; !ok {
		t.Error("expected global key ppp.anthropic to be stored")
	}
	if _, ok := h.store.data["ppp.ppp-red-bird.anthropic"]; !ok {
		t.Error("expected per-sandbox key ppp.ppp-red-bird.anthropic to be stored")
	}
}

func TestSecretLsNeverPrintsValues(t *testing.T) {
	testEnv(t)
	d, h := newHarness()
	h.store.data["ppp.anthropic"] = "FAKE-SECRET-VALUE-SHOULD-NOT-APPEAR"
	h.store.data["ppp.ppp-red-bird.usai"] = "FAKE-SANDBOX-VALUE-SHOULD-NOT-APPEAR"

	out, err := run(t, d, "", "secret", "ls")
	if err != nil {
		t.Fatalf("secret ls: %v", err)
	}
	if !strings.Contains(out, "ppp.anthropic") || !strings.Contains(out, "ppp.ppp-red-bird.usai") {
		t.Errorf("expected key names listed, got %q", out)
	}
	if strings.Contains(out, "FAKE-SECRET-VALUE-SHOULD-NOT-APPEAR") ||
		strings.Contains(out, "FAKE-SANDBOX-VALUE-SHOULD-NOT-APPEAR") {
		t.Errorf("secret ls leaked a value: %q", out)
	}
}

func TestSecretLsScopedToSandbox(t *testing.T) {
	testEnv(t)
	d, h := newHarness()
	h.store.data["ppp.anthropic"] = "x"
	h.store.data["ppp.ppp-red-bird.usai"] = "y"

	out, err := run(t, d, "", "secret", "ls", "ppp-red-bird")
	if err != nil {
		t.Fatalf("secret ls scoped: %v", err)
	}
	if !strings.Contains(out, "ppp.ppp-red-bird.usai") {
		t.Errorf("expected per-sandbox key listed, got %q", out)
	}
	if strings.Contains(out, "ppp.anthropic\t") {
		t.Errorf("expected global key excluded from sandbox scope, got %q", out)
	}
}

func TestSecretLsEmpty(t *testing.T) {
	testEnv(t)
	d, _ := newHarness()
	out, err := run(t, d, "", "secret", "ls")
	if err != nil {
		t.Fatalf("secret ls empty: %v", err)
	}
	if !strings.Contains(out, "no secrets stored") {
		t.Errorf("expected friendly empty message, got %q", out)
	}
}

func TestSecretRm(t *testing.T) {
	testEnv(t)
	d, h := newHarness()
	h.store.data["ppp.anthropic"] = "x"
	if _, err := run(t, d, "", "secret", "rm", "anthropic"); err != nil {
		t.Fatalf("secret rm: %v", err)
	}
	if _, ok := h.store.data["ppp.anthropic"]; ok {
		t.Error("expected ppp.anthropic to be deleted")
	}
}

func TestSecretSetFromStdin(t *testing.T) {
	testEnv(t)
	d, h := newHarness()
	if _, err := run(t, d, "FAKE-STDIN-VALUE\n", "secret", "set", "openai"); err != nil {
		t.Fatalf("secret set stdin: %v", err)
	}
	if got := h.store.data["ppp.openai"]; got != "FAKE-STDIN-VALUE" {
		t.Errorf("expected stdin value stored, got %q", got)
	}
}

func TestSecretImportDryRun(t *testing.T) {
	testEnv(t)
	d, h := newHarness()
	t.Setenv("ANTHROPIC_API_KEY", "FAKE-IMPORT-VALUE")
	out, err := run(t, d, "", "secret", "import", "--dry-run")
	if err != nil {
		t.Fatalf("secret import --dry-run: %v", err)
	}
	if !strings.Contains(out, "would import anthropic") {
		t.Errorf("expected dry-run to name anthropic, got %q", out)
	}
	if len(h.store.data) != 0 {
		t.Errorf("dry-run must not store anything, got %v keys", len(h.store.data))
	}
}
