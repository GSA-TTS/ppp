package secret

import "errors"

// A Store retrieves a stored secret value by its fully-qualified key (for
// example "ppp.anthropic" or "ppp.ppp-red-bird.usai"). It is the single seam
// the Resolver depends on, so the resolution logic can be exercised against a
// fake in-memory store without touching the OS keychain or the age file.
//
// Implementations MUST distinguish two conditions with sentinel errors so
// callers (and, later, the T8 UDS server) can react precisely:
//   - ErrNotFound  — the key is absent; this is a normal "no secret" outcome.
//   - ErrLocked    — the backing store exists but is sealed and has not been
//     unlocked this session (the age fallback before Unlock). It is distinct
//     from ErrNotFound so the server can report reason:"locked" rather than a
//     silent miss.
//
// Any other error is an unexpected backend failure and should be wrapped.
type Store interface {
	Get(key string) (string, error)
}

var (
	// ErrNotFound signals that a key is absent from the store. It is not a
	// failure: the Resolver translates it into ok=false with a nil error.
	ErrNotFound = errors.New("secret: key not found")

	// ErrLocked signals that a store is sealed (e.g. the age fallback before
	// it has been unlocked this session). Resolution against a locked store
	// fails closed with this sentinel rather than returning a phantom miss.
	ErrLocked = errors.New("secret: store is locked")
)
