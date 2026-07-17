package secret

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sync"

	"filippo.io/age"
	"gopkg.in/yaml.v3"
)

// AgeStore is the fallback Store used only where no OS keychain backend is
// available (spec §5.6, ADR-0004). Secrets live in an age-encrypted file
// (conventionally $PPP_DATA/secrets.age) as a small YAML map of key→value.
//
// The store models a locked state: it starts locked and Get returns ErrLocked
// until Unlock decrypts the file once at daemon start (via PPP_AGE_PASSPHRASE
// or an interactive prompt). Decryption happens exactly once — the plaintext
// map is held in memory and never re-derived per request — matching the
// "parent-only decryption; unlock once" requirement. AgeStore is safe for
// concurrent Get calls after Unlock.
type AgeStore struct {
	path string

	mu       sync.RWMutex
	unlocked bool
	secrets  map[string]string
}

// NewAgeStore returns a locked AgeStore reading from the age file at path.
// Call Unlock before Get, or Get returns ErrLocked.
func NewAgeStore(path string) *AgeStore {
	return &AgeStore{path: path}
}

// Unlock decrypts the age file with the given passphrase and caches the
// plaintext secret map in memory. It is idempotent: a second successful call
// simply refreshes the cache. On failure the store remains locked and the error
// is returned. Unlock is intended to run once at daemon start, never
// per-request.
func (s *AgeStore) Unlock(passphrase string) error {
	secrets, err := decryptAgeFile(s.path, passphrase)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.secrets = secrets
	s.unlocked = true
	s.mu.Unlock()
	return nil
}

// Get returns the secret stored under key. Before Unlock it returns ErrLocked;
// after Unlock a missing key returns ErrNotFound.
func (s *AgeStore) Get(key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.unlocked {
		return "", ErrLocked
	}
	v, ok := s.secrets[key]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

// decryptAgeFile reads and decrypts the age file, unmarshaling the YAML map.
func decryptAgeFile(path, passphrase string) (map[string]string, error) {
	id, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, fmt.Errorf("secret: age identity: %w", err)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("secret: open age store: %w", err)
	}
	defer func() { _ = f.Close() }() // read-only handle; close error is not actionable

	r, err := age.Decrypt(f, id)
	if err != nil {
		return nil, fmt.Errorf("secret: decrypt age store: %w", err)
	}
	plaintext, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("secret: read age store: %w", err)
	}

	secrets := map[string]string{}
	if err := yaml.Unmarshal(plaintext, &secrets); err != nil {
		return nil, fmt.Errorf("secret: parse age store: %w", err)
	}
	return secrets, nil
}

// WriteAgeStore encrypts the given secret map to an age file at path using a
// scrypt passphrase. It is used by the CLI's `secret set` path and by tests to
// build a fixture store; it does not mutate any in-memory AgeStore.
func WriteAgeStore(path, passphrase string, secrets map[string]string) error {
	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return fmt.Errorf("secret: age recipient: %w", err)
	}
	plaintext, err := yaml.Marshal(secrets)
	if err != nil {
		return fmt.Errorf("secret: marshal secrets: %w", err)
	}

	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recipient)
	if err != nil {
		return fmt.Errorf("secret: age encrypt: %w", err)
	}
	if _, err := w.Write(plaintext); err != nil {
		return fmt.Errorf("secret: write age stream: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("secret: close age stream: %w", err)
	}

	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("secret: write age file: %w", err)
	}
	return nil
}
