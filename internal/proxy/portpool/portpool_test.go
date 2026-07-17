package portpool_test

import (
	"path/filepath"
	"testing"

	"github.com/GSA-TTS/ppp/internal/proxy/portpool"
)

// newTestPool builds a pool backed by a registry file in a temp dir, using the
// default size and cap unless the test overrides them.
func newTestPool(t *testing.T, opts ...portpool.Option) *portpool.Pool {
	t.Helper()
	path := filepath.Join(t.TempDir(), "port-registry.json")
	p, err := portpool.New(path, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

func TestAllocateSequential(t *testing.T) {
	p := newTestPool(t)

	tests := []struct {
		sandbox  string
		wantPort int
	}{
		{"ppp-red-bird", 51820},
		{"ppp-blue-fox", 51821},
		{"ppp-green-owl", 51822},
	}

	for _, tc := range tests {
		a, err := p.Allocate(tc.sandbox)
		if err != nil {
			t.Fatalf("Allocate(%q): %v", tc.sandbox, err)
		}
		if a.Port != tc.wantPort {
			t.Errorf("Allocate(%q).Port = %d, want %d", tc.sandbox, a.Port, tc.wantPort)
		}
	}
}

func TestPortIPMath(t *testing.T) {
	p := newTestPool(t)

	tests := []struct {
		name    string
		sandbox string
		wantN   int
		wantIP  string
	}{
		{"first port", "s1", 1, "10.0.0.1"},
		{"second port", "s2", 2, "10.0.0.2"},
		{"third port", "s3", 3, "10.0.0.3"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a, err := p.Allocate(tc.sandbox)
			if err != nil {
				t.Fatalf("Allocate: %v", err)
			}
			if a.N != tc.wantN {
				t.Errorf("N = %d, want %d", a.N, tc.wantN)
			}
			if a.InnerIP != tc.wantIP {
				t.Errorf("InnerIP = %q, want %q", a.InnerIP, tc.wantIP)
			}
			if got := a.Port - 51819; got != a.N {
				t.Errorf("port-51819 = %d, want N=%d", got, a.N)
			}
		})
	}
}
