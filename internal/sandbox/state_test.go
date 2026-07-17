package sandbox_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/GSA-TTS/ppp/internal/sandbox"
)

func sampleSandbox() sandbox.Sandbox {
	return sandbox.Sandbox{
		Name:        "ppp-brave-otter",
		Agent:       "opencode",
		Workspace:   "/tmp/workspace",
		Status:      sandbox.StatusCreated,
		CreatedAt:   time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC),
		CPUs:        4,
		Memory:      4096,
		Port:        51820,
		InnerIP:     "10.0.0.1",
		KitRefs:     []string{},
		TemplateTag: "",
		MachineName: "ppp-brave-otter",
	}
}

// TestSaveLoadRoundtrip writes a sandbox record and reads it back, asserting
// the loaded value equals the original and machine_name == name (ADR-0001).
func TestSaveLoadRoundtrip(t *testing.T) {
	data := t.TempDir()
	t.Setenv("PPP_DATA", data)

	original := sampleSandbox()
	if err := original.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := sandbox.Load(original.Name)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !reflect.DeepEqual(loaded, original) {
		t.Errorf("roundtrip mismatch:\n got  %+v\n want %+v", loaded, original)
	}
	if loaded.MachineName != loaded.Name {
		t.Errorf("machine_name %q != name %q (ADR-0001)", loaded.MachineName, loaded.Name)
	}
}

// TestJSONTagsMatchSpec asserts the on-disk field names match the spec §5.8
// schema.
func TestJSONTagsMatchSpec(t *testing.T) {
	data := t.TempDir()
	t.Setenv("PPP_DATA", data)

	s := sampleSandbox()
	if err := s.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	path, err := sandbox.SandboxJSONPath(s.Name)
	if err != nil {
		t.Fatalf("SandboxJSONPath() error = %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading sandbox.json: %v", err)
	}
	want := []string{
		`"name"`, `"agent"`, `"workspace"`, `"status"`, `"created_at"`,
		`"cpus"`, `"memory"`, `"port"`, `"inner_ip"`, `"kit_refs"`,
		`"template_tag"`, `"machine_name"`,
	}
	for _, key := range want {
		if !contains(string(raw), key) {
			t.Errorf("sandbox.json missing JSON key %s\ngot: %s", key, raw)
		}
	}
}

// TestSaveDerivesMachineName ensures Save enforces machine_name == name even
// when the caller leaves it blank (ADR-0001).
func TestSaveDerivesMachineName(t *testing.T) {
	data := t.TempDir()
	t.Setenv("PPP_DATA", data)

	s := sampleSandbox()
	s.MachineName = ""
	if err := s.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	loaded, err := sandbox.Load(s.Name)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.MachineName != s.Name {
		t.Errorf("machine_name = %q, want %q", loaded.MachineName, s.Name)
	}
}

// TestSaveRejectsMismatchedMachineName ensures a conflicting machine_name is
// rejected rather than silently persisted (ADR-0001: VMs never shared).
func TestSaveRejectsMismatchedMachineName(t *testing.T) {
	data := t.TempDir()
	t.Setenv("PPP_DATA", data)

	s := sampleSandbox()
	s.MachineName = "some-other-machine"
	if err := s.Save(); err == nil {
		t.Fatal("Save() with mismatched machine_name: want error, got nil")
	}
}

// TestSaveIsAtomic asserts no temp file is left behind after a successful save.
func TestSaveIsAtomic(t *testing.T) {
	data := t.TempDir()
	t.Setenv("PPP_DATA", data)

	s := sampleSandbox()
	if err := s.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	dir, err := sandbox.SandboxDir(s.Name)
	if err != nil {
		t.Fatalf("SandboxDir() error = %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("temp file %q left behind after Save()", e.Name())
		}
	}
}

func TestLoadMissingReturnsError(t *testing.T) {
	data := t.TempDir()
	t.Setenv("PPP_DATA", data)

	if _, err := sandbox.Load("does-not-exist"); err == nil {
		t.Fatal("Load() of missing sandbox: want error, got nil")
	}
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
