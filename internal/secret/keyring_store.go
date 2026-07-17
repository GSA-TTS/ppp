package secret

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

// keyringUser is the fixed "user" component of every keyring entry. ppp encodes
// all of the identifying scope (service, sandbox) in the key itself (the
// keyring "service" field), so a single constant user keeps entries uniform
// across the macOS Keychain, Windows Credential Manager, and the Linux Secret
// Service.
const keyringUser = "ppp"

// KeyringStore is the primary Store: it reads secrets from the OS keychain via
// go-keyring. It has no locked state — the keychain enforces its own access
// control — so Get never returns ErrLocked.
type KeyringStore struct{}

// NewKeyringStore returns a Store backed by the OS keychain.
func NewKeyringStore() *KeyringStore {
	return &KeyringStore{}
}

// Get reads the secret stored under key from the OS keychain. A missing entry
// is reported as ErrNotFound; any other backend failure is wrapped.
func (s *KeyringStore) Get(key string) (string, error) {
	v, err := keyring.Get(key, keyringUser)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("secret: keyring get %q: %w", key, err)
	}
	return v, nil
}
