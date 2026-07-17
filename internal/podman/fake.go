package podman

import (
	"context"
	"fmt"
	"sync"
)

// Call records a single argv slice the Fake was asked to run, tagged with the
// operation name, so other packages' tests can assert on both the operation
// and its exact argv.
type Call struct {
	Op   string
	Argv []string
}

// Fake is an in-memory PodmanRunner for unit tests. It performs the same name
// validation and argv construction as the real Runner (so tests exercise the
// real invariants), records every call's argv, and returns canned List/Inspect
// results instead of shelling out. Fake is exported for use by other packages'
// tests. The zero value is ready to use; it is safe for concurrent use.
type Fake struct {
	mu sync.Mutex

	// Calls is the ordered log of operations, each carrying the argv the Fake
	// built for it.
	Calls []Call

	// ListResult is returned by List (nil => empty slice).
	ListResult []Machine
	// ListErr, if set, is returned by List instead of ListResult.
	ListErr error

	// InspectResult is returned by Inspect keyed by machine name; a missing
	// key yields InspectDefault.
	InspectResult map[string][]byte
	// InspectDefault is returned by Inspect when no per-name result is set.
	InspectDefault []byte

	// SSHResult is the canned combined output returned by SSH.
	SSHResult []byte

	// ProviderValue is reported by Provider; zero value falls back to the host
	// default (DetectProvider).
	ProviderValue Provider
}

// NewFake returns a ready-to-use Fake.
func NewFake() *Fake {
	return &Fake{}
}

func (f *Fake) record(op string, argv []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, Call{Op: op, Argv: argv})
}

// Provider reports the configured provider, defaulting to the host's.
func (f *Fake) Provider() Provider {
	if f.ProviderValue == "" {
		return DetectProvider()
	}
	return f.ProviderValue
}

func (f *Fake) Init(_ context.Context, opts InitOptions) error {
	argv, err := InitArgs(opts)
	if err != nil {
		return err
	}
	f.record("init", argv)
	return nil
}

func (f *Fake) Start(_ context.Context, name string) error {
	argv, err := StartArgs(name)
	if err != nil {
		return err
	}
	f.record("start", argv)
	return nil
}

func (f *Fake) Stop(_ context.Context, name string) error {
	argv, err := StopArgs(name)
	if err != nil {
		return err
	}
	f.record("stop", argv)
	return nil
}

func (f *Fake) Rm(_ context.Context, name string, force bool) error {
	argv, err := RmArgs(name, force)
	if err != nil {
		return err
	}
	f.record("rm", argv)
	return nil
}

func (f *Fake) SSH(_ context.Context, name string, command ...string) ([]byte, error) {
	argv, err := SSHArgs(name, command...)
	if err != nil {
		return nil, err
	}
	f.record("ssh", argv)
	return f.SSHResult, nil
}

func (f *Fake) Cp(_ context.Context, name, localPath, remotePath string) error {
	argv, err := CpArgs(name, localPath, remotePath)
	if err != nil {
		return err
	}
	f.record("cp", argv)
	return nil
}

func (f *Fake) List(_ context.Context) ([]Machine, error) {
	f.record("list", ListArgs())
	if f.ListErr != nil {
		return nil, f.ListErr
	}
	return f.ListResult, nil
}

func (f *Fake) Inspect(_ context.Context, name string) ([]byte, error) {
	argv, err := InspectArgs(name)
	if err != nil {
		return nil, err
	}
	f.record("inspect", argv)
	if f.InspectResult != nil {
		if out, ok := f.InspectResult[name]; ok {
			return out, nil
		}
	}
	return f.InspectDefault, nil
}

// LastCall returns the most recently recorded call, or an error if none.
func (f *Fake) LastCall() (Call, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Calls) == 0 {
		return Call{}, fmt.Errorf("podman: fake has recorded no calls")
	}
	return f.Calls[len(f.Calls)-1], nil
}

// static assertions that both implementations satisfy the interface.
var (
	_ PodmanRunner = (*Fake)(nil)
	_ PodmanRunner = (*Runner)(nil)
)
