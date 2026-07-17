package sandbox_test

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/GSA-TTS/ppp/internal/sandbox"
)

// TestWithLockRunsWhileHoldingLock exercises the happy path: the callback runs
// and its result is returned, and the lock file lives at <PPP_DATA>/state.lock.
func TestWithLockRunsWhileHoldingLock(t *testing.T) {
	data := t.TempDir()
	t.Setenv("PPP_DATA", data)

	ran := false
	err := sandbox.WithLock(func() error {
		ran = true
		return nil
	})
	if err != nil {
		t.Fatalf("WithLock() error = %v", err)
	}
	if !ran {
		t.Fatal("WithLock did not run the callback")
	}
	if _, statErr := os.Stat(filepath.Join(data, "state.lock")); statErr != nil {
		t.Errorf("state.lock not created: %v", statErr)
	}
}

// TestWithLockPropagatesCallbackError ensures the callback's error is returned
// unchanged (and the lock is still released — a follow-up lock succeeds).
func TestWithLockPropagatesCallbackError(t *testing.T) {
	data := t.TempDir()
	t.Setenv("PPP_DATA", data)

	sentinel := errors.New("boom")
	err := sandbox.WithLock(func() error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithLock() error = %v, want %v", err, sentinel)
	}

	// Lock must have been released; a second WithLock should succeed.
	if err := sandbox.WithLock(func() error { return nil }); err != nil {
		t.Fatalf("second WithLock() after error error = %v", err)
	}
}

// TestWithLockSerializesConcurrentCallers checks that two WithLock calls in the
// same process do not run their critical sections concurrently.
func TestWithLockSerializesConcurrentCallers(t *testing.T) {
	data := t.TempDir()
	t.Setenv("PPP_DATA", data)

	var mu sync.Mutex
	inside := 0
	maxInside := 0
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = sandbox.WithLock(func() error {
				mu.Lock()
				inside++
				if inside > maxInside {
					maxInside = inside
				}
				mu.Unlock()

				mu.Lock()
				inside--
				mu.Unlock()
				return nil
			})
		}()
	}
	wg.Wait()

	if maxInside > 1 {
		t.Errorf("observed %d concurrent critical sections, want 1", maxInside)
	}
}
