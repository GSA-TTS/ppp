package podman

import (
	"context"
	"reflect"
	"testing"
)

func TestDecodeMachines(t *testing.T) {
	raw := []byte(`[{"Name":"ppp-a","Running":true,"VMType":"libkrun"},{"Name":"ppp-b","Running":false}]`)
	got, err := decodeMachines(raw)
	if err != nil {
		t.Fatalf("decodeMachines: %v", err)
	}
	want := []Machine{
		{Name: "ppp-a", Running: true, VMType: "libkrun"},
		{Name: "ppp-b", Running: false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("decodeMachines = %#v, want %#v", got, want)
	}
}

func TestDecodeMachines_Invalid(t *testing.T) {
	if _, err := decodeMachines([]byte("not json")); err == nil {
		t.Fatal("expected error decoding invalid JSON")
	}
}

func TestCommand_BuildsExecCmd(t *testing.T) {
	// command is the single argv->process point (exec.Command(argv[0],
	// argv[1:]...)); verify it wires argv positionally and rejects empty argv.
	cmd, err := command(context.Background(), []string{"podman", "machine", "list"})
	if err != nil {
		t.Fatalf("command: %v", err)
	}
	if len(cmd.Args) != 3 || cmd.Args[0] != "podman" || cmd.Args[2] != "list" {
		t.Errorf("cmd.Args = %#v", cmd.Args)
	}
	if _, err := command(context.Background(), nil); err == nil {
		t.Fatal("expected error for empty argv")
	}
}
