package secret

import (
	"errors"
	"testing"
)

// resolverWith builds a Resolver over a fake store seeded with the given
// key/value pairs. Values are obvious fakes; no real secret ever appears here.
func resolverWith(pairs map[string]string) (*Resolver, *fakeStore) {
	fs := newFakeStore()
	for k, v := range pairs {
		fs.set(k, v)
	}
	return NewResolver(fs), fs
}

func TestResolve_PerSandboxShadowsGlobal(t *testing.T) {
	r, _ := resolverWith(map[string]string{
		"ppp.anthropic":              "FAKE-GLOBAL-KEY",
		"ppp.ppp-red-bird.anthropic": "FAKE-SANDBOX-KEY",
	})

	inj, ok, err := r.Resolve("anthropic", "ppp-red-bird")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected a resolved injection, got none")
	}
	if inj.Value != "FAKE-SANDBOX-KEY" {
		t.Errorf("sandbox-scoped key must win: got value %q", inj.Value)
	}
}

func TestResolve_FallsBackToGlobal(t *testing.T) {
	r, _ := resolverWith(map[string]string{
		"ppp.anthropic": "FAKE-GLOBAL-KEY",
	})

	inj, ok, err := r.Resolve("anthropic", "ppp-red-bird")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected fallback to global key, got none")
	}
	if inj.Value != "FAKE-GLOBAL-KEY" {
		t.Errorf("expected global key, got %q", inj.Value)
	}
}

func TestResolve_EmptySandboxUsesGlobalOnly(t *testing.T) {
	r, _ := resolverWith(map[string]string{
		"ppp.openai": "FAKE-OPENAI-KEY",
	})

	inj, ok, err := r.Resolve("openai", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok || inj.Value != "Bearer FAKE-OPENAI-KEY" {
		t.Errorf("expected global openai bearer, got ok=%v value=%q", ok, inj.Value)
	}
}

func TestResolve_NotFound(t *testing.T) {
	r, _ := resolverWith(map[string]string{})

	_, ok, err := r.Resolve("anthropic", "ppp-red-bird")
	if err != nil {
		t.Fatalf("not-found must not be an error, got: %v", err)
	}
	if ok {
		t.Error("expected ok=false for a missing secret")
	}
}

func TestResolve_HeaderMapping(t *testing.T) {
	cases := []struct {
		service    string
		wantHeader string
		wantValue  string
	}{
		{"anthropic", "x-api-key", "FAKE-KEY-123"},
		{"google", "x-goog-api-key", "FAKE-KEY-123"},
		{"gemini", "x-goog-api-key", "FAKE-KEY-123"},
		{"openai", "Authorization", "Bearer FAKE-KEY-123"},
		{"github", "Authorization", "Bearer FAKE-KEY-123"},
		{"usai", "Authorization", "Bearer FAKE-KEY-123"},
		{"totally-unknown-service", "Authorization", "Bearer FAKE-KEY-123"},
	}
	for _, tc := range cases {
		t.Run(tc.service, func(t *testing.T) {
			r, _ := resolverWith(map[string]string{
				"ppp." + tc.service: "FAKE-KEY-123",
			})
			inj, ok, err := r.Resolve(tc.service, "")
			if err != nil || !ok {
				t.Fatalf("resolve failed: ok=%v err=%v", ok, err)
			}
			if inj.Header != tc.wantHeader {
				t.Errorf("header: got %q want %q", inj.Header, tc.wantHeader)
			}
			if inj.Value != tc.wantValue {
				t.Errorf("value: got %q want %q", inj.Value, tc.wantValue)
			}
		})
	}
}

func TestResolve_ServiceMatchingIsCaseInsensitive(t *testing.T) {
	r, _ := resolverWith(map[string]string{
		"ppp.anthropic": "FAKE-KEY-123",
	})
	inj, ok, err := r.Resolve("Anthropic", "")
	if err != nil || !ok {
		t.Fatalf("resolve failed: ok=%v err=%v", ok, err)
	}
	if inj.Header != "x-api-key" {
		t.Errorf("case-insensitive provider lookup: got header %q", inj.Header)
	}
}

func TestResolve_EmptyServiceIsError(t *testing.T) {
	r, _ := resolverWith(map[string]string{})
	if _, _, err := r.Resolve("", "sb"); err == nil {
		t.Error("expected an error for an empty service")
	}
}

func TestResolve_PropagatesLocked(t *testing.T) {
	r, fs := resolverWith(map[string]string{"ppp.anthropic": "FAKE-KEY-123"})
	fs.locked = true

	_, _, err := r.Resolve("anthropic", "")
	if !errors.Is(err, ErrLocked) {
		t.Errorf("expected ErrLocked to propagate, got %v", err)
	}
}
