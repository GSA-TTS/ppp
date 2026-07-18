package secret

import (
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestKeyringStoreSetGetDeleteKeys(t *testing.T) {
	keyring.MockInit() // in-memory keyring; no real OS keychain touched
	s := NewKeyringStore()

	if _, err := s.Get("ppp.anthropic"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing key, got %v", err)
	}
	if err := s.Set("ppp.anthropic", "sk-x"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Set("ppp.mybox.usai", "sk-y"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if v, err := s.Get("ppp.anthropic"); err != nil || v != "sk-x" {
		t.Fatalf("Get: %q %v", v, err)
	}
	keys, err := s.Keys()
	if err != nil {
		t.Fatalf("Keys: %v", err)
	}
	// index key is never listed
	for _, k := range keys {
		if k == keyIndexKey {
			t.Fatal("index key leaked into Keys()")
		}
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %v", keys)
	}
	if err := s.Delete("ppp.anthropic"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get("ppp.anthropic"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected key gone after delete, got %v", err)
	}
	if keys, _ := s.Keys(); len(keys) != 1 || keys[0] != "ppp.mybox.usai" {
		t.Errorf("expected only ppp.mybox.usai remaining, got %v", keys)
	}
}

func TestKeyringStoreDeleteMissingIsIdempotent(t *testing.T) {
	keyring.MockInit()
	s := NewKeyringStore()
	if err := s.Delete("ppp.nope"); err != nil {
		t.Errorf("Delete of missing key should be nil, got %v", err)
	}
}

func TestKeyringStoreReservedIndexKey(t *testing.T) {
	keyring.MockInit()
	s := NewKeyringStore()
	if err := s.Set(keyIndexKey, "x"); err == nil {
		t.Error("Set on reserved index key should error")
	}
}
