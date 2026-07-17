package sandbox

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
)

// WithLock runs fn while holding an exclusive flock on <PPP_DATA>/state.lock,
// serializing concurrent CLI operations that mutate sandbox state (spec §5.8).
//
// The lock is always released before WithLock returns, even if fn returns an
// error; fn's error is propagated unchanged. Acquisition blocks until the lock
// is available.
func WithLock(fn func() error) error {
	path, err := StateLockPath()
	if err != nil {
		return err
	}
	// The lock file lives directly under PPP_DATA; ensure that root exists.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating state dir for lock: %w", err)
	}

	lock := flock.New(path)
	if err := lock.Lock(); err != nil {
		return fmt.Errorf("acquiring state.lock: %w", err)
	}
	defer func() {
		// Unlock errors are non-fatal to fn's result but must not be silent.
		if unlockErr := lock.Unlock(); unlockErr != nil {
			// Surface via stderr; there is no logger wired into this package yet.
			fmt.Fprintf(os.Stderr, "ppp: releasing state.lock: %v\n", unlockErr)
		}
	}()

	return fn()
}
