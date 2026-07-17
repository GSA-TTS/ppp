package podman

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
)

// ErrNotImplemented marks the host-only real shell-out behavior that lands in
// T13. The argv-building logic is fully implemented and tested here; only the
// actual process execution is deferred.
var ErrNotImplemented = errors.New("podman: real shell-out not implemented (host-only, T13)")

// Runner is the real PodmanRunner that shells out to the `podman` binary.
//
// Argv construction is complete and tested via the exported *Args functions;
// the process-execution body is host-only and lands in T13. Until then the
// lifecycle methods build (and validate) their argv, then return
// ErrNotImplemented so no un-vetted argv is ever executed and tests never
// spawn a real process.
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

// Provider reports the provider this runner targets.
func (r *Runner) Provider() Provider {
	if r.provider == "" {
		return DetectProvider()
	}
	return r.provider
}

func (r *Runner) Init(_ context.Context, opts InitOptions) error {
	if _, err := InitArgs(opts); err != nil {
		return err
	}
	return ErrNotImplemented
}

func (r *Runner) Start(_ context.Context, name string) error {
	if _, err := StartArgs(name); err != nil {
		return err
	}
	return ErrNotImplemented
}

func (r *Runner) Stop(_ context.Context, name string) error {
	if _, err := StopArgs(name); err != nil {
		return err
	}
	return ErrNotImplemented
}

func (r *Runner) Rm(_ context.Context, name string, force bool) error {
	if _, err := RmArgs(name, force); err != nil {
		return err
	}
	return ErrNotImplemented
}

func (r *Runner) SSH(_ context.Context, name string, command ...string) ([]byte, error) {
	if _, err := SSHArgs(name, command...); err != nil {
		return nil, err
	}
	return nil, ErrNotImplemented
}

func (r *Runner) Cp(_ context.Context, name, localPath, remotePath string) error {
	if _, err := CpArgs(name, localPath, remotePath); err != nil {
		return err
	}
	return ErrNotImplemented
}

func (r *Runner) List(_ context.Context) ([]Machine, error) {
	_ = ListArgs()
	return nil, ErrNotImplemented
}

func (r *Runner) Inspect(_ context.Context, name string) ([]byte, error) {
	if _, err := InspectArgs(name); err != nil {
		return nil, err
	}
	return nil, ErrNotImplemented
}

// command builds the *exec.Cmd the real (T13) implementation will run. It is
// the single point where argv becomes a process: exec.Command(argv[0],
// argv[1:]...) — separate args, never a shell string. Present now so the argv
// contract and the exec shape are co-located; not invoked by tests.
func command(ctx context.Context, argv []string) (*exec.Cmd, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("podman: empty argv")
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
