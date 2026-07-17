package portpool_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/GSA-TTS/ppp/internal/proxy/portpool"
)

// fakeLister is an injected test double for portpool.MachineLister.
type fakeLister struct {
	live []string
	err  error
}

func (f fakeLister) List() ([]string, error) { return f.live, f.err }

func TestFreeReturnsPortToPool(t *testing.T) {
	p := newTestPool(t)

	first, err := p.Allocate("s1")
	if err != nil {
		t.Fatalf("Allocate s1: %v", err)
	}
	if err := p.Free("s1"); err != nil {
		t.Fatalf("Free s1: %v", err)
	}

	reused, err := p.Allocate("s2")
	if err != nil {
		t.Fatalf("Allocate s2: %v", err)
	}
	if reused.Port != first.Port {
		t.Errorf("after free, reused port = %d, want reuse of %d", reused.Port, first.Port)
	}
}

func TestFreeUnknownSandbox(t *testing.T) {
	p := newTestPool(t)
	if err := p.Free("nope"); err == nil {
		t.Fatal("Free of unknown sandbox: want error, got nil")
	}
}

func TestExhaustion(t *testing.T) {
	tests := []struct {
		name string
		size int
	}{
		{"size 1", 1},
		{"size 3", 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "port-registry.json")
			p, err := portpool.New(path, portpool.WithSize(tc.size))
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			for i := 0; i < tc.size; i++ {
				if _, err := p.Allocate(sandboxName(i)); err != nil {
					t.Fatalf("Allocate %d: %v", i, err)
				}
			}
			_, err = p.Allocate("overflow")
			if !errors.Is(err, portpool.ErrExhausted) {
				t.Errorf("Allocate past size: err = %v, want ErrExhausted", err)
			}
		})
	}
}

func TestHardCapClamped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "port-registry.json")
	p, err := portpool.New(path, portpool.WithSize(1000))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if p.Size() != portpool.HardCap {
		t.Errorf("Size = %d, want clamp to HardCap %d", p.Size(), portpool.HardCap)
	}
}

func TestRemovingTombstonePreventsReuse(t *testing.T) {
	p, err := portpool.New(filepath.Join(t.TempDir(), "port-registry.json"), portpool.WithSize(2))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	a, err := p.Allocate("s1")
	if err != nil {
		t.Fatalf("Allocate s1: %v", err)
	}
	if err := p.MarkRemoving("s1"); err != nil {
		t.Fatalf("MarkRemoving s1: %v", err)
	}

	// While tombstoned, s1's port must not be reallocated; the next free port
	// (51821) is handed out instead.
	b, err := p.Allocate("s2")
	if err != nil {
		t.Fatalf("Allocate s2: %v", err)
	}
	if b.Port == a.Port {
		t.Errorf("tombstoned port %d was reused for s2", a.Port)
	}

	// After Free, the tombstoned port returns to the pool.
	if err := p.Free("s1"); err != nil {
		t.Fatalf("Free s1: %v", err)
	}
	c, err := p.Allocate("s3")
	if err != nil {
		t.Fatalf("Allocate s3: %v", err)
	}
	if c.Port != a.Port {
		t.Errorf("after Free, port = %d, want freed port %d", c.Port, a.Port)
	}
}

func TestPersistenceAcrossReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "port-registry.json")

	p1, err := portpool.New(path)
	if err != nil {
		t.Fatalf("New p1: %v", err)
	}
	if _, err := p1.Allocate("s1"); err != nil {
		t.Fatalf("Allocate s1: %v", err)
	}
	if _, err := p1.Allocate("s2"); err != nil {
		t.Fatalf("Allocate s2: %v", err)
	}

	// A fresh pool over the same file must see the prior allocations, so the
	// next allocation continues the sequence rather than reusing 51820.
	p2, err := portpool.New(path)
	if err != nil {
		t.Fatalf("New p2: %v", err)
	}
	next, err := p2.Allocate("s3")
	if err != nil {
		t.Fatalf("Allocate s3: %v", err)
	}
	if next.Port != 51822 {
		t.Errorf("after reload, next port = %d, want 51822", next.Port)
	}
}

func TestPersistedFileIsValidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "port-registry.json")
	p, err := portpool.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := p.Allocate("s1"); err != nil {
		t.Fatalf("Allocate: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Errorf("registry file is not valid JSON: %v", err)
	}
}

func TestReconcileFreesStaleEntries(t *testing.T) {
	tests := []struct {
		name      string
		allocated []string
		live      []string
		wantFreed []string
	}{
		{
			name:      "three entries one live frees two",
			allocated: []string{"s1", "s2", "s3"},
			live:      []string{"s2"},
			wantFreed: []string{"s1", "s3"},
		},
		{
			name:      "all live frees none",
			allocated: []string{"s1", "s2"},
			live:      []string{"s1", "s2"},
			wantFreed: nil,
		},
		{
			name:      "none live frees all",
			allocated: []string{"s1", "s2"},
			live:      []string{},
			wantFreed: []string{"s1", "s2"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestPool(t)
			for _, s := range tc.allocated {
				if _, err := p.Allocate(s); err != nil {
					t.Fatalf("Allocate %q: %v", s, err)
				}
			}

			freed, err := p.Reconcile(fakeLister{live: tc.live})
			if err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			if !equalStrings(freed, tc.wantFreed) {
				t.Errorf("freed = %v, want %v", freed, tc.wantFreed)
			}

			// Surviving allocations are exactly the live set.
			got := map[string]bool{}
			for _, a := range p.Allocations() {
				got[a.Sandbox] = true
			}
			for _, s := range tc.live {
				if !got[s] {
					t.Errorf("live sandbox %q was wrongly freed", s)
				}
			}
			for _, s := range tc.wantFreed {
				if got[s] {
					t.Errorf("stale sandbox %q was not freed", s)
				}
			}
		})
	}
}

func TestReconcileListerError(t *testing.T) {
	p := newTestPool(t)
	if _, err := p.Allocate("s1"); err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	_, err := p.Reconcile(fakeLister{err: errors.New("boom")})
	if err == nil {
		t.Fatal("Reconcile with lister error: want error, got nil")
	}
}

func sandboxName(i int) string {
	return fmt.Sprintf("s%d", i)
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
