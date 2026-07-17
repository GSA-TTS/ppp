package cli

import (
	"sync"

	"github.com/GSA-TTS/ppp/internal/secret"
)

// errNotFound is the sentinel a fakeStore returns for a missing key, matching
// the secret package's ErrNotFound contract.
var errNotFound = secret.ErrNotFound

// fakeStore is an in-memory mutableStore for command tests. It NEVER touches
// the OS keychain and never persists to disk, so tests carry no real
// credentials. It records Set/Delete so tests can assert on mutations, and
// implements Keys() for `secret ls`.
type fakeStore struct {
	mu   sync.Mutex
	data map[string]string
}

func newTestStore() *fakeStore {
	return &fakeStore{data: map[string]string{}}
}

func (f *fakeStore) Get(key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.data[key]
	if !ok {
		return "", errNotFound
	}
	return v, nil
}

func (f *fakeStore) Set(key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data[key] = value
	return nil
}

func (f *fakeStore) Delete(key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.data, key)
	return nil
}

func (f *fakeStore) Keys() ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	keys := make([]string, 0, len(f.data))
	for k := range f.data {
		keys = append(keys, k)
	}
	return keys, nil
}

var _ mutableStore = (*fakeStore)(nil)
