package portpool

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
)

const (
	// BasePort is the first WireGuard listen port in the pool.
	BasePort = 51820

	// portIndexOffset converts a port to its 1-based index N: N = port - 51819.
	// So BasePort (51820) is N=1.
	portIndexOffset = BasePort - 1

	// DefaultSize is the default number of ports the pool will hand out.
	DefaultSize = 32

	// HardCap is the maximum pool size regardless of configuration (spec §5.3
	// reserves ports 51820–51899, i.e. 80 sandboxes).
	HardCap = 80
)

// state is the lifecycle of a single allocation.
type state string

const (
	// stateActive means the port is in use by a live sandbox.
	stateActive state = "active"

	// stateRemoving is a tombstone: the sandbox is being torn down, so the port
	// is held out of the free pool until [Pool.Free] confirms teardown. This
	// prevents an in-flight teardown from having its port reallocated.
	stateRemoving state = "removing"
)

// ErrExhausted is returned by [Pool.Allocate] when no free port remains within
// the configured pool size.
var ErrExhausted = errors.New("portpool: no free port available")

// MachineLister reports the set of live sandbox names. It is injected so callers
// supply a real podman-backed implementation and tests supply a fake; this
// package never imports the podman package (avoids a cross-package cycle).
type MachineLister interface {
	// List returns the names of sandboxes/machines that currently exist.
	List() ([]string, error)
}

// Allocation is a single port assignment returned to callers.
type Allocation struct {
	// Port is the WireGuard listen port; it is the sandbox's identity (ADR-0003).
	Port int
	// N is the 1-based pool index, N = Port - 51819.
	N int
	// InnerIP is the derived inner tunnel IP, 10.0.0.N. It is derived from the
	// port and never tracked separately, so freeing the port frees the IP.
	InnerIP string
	// Sandbox is the name of the sandbox holding the port.
	Sandbox string
}

// entry is the persisted record for one port in the registry file.
type entry struct {
	Sandbox string `json:"sandbox"`
	State   state  `json:"state"`
}

// Pool allocates and frees WireGuard ports (and their derived inner IPs) and
// persists the port-to-sandbox mapping. It is safe for concurrent use.
type Pool struct {
	path string
	size int

	mu      sync.Mutex
	entries map[int]entry // keyed by port
}

// Option configures a [Pool].
type Option func(*config)

type config struct {
	size int
}

// WithSize sets the number of ports the pool will hand out. Values above
// [HardCap] are clamped to [HardCap]; values below 1 fall back to [DefaultSize].
func WithSize(n int) Option {
	return func(c *config) { c.size = n }
}

// New creates a pool whose registry is persisted at path (a port-registry.json
// file). If the file exists, its allocations are loaded so state survives a
// daemon restart.
func New(path string, opts ...Option) (*Pool, error) {
	cfg := config{size: DefaultSize}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.size < 1 {
		cfg.size = DefaultSize
	}
	if cfg.size > HardCap {
		cfg.size = HardCap
	}

	p := &Pool{
		path:    path,
		size:    cfg.size,
		entries: make(map[int]entry),
	}
	if err := p.load(); err != nil {
		return nil, err
	}
	return p, nil
}

// Size reports the configured number of allocatable ports.
func (p *Pool) Size() int { return p.size }

// portToN converts a port to its 1-based pool index.
func portToN(port int) int { return port - portIndexOffset }

// innerIP derives the inner tunnel IP for a pool index N.
func innerIP(n int) string { return fmt.Sprintf("10.0.0.%d", n) }

// newAllocation builds an [Allocation] for a port held by sandbox.
func newAllocation(port int, sandbox string) Allocation {
	n := portToN(port)
	return Allocation{Port: port, N: n, InnerIP: innerIP(n), Sandbox: sandbox}
}

// Allocate assigns the next free port (and its derived inner IP) to sandbox and
// persists the change. Ports held by a "removing" tombstone are not reused until
// freed. Reusing a previously freed port is safe because identity is bound to
// the listen port and its per-port keypair, not the inner IP (ADR-0003).
func (p *Pool) Allocate(sandbox string) (Allocation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i := 0; i < p.size; i++ {
		port := BasePort + i
		if _, taken := p.entries[port]; taken {
			continue
		}
		p.entries[port] = entry{Sandbox: sandbox, State: stateActive}
		if err := p.save(); err != nil {
			delete(p.entries, port)
			return Allocation{}, err
		}
		return newAllocation(port, sandbox), nil
	}
	return Allocation{}, fmt.Errorf("%w: pool size %d exhausted", ErrExhausted, p.size)
}

// MarkRemoving tombstones the port held by sandbox so it is not reallocated
// while teardown is in progress. It is a no-op-safe error if the sandbox holds
// no port.
func (p *Pool) MarkRemoving(sandbox string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	port, ok := p.portOf(sandbox)
	if !ok {
		return fmt.Errorf("portpool: sandbox %q holds no port", sandbox)
	}
	prev := p.entries[port]
	p.entries[port] = entry{Sandbox: sandbox, State: stateRemoving}
	if err := p.save(); err != nil {
		p.entries[port] = prev
		return err
	}
	return nil
}

// Free releases the port held by sandbox back into the pool and persists the
// change. Freeing the port frees its derived inner IP automatically.
func (p *Pool) Free(sandbox string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	port, ok := p.portOf(sandbox)
	if !ok {
		return fmt.Errorf("portpool: sandbox %q holds no port", sandbox)
	}
	prev := p.entries[port]
	delete(p.entries, port)
	if err := p.save(); err != nil {
		p.entries[port] = prev
		return err
	}
	return nil
}

// Reconcile frees any registry entry whose sandbox is not reported live by the
// lister — stale entries left by a crash. It is called on daemon start. The
// number of freed sandbox names is returned, sorted for deterministic output.
func (p *Pool) Reconcile(lister MachineLister) ([]string, error) {
	live, err := lister.List()
	if err != nil {
		return nil, fmt.Errorf("portpool: listing live sandboxes: %w", err)
	}
	liveSet := make(map[string]struct{}, len(live))
	for _, name := range live {
		liveSet[name] = struct{}{}
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	var freed []string
	for port, e := range p.entries {
		if _, ok := liveSet[e.Sandbox]; !ok {
			delete(p.entries, port)
			freed = append(freed, e.Sandbox)
		}
	}
	if len(freed) == 0 {
		return nil, nil
	}
	if err := p.save(); err != nil {
		return nil, err
	}
	sort.Strings(freed)
	return freed, nil
}

// Allocations returns the current active and removing allocations, sorted by
// port, for inspection (e.g. `ppp ports`).
func (p *Pool) Allocations() []Allocation {
	p.mu.Lock()
	defer p.mu.Unlock()

	ports := make([]int, 0, len(p.entries))
	for port := range p.entries {
		ports = append(ports, port)
	}
	sort.Ints(ports)

	out := make([]Allocation, 0, len(ports))
	for _, port := range ports {
		out = append(out, newAllocation(port, p.entries[port].Sandbox))
	}
	return out
}

// portOf returns the port held by sandbox. The caller must hold p.mu.
func (p *Pool) portOf(sandbox string) (int, bool) {
	for port, e := range p.entries {
		if e.Sandbox == sandbox {
			return port, true
		}
	}
	return 0, false
}

// registryFile is the on-disk shape of port-registry.json: port (as a string
// key) → entry.
type registryFile struct {
	Ports map[string]entry `json:"ports"`
}

// load reads the registry file into memory. A missing file is not an error (a
// fresh pool starts empty). The caller need not hold p.mu (New is not shared).
func (p *Pool) load() error {
	data, err := os.ReadFile(p.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("portpool: reading registry %q: %w", p.path, err)
	}

	var rf registryFile
	if err := json.Unmarshal(data, &rf); err != nil {
		return fmt.Errorf("portpool: parsing registry %q: %w", p.path, err)
	}
	for key, e := range rf.Ports {
		var port int
		if _, err := fmt.Sscanf(key, "%d", &port); err != nil {
			return fmt.Errorf("portpool: bad port key %q in registry: %w", key, err)
		}
		p.entries[port] = e
	}
	return nil
}

// save writes the registry atomically (temp file + rename) so a crash mid-write
// cannot corrupt the registry. The caller must hold p.mu.
func (p *Pool) save() error {
	rf := registryFile{Ports: make(map[string]entry, len(p.entries))}
	for port, e := range p.entries {
		rf.Ports[fmt.Sprintf("%d", port)] = e
	}
	data, err := json.MarshalIndent(rf, "", "  ")
	if err != nil {
		return fmt.Errorf("portpool: encoding registry: %w", err)
	}

	tmp := p.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("portpool: writing temp registry: %w", err)
	}
	if err := os.Rename(tmp, p.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("portpool: renaming registry into place: %w", err)
	}
	return nil
}
