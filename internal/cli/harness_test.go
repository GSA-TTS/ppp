package cli

import (
	"bytes"
	"testing"

	"github.com/GSA-TTS/ppp/internal/podman"
	"github.com/GSA-TTS/ppp/internal/secret"
)

// fakeSupervisor is an in-memory Supervisor for daemon-command tests. It records
// Start/Stop calls and returns a canned status without touching a process.
type fakeSupervisor struct {
	started   int
	stopped   int
	status    ProxyStatus
	statusErr error
	startErr  error
	stopErr   error
}

func (f *fakeSupervisor) Start() error { f.started++; return f.startErr }
func (f *fakeSupervisor) Stop() error  { f.stopped++; return f.stopErr }
func (f *fakeSupervisor) Status() (ProxyStatus, error) {
	return f.status, f.statusErr
}

// testEnv points $PPP_DATA and $PPP_CONFIG at fresh temp dirs so a test never
// touches the developer's real ppp state.
func testEnv(t *testing.T) {
	t.Helper()
	t.Setenv("PPP_DATA", t.TempDir())
	t.Setenv("PPP_CONFIG", t.TempDir())
}

// harness bundles the injected fakes so a test can inspect them after a run.
type harness struct {
	runner     *podman.Fake
	store      *fakeStore
	supervisor *fakeSupervisor
}

// newHarness builds a deps set backed by fresh fakes and returns both the deps
// and the harness for assertions.
func newHarness() (deps, *harness) {
	h := &harness{
		runner:     podman.NewFake(),
		store:      newTestStore(),
		supervisor: &fakeSupervisor{},
	}
	d := deps{
		newRunner:     func() podman.PodmanRunner { return h.runner },
		newStore:      func() secret.Store { return h.store },
		newSupervisor: func() Supervisor { return h.supervisor },
	}
	return d, h
}

// run executes `ppp <args...>` against a root built with the harness deps,
// capturing stdout. stdin, when non-nil, is fed to the command.
func run(t *testing.T, d deps, stdin string, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd(d)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if stdin != "" {
		root.SetIn(bytes.NewBufferString(stdin))
	}
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}
