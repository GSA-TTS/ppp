package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Sandbox is the persisted per-sandbox record stored at
// <PPP_DATA>/sandboxes/<name>/sandbox.json (spec §5.8).
//
// MachineName maps 1:1 to Name — a sandbox owns exactly one dedicated Podman
// Machine and machines are never shared (ADR-0001). Save enforces this.
type Sandbox struct {
	Name        string    `json:"name"`
	Agent       string    `json:"agent"`
	Workspace   string    `json:"workspace"`
	Status      Status    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	CPUs        uint      `json:"cpus"`
	Memory      uint      `json:"memory"` // MiB
	Port        int       `json:"port"`   // WireGuard listen port; sandbox identity (ADR-0003)
	InnerIP     string    `json:"inner_ip"`
	KitRefs     []string  `json:"kit_refs"`     // empty in v1
	TemplateTag string    `json:"template_tag"` // empty in v1
	MachineName string    `json:"machine_name"` // == Name (ADR-0001)
}

// Save writes the sandbox record atomically to its sandbox.json path.
//
// MachineName is derived from Name when empty and MUST otherwise equal Name
// (ADR-0001); a mismatch is rejected. The write is atomic: the JSON is written
// to a temp file in the destination directory and renamed into place, so a
// crash mid-write never leaves a partial sandbox.json.
func (s *Sandbox) Save() error {
	if s.MachineName == "" {
		s.MachineName = s.Name
	}
	if s.MachineName != s.Name {
		return fmt.Errorf("machine_name %q must equal name %q (ADR-0001: one machine per sandbox, never shared)", s.MachineName, s.Name)
	}

	dir, err := SandboxDir(s.Name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating sandbox dir %q: %w", dir, err)
	}

	encoded, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling sandbox %q: %w", s.Name, err)
	}

	return writeFileAtomic(filepath.Join(dir, "sandbox.json"), encoded, 0o600)
}

// Load reads and decodes the sandbox.json record for the named sandbox.
func Load(name string) (Sandbox, error) {
	path, err := SandboxJSONPath(name)
	if err != nil {
		return Sandbox{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Sandbox{}, fmt.Errorf("reading sandbox %q: %w", name, err)
	}
	var s Sandbox
	if err := json.Unmarshal(raw, &s); err != nil {
		return Sandbox{}, fmt.Errorf("decoding sandbox %q: %w", name, err)
	}
	return s, nil
}

// writeFileAtomic writes data to path via a temp file in the same directory
// followed by a rename, so readers never observe a partially written file.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file in %q: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after a successful rename

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing temp file %q: %w", tmpName, err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp file %q: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file %q: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("renaming %q to %q: %w", tmpName, path, err)
	}
	return nil
}
