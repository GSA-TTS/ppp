package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/GSA-TTS/ppp/internal/sandbox"
)

// ProxyStatus is the observable state of the single mitmdump proxy process
// (spec §6.15). It is intentionally small: T12 wires only what can be known
// from state (the PID file); the richer per-sandbox listener detail lands with
// the real supervisor in T13.
type ProxyStatus struct {
	// Running reports whether the proxy is considered up.
	Running bool
	// PID is the recorded mitmdump process id, or 0 when not running.
	PID int
}

// Supervisor manages the single mitmdump proxy process (spec §6.15). It is an
// interface so `daemon` commands are driven through a seam: the real host
// supervisor spawns/kills mitmdump (host-only, T13), while tests inject a fake
// that records calls and returns canned status without touching a process.
type Supervisor interface {
	// Start ensures the proxy is running (no-op if already up).
	Start() error
	// Stop terminates the proxy if running (no-op if already down).
	Stop() error
	// Status reports the current proxy state.
	Status() (ProxyStatus, error)
}

// hostSupervisor is the production Supervisor. Its Status is fully wired in
// T12: it reads $PPP_DATA/proxy.pid to report whether a proxy is recorded as
// running. Start/Stop spawn and signal the real mitmdump process, which is
// host-only and lands in T13; until then they report a clear deferral rather
// than pretending to succeed.
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

// Status reports whether $PPP_DATA/proxy.pid exists and, if so, the recorded
// PID. It does not send a liveness signal to the process (that liveness probe
// is part of the host-only supervisor in T13); a present PID file means
// "recorded as running".
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
	return ProxyStatus{Running: true, PID: pid}, nil
}

// Start spawns the mitmdump process. Real process spawn is host-only (T13).
func (h *hostSupervisor) Start() error {
	return errProxyHostOnly("start")
}

// Stop signals the mitmdump process. Real process control is host-only (T13).
func (h *hostSupervisor) Stop() error {
	return errProxyHostOnly("stop")
}

// errProxyHostOnly reports that a proxy process operation is deferred to T13.
func errProxyHostOnly(op string) error {
	return fmt.Errorf("daemon %s: spawning/controlling the mitmdump process is not implemented on this host yet (T13)", op)
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

var _ Supervisor = (*hostSupervisor)(nil)
