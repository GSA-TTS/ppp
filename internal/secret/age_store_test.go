package secret

import (
	"errors"
	"path/filepath"
	"testing"
)

// TestAgeStore_LockedUntilUnlock proves the load-bearing locked-state model:
// before Unlock, Get returns ErrLocked; after Unlock with the right passphrase,
// it resolves. It writes an age file to a temp dir with obvious fake values —
// never a real key, never the real $PPP_DATA.
func TestAgeStore_LockedUntilUnlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.age")
	const pass = "correct horse battery staple"

	if err := WriteAgeStore(path, pass, map[string]string{
		"ppp.anthropic": "FAKE-KEY-123",
	}); err != nil {
		t.Fatalf("write age store: %v", err)
	}

	s := NewAgeStore(path)

	if _, err := s.Get("ppp.anthropic"); !errors.Is(err, ErrLocked) {
		t.Fatalf("expected ErrLocked before unlock, got %v", err)
	}

	if err := s.Unlock(pass); err != nil {
		t.Fatalf("unlock: %v", err)
	}

	got, err := s.Get("ppp.anthropic")
	if err != nil {
		t.Fatalf("get after unlock: %v", err)
	}
	if got != "FAKE-KEY-123" {
		t.Errorf("got %q want FAKE-KEY-123", got)
	}

	if _, err := s.Get("ppp.missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for a missing key, got %v", err)
	}
}

func TestAgeStore_WrongPassphraseFailsAndStaysLocked(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.age")

	if err := WriteAgeStore(path, "right-pass", map[string]string{
		"ppp.openai": "FAKE-KEY-123",
	}); err != nil {
		t.Fatalf("write age store: %v", err)
	}

	s := NewAgeStore(path)
	if err := s.Unlock("wrong-pass"); err == nil {
		t.Fatal("expected unlock with wrong passphrase to fail")
	}
	if _, err := s.Get("ppp.openai"); !errors.Is(err, ErrLocked) {
		t.Errorf("store must remain locked after a failed unlock, got %v", err)
	}
}

// TestAgeStore_ResolverReportsLocked ties the store to the resolver: a Resolver
// backed by a locked age store surfaces ErrLocked so T8 can map it to
// {ok:false, reason:"locked"}.
func TestAgeStore_ResolverReportsLocked(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.age")
	if err := WriteAgeStore(path, "p", map[string]string{"ppp.usai": "FAKE-KEY-123"}); err != nil {
		t.Fatalf("write age store: %v", err)
	}

	r := NewResolver(NewAgeStore(path))
	if _, _, err := r.Resolve("usai", ""); !errors.Is(err, ErrLocked) {
		t.Errorf("expected resolver to surface ErrLocked, got %v", err)
	}
}
