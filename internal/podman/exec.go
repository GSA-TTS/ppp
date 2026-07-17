package podman

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
)

// Runner is the real PodmanRunner that shells out to the `podman` binary.
//
// Argv construction lives in the exported *Args functions (fully unit-tested,
// no process spawned). Each lifecycle method builds and validates its argv,
// then executes it via runQuiet/runOutput. exec.CommandContext is always given
// separate arguments — never a shell string — so nothing here is subject to
// shell interpolation (spec §5.1, ADR-0001).
type Runner struct {
	// provider is the provider this runner targets; zero value defaults to
	// the host autodetected provider via Provider().
	provider Provider
}

// NewRunner returns a real shell-out Runner targeting the host's default
// provider (spec §5.1 autodetection).
func NewRunner() *Runner {
	return &Runner{provider: DetectProvider()}
}

// NewRunnerWithProvider returns a Runner that names the given provider
// explicitly (validated when it reaches argv, e.g. in InitArgs).
func NewRunnerWithProvider(p Provider) *Runner {
	return &Runner{provider: p}
}

// Provider reports the provider this runner targets.
func (r *Runner) Provider() Provider {
	if r.provider == "" {
		return DetectProvider()
	}
	return r.provider
}

// CommandError carries the failing argv and captured stderr so callers (and
// `ppp diagnose`) can see exactly what podman was asked to do and why it failed.
// It deliberately does not include stdout, which may be large.
type CommandError struct {
	Argv   []string
	Err    error
	Stderr string
}

func (e *CommandError) Error() string {
	// Argv[0:3] is the stable "podman machine <verb>" prefix; include the full
	// argv for actionable diagnostics. Stderr is trimmed by the caller.
	msg := fmt.Sprintf("podman: command failed: %v: %v", e.Argv, e.Err)
	if e.Stderr != "" {
		msg += "\nstderr: " + e.Stderr
	}
	return msg
}

func (e *CommandError) Unwrap() error { return e.Err }

// runOutput executes argv and returns its stdout. stderr is captured separately
// and, on failure, wrapped into a CommandError so it never contaminates the
// stdout a caller parses (e.g. JSON from `machine list`).
func (r *Runner) runOutput(ctx context.Context, argv []string) ([]byte, error) {
	cmd, err := command(ctx, argv)
	if err != nil {
		return nil, err
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, &CommandError{Argv: argv, Err: err, Stderr: trimStderr(stderr.String())}
	}
	return stdout.Bytes(), nil
}

// runQuiet executes argv and discards stdout, returning only an error. Used for
// lifecycle verbs (init/start/stop/rm/cp) whose stdout is not consumed.
func (r *Runner) runQuiet(ctx context.Context, argv []string) error {
	_, err := r.runOutput(ctx, argv)
	return err
}

// trimStderr bounds captured stderr so a runaway podman error cannot bloat an
// error value or a diagnostic log.
func trimStderr(s string) string {
	const max = 4096
	if len(s) > max {
		return s[:max] + "…(truncated)"
	}
	return s
}

func (r *Runner) Init(ctx context.Context, opts InitOptions) error {
	argv, err := InitArgs(opts)
	if err != nil {
		return err
	}
	return r.runQuiet(ctx, argv)
}

func (r *Runner) Start(ctx context.Context, name string) error {
	argv, err := StartArgs(name)
	if err != nil {
		return err
	}
	return r.runQuiet(ctx, argv)
}

func (r *Runner) Stop(ctx context.Context, name string) error {
	argv, err := StopArgs(name)
	if err != nil {
		return err
	}
	return r.runQuiet(ctx, argv)
}

func (r *Runner) Rm(ctx context.Context, name string, force bool) error {
	argv, err := RmArgs(name, force)
	if err != nil {
		return err
	}
	return r.runQuiet(ctx, argv)
}

func (r *Runner) SSH(ctx context.Context, name string, command ...string) ([]byte, error) {
	argv, err := SSHArgs(name, command...)
	if err != nil {
		return nil, err
	}
	return r.runOutput(ctx, argv)
}

func (r *Runner) Cp(ctx context.Context, name, localPath, remotePath string) error {
	argv, err := CpArgs(name, localPath, remotePath)
	if err != nil {
		return err
	}
	return r.runQuiet(ctx, argv)
}

func (r *Runner) List(ctx context.Context) ([]Machine, error) {
	out, err := r.runOutput(ctx, ListArgs())
	if err != nil {
		return nil, err
	}
	// `podman machine list --format json` prints `[]` (or nothing on some
	// versions) when there are no machines; treat empty output as no machines.
	if len(bytes.TrimSpace(out)) == 0 {
		return nil, nil
	}
	return decodeMachines(out)
}

func (r *Runner) Inspect(ctx context.Context, name string) ([]byte, error) {
	argv, err := InspectArgs(name)
	if err != nil {
		return nil, err
	}
	return r.runOutput(ctx, argv)
}

// command builds the *exec.Cmd used to run an argv. It is the single point
// where argv becomes a process: exec.CommandContext(argv[0], argv[1:]...) —
// separate args, never a shell string.
func command(ctx context.Context, argv []string) (*exec.Cmd, error) {
	if len(argv) == 0 {
		return nil, errors.New("podman: empty argv")
	}
	return exec.CommandContext(ctx, argv[0], argv[1:]...), nil
}

// decodeMachines parses `podman machine list --format json` output. Factored
// out so the JSON contract is testable without shelling out.
func decodeMachines(raw []byte) ([]Machine, error) {
	var machines []Machine
	if err := json.Unmarshal(raw, &machines); err != nil {
		return nil, fmt.Errorf("podman: decoding machine list: %w", err)
	}
	return machines, nil
}
