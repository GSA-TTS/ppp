// Package supervisor spawns and manages the single long-lived mitmdump process
// that fronts every sandbox's WireGuard tunnel (spec §5.3, ADR-0002).
//
// It builds the mitmdump command with one --mode wireguard flag per pooled
// port, spawns it under a PTY (mitmdump block-buffers stdout when it is not a
// terminal, which would keep the client-config blocks from appearing promptly —
// spec §3.1 "impl note"), tees the combined output to $PPP_DATA/proxy.log, and
// waits until every instance has emitted its client config. Callers then read
// the captured configs (parsed by internal/proxy/capture) to build each
// sandbox's wg0.conf.
//
// The real process spawn is host-only (ticket #26/T13): mitmdump must be on
// PATH. Command construction, the mitmproxy version gate, and log parsing are
// unit-testable without spawning anything.
package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/GSA-TTS/ppp/internal/proxy/capture"
)

// SupportedMitmproxyVersion is the exact mitmproxy version ppp is pinned to
// (ADR-0003, spec §3.1). The WireGuard client-config format is verified against
// it; a different version fails the gate loudly rather than risking a silent
// parser mismatch.
const SupportedMitmproxyVersion = "12.2.3"

// mitmdumpBin is the mitmdump executable name; overridable in tests.
var mitmdumpBin = "mitmdump"

// Config configures a Supervisor.
type Config struct {
	// DataDir is $PPP_DATA. The WG keys files live under DataDir/wg/, the log at
	// DataDir/proxy.log, the PID file at DataDir/proxy.pid.
	DataDir string
	// Ports is the ordered set of WireGuard listen ports to start (one --mode
	// wireguard instance each). Emission order is non-deterministic, so callers
	// correlate captured configs by their Endpoint port, not by this order.
	Ports []int
	// AddonPath is the filesystem path to the extracted addon.py.
	AddonPath string
	// ReadyTimeout bounds how long Start waits for all client configs to appear.
	// Zero means DefaultReadyTimeout.
	ReadyTimeout time.Duration
}

// DefaultReadyTimeout is the default wait for all instances to emit their
// client configs.
const DefaultReadyTimeout = 30 * time.Second

// Supervisor owns a running mitmdump process.
type Supervisor struct {
	cfg Config

	mu      sync.Mutex
	cmd     *exec.Cmd
	logFile *os.File
	done    chan struct{}
}

// New returns a Supervisor for the given config. It does not start anything.
func New(cfg Config) (*Supervisor, error) {
	if cfg.DataDir == "" {
		return nil, errors.New("supervisor: DataDir is required")
	}
	if len(cfg.Ports) == 0 {
		return nil, errors.New("supervisor: at least one port is required")
	}
	if cfg.AddonPath == "" {
		return nil, errors.New("supervisor: AddonPath is required")
	}
	if cfg.ReadyTimeout == 0 {
		cfg.ReadyTimeout = DefaultReadyTimeout
	}
	return &Supervisor{cfg: cfg}, nil
}

// wgDir is $PPP_DATA/wg, holding one keys file per port.
func (s *Supervisor) wgDir() string { return filepath.Join(s.cfg.DataDir, "wg") }

// keysPath is the per-port WG keys file. mitmdump generates it if absent; ppp
// never pre-creates it empty (an empty/non-JSON file makes mitmdump error).
func (s *Supervisor) keysPath(port int) string {
	return filepath.Join(s.wgDir(), fmt.Sprintf("keys-%d.conf", port))
}

// logPath is $PPP_DATA/proxy.log (captured mitmdump stdout+stderr).
func (s *Supervisor) logPath() string { return filepath.Join(s.cfg.DataDir, "proxy.log") }

// pidPath is $PPP_DATA/proxy.pid.
func (s *Supervisor) pidPath() string { return filepath.Join(s.cfg.DataDir, "proxy.pid") }

// buildArgs constructs the mitmdump argv: one --mode wireguard:<keys>@<port>
// per port, plus the addon and the state-dir option. Never a shell string.
func (s *Supervisor) buildArgs() []string {
	argv := []string{mitmdumpBin}
	for _, port := range s.cfg.Ports {
		argv = append(argv, "--mode",
			fmt.Sprintf("wireguard:%s@%d", s.keysPath(port), port))
	}
	argv = append(argv,
		"-s", s.cfg.AddonPath,
		"--set", "ppp_state_dir="+s.cfg.DataDir,
	)
	return argv
}

// CheckVersion runs `mitmdump --version` and verifies it reports the pinned
// SupportedMitmproxyVersion. It fails closed on a mismatch so a drifted
// mitmproxy cannot silently break the client-config parser.
func CheckVersion(ctx context.Context) error {
	out, err := exec.CommandContext(ctx, mitmdumpBin, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("supervisor: running `mitmdump --version`: %w", err)
	}
	if !versionMatches(string(out), SupportedMitmproxyVersion) {
		return fmt.Errorf("supervisor: unsupported mitmproxy version; ppp is pinned to %s, got:\n%s",
			SupportedMitmproxyVersion, strings.TrimSpace(string(out)))
	}
	return nil
}

var versionRe = regexp.MustCompile(`\b(\d+\.\d+\.\d+)\b`)

// versionMatches reports whether the `mitmdump --version` output contains the
// wanted semver as its first version token (e.g. "Mitmproxy: 12.2.3").
func versionMatches(out, want string) bool {
	m := versionRe.FindString(out)
	return m == want
}

// Start spawns mitmdump under a PTY, tees output to proxy.log, writes the PID
// file, and waits until every port's client config has been captured. It
// Start spawns mitmdump as a DETACHED daemon (its own session, via Setsid),
// writing its combined stdout/stderr straight to proxy.log, writes the PID
// file, and waits until every port's client config has been captured by
// tailing proxy.log. It returns the parsed configs (indexed by their own
// Endpoint port). Because the child is detached and writes to the log file
// directly, it keeps running after the launching `ppp daemon start` process
// exits (spec §5.8/§6.15). Host-only: requires mitmdump on PATH.
//
// PYTHONUNBUFFERED=1 defeats Python's block-buffering to the (non-tty) file so
// the client-config blocks appear promptly without needing a PTY that would
// tie the child's controlling terminal to the ephemeral CLI process.
func (s *Supervisor) Start(ctx context.Context) ([]capture.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd != nil {
		return nil, errors.New("supervisor: already started")
	}
	if err := os.MkdirAll(s.wgDir(), 0o700); err != nil {
		return nil, fmt.Errorf("supervisor: creating wg dir: %w", err)
	}

	logFile, err := os.OpenFile(s.logPath(), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("supervisor: opening proxy log: %w", err)
	}

	argv := s.buildArgs()
	// Deliberately NOT exec.CommandContext: the daemon must outlive this CLI
	// invocation, so its lifetime is not bound to ctx. ctx still bounds the
	// readiness wait below.
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	// Detach into its own session so closing the CLI's controlling terminal
	// does not SIGHUP the daemon.
	cmd.SysProcAttr = detachSysProcAttr()

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, fmt.Errorf("supervisor: starting mitmdump: %w", err)
	}

	s.cmd = cmd
	s.logFile = logFile
	s.done = make(chan struct{})
	// Reap the child if it exits while this process is still alive (avoids a
	// zombie); harmless once we've detached.
	go func() {
		defer close(s.done)
		_ = cmd.Wait()
	}()

	if err := s.writePID(cmd.Process.Pid); err != nil {
		_ = s.stopLocked()
		return nil, err
	}

	cfgs, err := waitForConfigs(ctx, s.logPath(), len(s.cfg.Ports), s.cfg.ReadyTimeout)
	if err != nil {
		_ = s.stopLocked()
		return nil, err
	}
	if err := s.writeClientConfigs(cfgs); err != nil {
		_ = s.stopLocked()
		return nil, err
	}
	return cfgs, nil
}

// writeClientConfigs persists the captured configs to
// $PPP_DATA/wg/client-confs.json, indexed by listen port (as a string key), so
// `ppp run`/`create` can render each sandbox's wg0.conf without re-reading the
// log. Written atomically (temp + rename).
func (s *Supervisor) writeClientConfigs(cfgs []capture.Config) error {
	byPort := make(map[string]capture.Config, len(cfgs))
	for _, c := range cfgs {
		byPort[strconv.Itoa(c.ListenPort)] = c
	}
	data, err := json.MarshalIndent(byPort, "", "  ")
	if err != nil {
		return fmt.Errorf("supervisor: encoding client configs: %w", err)
	}
	path := filepath.Join(s.wgDir(), "client-confs.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("supervisor: writing client configs: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("supervisor: renaming client configs into place: %w", err)
	}
	return nil
}

// waitForConfigs polls the growing proxy.log until capture.Parse finds at least
// want blocks or the timeout elapses. It reads the log file (the detached child
// writes there directly), so it does not depend on holding the child's pipe.
func waitForConfigs(ctx context.Context, logPath string, want int, timeout time.Duration) ([]capture.Config, error) {
	deadline := time.After(timeout)
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		data, _ := os.ReadFile(logPath) // best-effort; may not exist for a tick
		if cfgs, err := capture.Parse(data); err == nil && len(cfgs) >= want {
			return cfgs, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline:
			data, _ := os.ReadFile(logPath)
			return nil, fmt.Errorf("supervisor: timed out after %s waiting for %d client configs (got output:\n%s)",
				timeout, want, tail(data, 2048))
		case <-ticker.C:
		}
	}
}

// writePID records the mitmdump PID for `ppp daemon status`/`stop`.
func (s *Supervisor) writePID(pid int) error {
	if err := os.WriteFile(s.pidPath(), []byte(fmt.Sprintf("%d\n", pid)), 0o600); err != nil {
		return fmt.Errorf("supervisor: writing pid file: %w", err)
	}
	return nil
}

// Stop terminates the mitmdump process, closes the PTY and log, and removes the
// PID file. Safe to call more than once.
func (s *Supervisor) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopLocked()
}

func (s *Supervisor) stopLocked() error {
	if s.cmd == nil {
		return nil
	}
	var firstErr error
	if s.cmd.Process != nil {
		if err := s.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			firstErr = err
		}
	}
	if s.done != nil {
		<-s.done // wait for the Wait() goroutine to reap the child
	}
	if s.logFile != nil {
		_ = s.logFile.Close()
	}
	_ = os.Remove(s.pidPath())
	s.cmd, s.logFile, s.done = nil, nil, nil
	return firstErr
}

// tail returns the last n bytes of b (for error context).
func tail(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return "…" + string(b[len(b)-n:])
}
