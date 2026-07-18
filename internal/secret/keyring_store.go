package secret

import (
	"errors"
	"fmt"
	"sort"

	"github.com/zalando/go-keyring"
)

// keyringUser is the fixed "user" component of every keyring entry. ppp encodes
// all of the identifying scope (service, sandbox) in the key itself (the
// keyring "service" field), so a single constant user keeps entries uniform
// across the macOS Keychain, Windows Credential Manager, and the Linux Secret
// Service.
const keyringUser = "ppp"

// keyIndexEntry is the keyring entry that records the set of ppp keys, so `ls`
// and cleanup can enumerate them (the OS keyrings have no portable "list keys
// for a service prefix" API). It is stored like any other entry, under this
// reserved key, as a newline-joined list.
const keyIndexKey = "ppp.__index__"

// KeyringStore is the primary Store: it reads and writes secrets in the OS
// keychain via go-keyring. It has no locked state — the keychain enforces its
// own access control — so Get never returns ErrLocked.
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

// Set stores value under key in the OS keychain and records key in the index so
// it can later be listed and removed. On macOS this may prompt for keychain
// approval the first time.
func (s *KeyringStore) Set(key, value string) error {
	if key == keyIndexKey {
		return fmt.Errorf("secret: %q is reserved", key)
	}
	if err := keyring.Set(key, keyringUser, value); err != nil {
		return fmt.Errorf("secret: keyring set %q: %w", key, err)
	}
	return s.indexAdd(key)
}

// Delete removes key from the OS keychain and the index. A missing entry is not
// an error (delete is idempotent).
func (s *KeyringStore) Delete(key string) error {
	if key == keyIndexKey {
		return fmt.Errorf("secret: %q is reserved", key)
	}
	err := keyring.Delete(key, keyringUser)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("secret: keyring delete %q: %w", key, err)
	}
	return s.indexRemove(key)
}

// Keys returns the stored ppp secret keys (from the index), sorted, self-healing
// the index: any indexed key whose value no longer exists (e.g. an index update
// failed after a Delete) is dropped so `ls` stays truthful. The index itself is
// never returned.
//
// Note: Set/Delete update the value and the index non-atomically, so a partial
// failure can briefly leave the index out of sync (a stored-but-unlisted orphan,
// or a listed-but-missing phantom). Keys() heals the phantom case; a subsequent
// successful Set of an orphan re-adds it. No secret is lost or leaked either way.
func (s *KeyringStore) Keys() ([]string, error) {
	keys, err := s.index()
	if err != nil {
		return nil, err
	}
	live := keys[:0]
	changed := false
	for _, k := range keys {
		if _, gerr := keyring.Get(k, keyringUser); errors.Is(gerr, keyring.ErrNotFound) {
			changed = true
			continue // phantom index entry; drop it
		}
		live = append(live, k)
	}
	if changed {
		_ = s.writeIndex(live) // best-effort heal; ls is still correct if it fails
	}
	sort.Strings(live)
	return live, nil
}

// index reads the key index, returning an empty slice when it does not exist.
func (s *KeyringStore) index() ([]string, error) {
	v, err := keyring.Get(keyIndexKey, keyringUser)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("secret: keyring get index: %w", err)
	}
	return splitIndex(v), nil
}

func (s *KeyringStore) indexAdd(key string) error {
	keys, err := s.index()
	if err != nil {
		return err
	}
	for _, k := range keys {
		if k == key {
			return nil // already present
		}
	}
	keys = append(keys, key)
	return s.writeIndex(keys)
}

func (s *KeyringStore) indexRemove(key string) error {
	keys, err := s.index()
	if err != nil {
		return err
	}
	out := keys[:0]
	for _, k := range keys {
		if k != key {
			out = append(out, k)
		}
	}
	return s.writeIndex(out)
}

func (s *KeyringStore) writeIndex(keys []string) error {
	if err := keyring.Set(keyIndexKey, keyringUser, joinIndex(keys)); err != nil {
		return fmt.Errorf("secret: keyring set index: %w", err)
	}
	return nil
}

func splitIndex(v string) []string {
	if v == "" {
		return nil
	}
	var out []string
	for _, k := range splitLines(v) {
		if k != "" {
			out = append(out, k)
		}
	}
	return out
}

func joinIndex(keys []string) string {
	s := ""
	for i, k := range keys {
		if i > 0 {
			s += "\n"
		}
		s += k
	}
	return s
}

func splitLines(v string) []string {
	var out []string
	start := 0
	for i := 0; i < len(v); i++ {
		if v[i] == '\n' {
			out = append(out, v[start:i])
			start = i + 1
		}
	}
	out = append(out, v[start:])
	return out
}
