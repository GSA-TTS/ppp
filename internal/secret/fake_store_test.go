package secret

// fakeStore is an in-memory Store used only in tests. It NEVER touches the OS
// keychain and never persists to disk, so tests carry no real credentials. A
// fakeStore may be put in a locked state to exercise the ErrLocked path that
// the age store exposes before unlock.
type fakeStore struct {
	data   map[string]string
	locked bool
}

func newFakeStore() *fakeStore {
	return &fakeStore{data: map[string]string{}}
}

func (f *fakeStore) set(key, value string) {
	f.data[key] = value
}

func (f *fakeStore) Get(key string) (string, error) {
	if f.locked {
		return "", ErrLocked
	}
	v, ok := f.data[key]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}
