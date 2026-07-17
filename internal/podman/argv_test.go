package podman_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/GSA-TTS/ppp/internal/podman"
)

func TestInitArgs_FullOptions_UnitTranslation(t *testing.T) {
	// Unit translation: MemoryMiB and DiskGiB are integers; argv must carry
	// bare integers (spec §5.1), never unit-suffixed strings like "8g".
	got, err := podman.InitArgs(podman.InitOptions{
		Name:           "ppp-brave-otter",
		CPUs:           4,
		MemoryMiB:      8192,
		DiskGiB:        100,
		ImportNativeCA: true,
	})
	if err != nil {
		t.Fatalf("InitArgs: unexpected error: %v", err)
	}
	want := []string{
		"podman", "machine", "init",
		"--cpus", "4",
		"--memory", "8192",
		"--disk-size", "100",
		"--import-native-ca",
		"ppp-brave-otter",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("InitArgs argv mismatch:\n got  %#v\n want %#v", got, want)
	}
}

func TestInitArgs_MemoryIsIntegerMiB(t *testing.T) {
	got, err := podman.InitArgs(podman.InitOptions{Name: "ppp-x", MemoryMiB: 2048})
	if err != nil {
		t.Fatalf("InitArgs: %v", err)
	}
	for _, a := range got {
		if a == "2g" || a == "8g" || a == "2048MiB" {
			t.Fatalf("argv contains unit-suffixed memory %q: %#v", a, got)
		}
	}
	// The literal integer must be present.
	if !containsPair(got, "--memory", "2048") {
		t.Errorf("expected `--memory 2048` in %#v", got)
	}
}

func TestInitArgs_ZeroValuesOmitFlags(t *testing.T) {
	got, err := podman.InitArgs(podman.InitOptions{Name: "ppp-min"})
	if err != nil {
		t.Fatalf("InitArgs: %v", err)
	}
	want := []string{"podman", "machine", "init", "ppp-min"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("minimal InitArgs mismatch:\n got  %#v\n want %#v", got, want)
	}
}

func TestInitArgs_ProviderAndNow(t *testing.T) {
	got, err := podman.InitArgs(podman.InitOptions{
		Name:     "ppp-p",
		Provider: string(podman.ProviderQEMU),
		Now:      true,
	})
	if err != nil {
		t.Fatalf("InitArgs: %v", err)
	}
	want := []string{
		"podman", "machine", "init",
		"--provider", "qemu",
		"--now",
		"ppp-p",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("InitArgs provider mismatch:\n got  %#v\n want %#v", got, want)
	}
}

func TestInitArgs_UnknownProviderRejected(t *testing.T) {
	_, err := podman.InitArgs(podman.InitOptions{Name: "ppp-p", Provider: "virtualbox"})
	if err == nil {
		t.Fatal("expected error for unknown provider, got nil")
	}
}

func TestStartStopArgs(t *testing.T) {
	start, err := podman.StartArgs("ppp-a")
	if err != nil {
		t.Fatalf("StartArgs: %v", err)
	}
	if !reflect.DeepEqual(start, []string{"podman", "machine", "start", "ppp-a"}) {
		t.Errorf("StartArgs = %#v", start)
	}
	stop, err := podman.StopArgs("ppp-a")
	if err != nil {
		t.Fatalf("StopArgs: %v", err)
	}
	if !reflect.DeepEqual(stop, []string{"podman", "machine", "stop", "ppp-a"}) {
		t.Errorf("StopArgs = %#v", stop)
	}
}

func TestRmArgs(t *testing.T) {
	plain, err := podman.RmArgs("ppp-a", false)
	if err != nil {
		t.Fatalf("RmArgs: %v", err)
	}
	if !reflect.DeepEqual(plain, []string{"podman", "machine", "rm", "ppp-a"}) {
		t.Errorf("RmArgs(force=false) = %#v", plain)
	}
	forced, err := podman.RmArgs("ppp-a", true)
	if err != nil {
		t.Fatalf("RmArgs: %v", err)
	}
	if !reflect.DeepEqual(forced, []string{"podman", "machine", "rm", "--force", "ppp-a"}) {
		t.Errorf("RmArgs(force=true) = %#v", forced)
	}
}

func TestSSHArgs_CommandAfterSeparator(t *testing.T) {
	got, err := podman.SSHArgs("ppp-a", "bash", "/tmp/provision.sh")
	if err != nil {
		t.Fatalf("SSHArgs: %v", err)
	}
	want := []string{"podman", "machine", "ssh", "ppp-a", "--", "bash", "/tmp/provision.sh"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SSHArgs mismatch:\n got  %#v\n want %#v", got, want)
	}
}

func TestSSHArgs_NoCommand(t *testing.T) {
	got, err := podman.SSHArgs("ppp-a")
	if err != nil {
		t.Fatalf("SSHArgs: %v", err)
	}
	want := []string{"podman", "machine", "ssh", "ppp-a"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SSHArgs(no cmd) mismatch:\n got  %#v\n want %#v", got, want)
	}
}

func TestSSHArgs_EmptyCommandElementRejected(t *testing.T) {
	if _, err := podman.SSHArgs("ppp-a", "bash", ""); err == nil {
		t.Fatal("expected error for empty ssh command element")
	}
}

func TestCpArgs(t *testing.T) {
	got, err := podman.CpArgs("ppp-a", "/host/provision.sh", "/tmp/provision.sh")
	if err != nil {
		t.Fatalf("CpArgs: %v", err)
	}
	want := []string{"podman", "machine", "cp", "/host/provision.sh", "ppp-a:/tmp/provision.sh"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CpArgs mismatch:\n got  %#v\n want %#v", got, want)
	}
}

func TestCpArgs_EmptyPathRejected(t *testing.T) {
	if _, err := podman.CpArgs("ppp-a", "", "/tmp/x"); err == nil {
		t.Fatal("expected error for empty local path")
	}
	if _, err := podman.CpArgs("ppp-a", "/host/x", ""); err == nil {
		t.Fatal("expected error for empty remote path")
	}
}

func TestListArgs(t *testing.T) {
	got := podman.ListArgs()
	want := []string{"podman", "machine", "list", "--format", "json"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListArgs mismatch:\n got  %#v\n want %#v", got, want)
	}
}

func TestInspectArgs(t *testing.T) {
	got, err := podman.InspectArgs("ppp-a")
	if err != nil {
		t.Fatalf("InspectArgs: %v", err)
	}
	want := []string{"podman", "machine", "inspect", "ppp-a"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("InspectArgs mismatch:\n got  %#v\n want %#v", got, want)
	}
}

func TestArgv_RejectDefaultMachine(t *testing.T) {
	cases := map[string]func() error{
		"Init":    func() error { _, e := podman.InitArgs(podman.InitOptions{Name: "podman-machine-default"}); return e },
		"Start":   func() error { _, e := podman.StartArgs("podman-machine-default"); return e },
		"Stop":    func() error { _, e := podman.StopArgs("podman-machine-default"); return e },
		"Rm":      func() error { _, e := podman.RmArgs("podman-machine-default", true); return e },
		"SSH":     func() error { _, e := podman.SSHArgs("podman-machine-default", "ls"); return e },
		"Cp":      func() error { _, e := podman.CpArgs("podman-machine-default", "/a", "/b"); return e },
		"Inspect": func() error { _, e := podman.InspectArgs("podman-machine-default"); return e },
	}
	for name, fn := range cases {
		if err := fn(); !errors.Is(err, podman.ErrDefaultMachine) {
			t.Errorf("%s: expected ErrDefaultMachine, got %v", name, err)
		}
	}
}

func TestArgv_RejectNonNamespacedNames(t *testing.T) {
	bad := []string{
		"",          // empty
		"myvm",      // no ppp- prefix
		"PPP-upper", // uppercase
		"ppp_underscore",
		"ppp-",          // prefix only
		"ppp--double",   // doubled hyphen
		"ppp-trailing-", // trailing hyphen
		"notppp-x",      // prefix not at start
	}
	for _, name := range bad {
		if _, err := podman.StartArgs(name); !errors.Is(err, podman.ErrInvalidName) {
			t.Errorf("name %q: expected ErrInvalidName, got %v", name, err)
		}
	}
}

func TestArgv_AcceptNamespacedNames(t *testing.T) {
	good := []string{"ppp-a", "ppp-brave-otter", "ppp-123", "ppp-a1-b2-c3"}
	for _, name := range good {
		if _, err := podman.StartArgs(name); err != nil {
			t.Errorf("name %q: expected accepted, got %v", name, err)
		}
	}
}

// helper: does argv contain the sequential pair flag,value?
func containsPair(argv []string, flag, value string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == flag && argv[i+1] == value {
			return true
		}
	}
	return false
}
