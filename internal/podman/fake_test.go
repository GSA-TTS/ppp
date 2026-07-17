package podman_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/GSA-TTS/ppp/internal/podman"
)

func TestFake_RecordsArgvPerOperation(t *testing.T) {
	ctx := context.Background()
	f := podman.NewFake()

	if err := f.Init(ctx, podman.InitOptions{Name: "ppp-a", MemoryMiB: 4096, DiskGiB: 50}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := f.Start(ctx, "ppp-a"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := f.Stop(ctx, "ppp-a"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := f.Rm(ctx, "ppp-a", true); err != nil {
		t.Fatalf("Rm: %v", err)
	}

	if len(f.Calls) != 4 {
		t.Fatalf("expected 4 calls, got %d", len(f.Calls))
	}
	if f.Calls[0].Op != "init" {
		t.Errorf("call 0 op = %q, want init", f.Calls[0].Op)
	}
	wantInit := []string{"podman", "machine", "init", "--memory", "4096", "--disk-size", "50", "ppp-a"}
	if !reflect.DeepEqual(f.Calls[0].Argv, wantInit) {
		t.Errorf("init argv = %#v, want %#v", f.Calls[0].Argv, wantInit)
	}
	if !reflect.DeepEqual(f.Calls[3].Argv, []string{"podman", "machine", "rm", "--force", "ppp-a"}) {
		t.Errorf("rm argv = %#v", f.Calls[3].Argv)
	}
}

func TestFake_RejectsDefaultMachineAndRecordsNothing(t *testing.T) {
	ctx := context.Background()
	f := podman.NewFake()
	if err := f.Start(ctx, "podman-machine-default"); !errors.Is(err, podman.ErrDefaultMachine) {
		t.Fatalf("expected ErrDefaultMachine, got %v", err)
	}
	if len(f.Calls) != 0 {
		t.Errorf("expected no recorded calls after refusal, got %d", len(f.Calls))
	}
}

func TestFake_RejectsNonNamespacedName(t *testing.T) {
	ctx := context.Background()
	f := podman.NewFake()
	if err := f.Init(ctx, podman.InitOptions{Name: "myvm"}); !errors.Is(err, podman.ErrInvalidName) {
		t.Fatalf("expected ErrInvalidName, got %v", err)
	}
}

func TestFake_ListReturnsCanned(t *testing.T) {
	ctx := context.Background()
	f := podman.NewFake()
	f.ListResult = []podman.Machine{{Name: "ppp-a", Running: true}}
	got, err := f.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Name != "ppp-a" || !got[0].Running {
		t.Errorf("List = %#v", got)
	}
	last, err := f.LastCall()
	if err != nil {
		t.Fatalf("LastCall: %v", err)
	}
	if !reflect.DeepEqual(last.Argv, []string{"podman", "machine", "list", "--format", "json"}) {
		t.Errorf("list argv = %#v", last.Argv)
	}
}

func TestFake_ListError(t *testing.T) {
	ctx := context.Background()
	f := podman.NewFake()
	sentinel := errors.New("boom")
	f.ListErr = sentinel
	if _, err := f.List(ctx); !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

func TestFake_InspectCannedPerName(t *testing.T) {
	ctx := context.Background()
	f := podman.NewFake()
	f.InspectResult = map[string][]byte{"ppp-a": []byte(`{"Name":"ppp-a"}`)}
	f.InspectDefault = []byte(`{}`)

	out, err := f.Inspect(ctx, "ppp-a")
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if string(out) != `{"Name":"ppp-a"}` {
		t.Errorf("Inspect ppp-a = %s", out)
	}
	def, err := f.Inspect(ctx, "ppp-b")
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if string(def) != `{}` {
		t.Errorf("Inspect ppp-b (default) = %s", def)
	}
}

func TestFake_SSHReturnsCannedOutputAndRecordsArgv(t *testing.T) {
	ctx := context.Background()
	f := podman.NewFake()
	f.SSHResult = []byte("provisioned\n")
	out, err := f.SSH(ctx, "ppp-a", "bash", "/tmp/provision.sh")
	if err != nil {
		t.Fatalf("SSH: %v", err)
	}
	if string(out) != "provisioned\n" {
		t.Errorf("SSH out = %s", out)
	}
	last, _ := f.LastCall()
	want := []string{"podman", "machine", "ssh", "ppp-a", "--", "bash", "/tmp/provision.sh"}
	if !reflect.DeepEqual(last.Argv, want) {
		t.Errorf("ssh argv = %#v", last.Argv)
	}
}

func TestFake_CpRecordsArgv(t *testing.T) {
	ctx := context.Background()
	f := podman.NewFake()
	if err := f.Cp(ctx, "ppp-a", "/host/provision.sh", "/tmp/provision.sh"); err != nil {
		t.Fatalf("Cp: %v", err)
	}
	last, _ := f.LastCall()
	want := []string{"podman", "machine", "cp", "/host/provision.sh", "ppp-a:/tmp/provision.sh"}
	if !reflect.DeepEqual(last.Argv, want) {
		t.Errorf("cp argv = %#v", last.Argv)
	}
}

func TestFake_ProviderDefaultsToHost(t *testing.T) {
	f := podman.NewFake()
	if f.Provider() != podman.DetectProvider() {
		t.Errorf("Fake.Provider = %q, want host default %q", f.Provider(), podman.DetectProvider())
	}
	f.ProviderValue = podman.ProviderQEMU
	if f.Provider() != podman.ProviderQEMU {
		t.Errorf("Fake.Provider = %q, want qemu", f.Provider())
	}
}

func TestLastCall_Empty(t *testing.T) {
	f := podman.NewFake()
	if _, err := f.LastCall(); err == nil {
		t.Fatal("expected error from LastCall with no calls")
	}
}
