package portpool_test

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/GSA-TTS/ppp/internal/proxy/portpool"
)

// TestConcurrentAllocateNoDuplicatePorts runs many allocations in parallel and
// asserts every caller got a distinct port — guarding against a race in the
// allocator's shared state.
func TestConcurrentAllocateNoDuplicatePorts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "port-registry.json")
	p, err := portpool.New(path, portpool.WithSize(portpool.HardCap))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	const n = portpool.HardCap
	var wg sync.WaitGroup
	var mu sync.Mutex
	ports := make(map[int]int)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			a, err := p.Allocate(sandboxName(i))
			if err != nil {
				t.Errorf("Allocate %d: %v", i, err)
				return
			}
			mu.Lock()
			ports[a.Port]++
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	if len(ports) != n {
		t.Errorf("got %d distinct ports, want %d", len(ports), n)
	}
	for port, count := range ports {
		if count != 1 {
			t.Errorf("port %d allocated %d times, want 1", port, count)
		}
	}
}
