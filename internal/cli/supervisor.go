package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/GSA-TTS/ppp/assets"
	"github.com/GSA-TTS/ppp/internal/catrust"
	"github.com/GSA-TTS/ppp/internal/proxy/portpool"
	"github.com/GSA-TTS/ppp/internal/proxy/supervisor"
	"github.com/GSA-TTS/ppp/internal/sandbox"
)

// ProxyStatus is the observable state of the single mitmdump proxy process
// (spec §6.15).
type ProxyStatus struct {
	// Running reports whether the proxy is considered up.
	Running bool
	// PID is the recorded mitmdump process id, or 0 when not running.
	PID int
}

// Supervisor manages the single mitmdump proxy process (spec §6.15). It is an
// interface so `daemon` commands are driven through a seam: the real host
// supervisor spawns/kills mitmdump, while tests inject a fake that records
// calls and returns canned status without touching a process.
type Supervisor interface {
	// Start ensures the proxy is running (no-op if already up).
	Start() error
	// Stop terminates the proxy if running (no-op if already down).
	Stop() error
	// Status reports the current proxy state.
	Status() (ProxyStatus, error)
}

// hostSupervisor is the production Supervisor. It spawns and controls the real
// mitmdump process via internal/proxy/supervisor. Status reads
// $PPP_DATA/proxy.pid and probes process liveness.
type hostSupervisor struct{}

// newHostSupervisor returns the production Supervisor.
func newHostSupervisor() *hostSupervisor { return &hostSupervisor{} }

// proxyPIDPath returns $PPP_DATA/proxy.pid (spec §5.8).
func proxyPIDPath() (string, error) {
	dataDir, err := sandbox.ResolveDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "proxy.pid"), nil
}

// Status reports whether a proxy is recorded (via $PPP_DATA/proxy.pid) and, if
// so, whether that PID is actually alive. A stale PID file (process gone) is
// reported as not running so `ppp daemon start` will re-spawn on the next
// invocation (on-demand recovery, wayfinder #7).
func (h *hostSupervisor) Status() (ProxyStatus, error) {
	path, err := proxyPIDPath()
	if err != nil {
		return ProxyStatus{}, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ProxyStatus{Running: false}, nil
	}
	if err != nil {
		return ProxyStatus{}, fmt.Errorf("reading %q: %w", path, err)
	}
	pid, perr := parsePID(data)
	if perr != nil {
		return ProxyStatus{}, fmt.Errorf("parsing %q: %w", path, perr)
	}
	if !processAlive(pid) {
		// Stale PID file: the recorded process is gone. Report not-running.
		return ProxyStatus{Running: false}, nil
	}
	return ProxyStatus{Running: true, PID: pid}, nil
}

// Start spawns the mitmdump proxy for the configured WireGuard port pool if it
// is not already running. It writes the client configs captured at startup to
// $PPP_DATA/wg/client-confs.json for per-sandbox rewrite.
func (h *hostSupervisor) Start() error {
	st, err := h.Status()
	if err != nil {
		return err
	}
	if st.Running {
		return nil // already up (idempotent)
	}

	dataDir, err := sandbox.ResolveDataDir()
	if err != nil {
		return err
	}
	addonPath, err := assets.WriteAddon(dataDir)
	if err != nil {
		return err
	}
	caBundle, err := writeUpstreamCABundle(dataDir)
	if err != nil {
		return err
	}
	ports, err := activePoolPorts(dataDir)
	if err != nil {
		return err
	}
	if len(ports) == 0 {
		// No sandbox has claimed a port yet: start the single base-port listener
		// so the daemon is up and ready for the first sandbox's tunnel.
		ports = []int{portpool.BasePort}
	}

	if err := supervisor.CheckVersion(context.Background()); err != nil {
		return err
	}
	sup, err := supervisor.New(supervisor.Config{
		DataDir:          dataDir,
		Ports:            ports,
		AddonPath:        addonPath,
		UpstreamCABundle: caBundle,
	})
	if err != nil {
		return err
	}
	if _, err := sup.Start(context.Background()); err != nil {
		return err
	}
	// The supervisor keeps running as our child; the PID file it wrote lets a
	// subsequent ppp invocation find and control it.
	return nil
}

// writeUpstreamCABundle composes the CA bundle mitmproxy uses to verify upstream
// (real server) TLS and writes it to $PPP_DATA/wg/upstream-ca.pem, returning its
// path. It is the host OS trust store minus CA certs OpenSSL 3 rejects
// (non-critical BasicConstraints). Normal public chains verify against it
// directly; interception chains are handled at handshake time by the addon's
// verify callback (ADR-0006), which authorizes them against the host trust
// store — so no probed/vendored interception cert is baked into this bundle.
// PPP_UPSTREAM_CA overrides the whole thing.
func writeUpstreamCABundle(dataDir string) (string, error) {
	bundle, err := catrust.Compose(os.Getenv("PPP_UPSTREAM_CA"))
	if err != nil {
		return "", fmt.Errorf("composing upstream CA bundle: %w", err)
	}
	path := filepath.Join(dataDir, "wg", "upstream-ca.pem")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, bundle, 0o600); err != nil {
		return "", fmt.Errorf("writing upstream CA bundle: %w", err)
	}
	return path, nil
}

// Stop terminates the recorded mitmdump process by PID and removes the PID file.
func (h *hostSupervisor) Stop() error {
	st, err := h.Status()
	if err != nil {
		return err
	}
	if !st.Running {
		return nil // already down (idempotent)
	}
	proc, err := os.FindProcess(st.PID)
	if err != nil {
		return fmt.Errorf("finding proxy process %d: %w", st.PID, err)
	}
	if err := proc.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("killing proxy process %d: %w", st.PID, err)
	}
	if path, perr := proxyPIDPath(); perr == nil {
		_ = os.Remove(path)
	}
	return nil
}

// activePoolPorts returns the WireGuard ports actually claimed by sandboxes in
// the port registry. Binding only claimed ports (rather than the whole
// 51820–51899 reservation) means a stray process on some unused pool port can
// no longer take down the entire proxy at start (diagnosis: one busy port in
// the full range killed mitmdump with "address already in use — exiting").
// Given the v1 single-active-sandbox limit (ADR-0007) this is normally one port.
func activePoolPorts(dataDir string) ([]int, error) {
	pool, err := portpool.New(filepath.Join(dataDir, "port-registry.json"))
	if err != nil {
		return nil, err
	}
	allocs := pool.Allocations()
	ports := make([]int, 0, len(allocs))
	for _, a := range allocs {
		ports = append(ports, a.Port)
	}
	sort.Ints(ports)
	return ports, nil
}

// parsePID parses a trimmed decimal PID from proxy.pid contents.
func parsePID(data []byte) (int, error) {
	var pid int
	if _, err := fmt.Sscanf(string(data), "%d", &pid); err != nil {
		return 0, fmt.Errorf("invalid pid contents %q", string(data))
	}
	if pid <= 0 {
		return 0, fmt.Errorf("non-positive pid %d", pid)
	}
	return pid, nil
}

// processAlive reports whether a process with the given PID exists. On Unix,
// signal 0 probes existence without affecting the process.
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(unixSignal0()) == nil
}

var _ Supervisor = (*hostSupervisor)(nil)
