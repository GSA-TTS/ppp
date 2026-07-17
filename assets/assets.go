// Package assets embeds the runtime files ppp ships inside its binary and
// materializes them on disk when needed: the mitmproxy addon, the guest
// provisioning script, and the opencode agent Containerfile (spec §7).
//
// Embedding keeps ppp a single self-contained binary — no external asset paths
// to resolve at runtime. Callers write these out to $PPP_DATA (or copy them
// into a guest) via the Write* helpers.
//
// This package lives in the top-level assets/ directory (spec §7) because
// //go:embed cannot reference files outside its own package directory.
package assets

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed provision.sh
var provisionScript []byte

//go:embed addon.py
var addonPy []byte

//go:embed opencode.Containerfile
var opencodeContainerfile []byte

// ProvisionScript returns the embedded guest provisioning script.
func ProvisionScript() []byte { return provisionScript }

// AddonPy returns the embedded mitmproxy addon source.
func AddonPy() []byte { return addonPy }

// OpencodeContainerfile returns the embedded opencode agent Containerfile.
func OpencodeContainerfile() []byte { return opencodeContainerfile }

// WriteAddon writes the addon to dir/addon.py (0644) and returns its path.
func WriteAddon(dir string) (string, error) {
	return writeAsset(dir, "addon.py", addonPy, 0o644)
}

// WriteProvisionScript writes the provisioning script to dir/provision.sh
// (0755) and returns its path.
func WriteProvisionScript(dir string) (string, error) {
	return writeAsset(dir, "provision.sh", provisionScript, 0o755)
}

func writeAsset(dir, name string, data []byte, mode os.FileMode) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("assets: empty output dir for %s", name)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("assets: creating %s: %w", dir, err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, mode); err != nil {
		return "", fmt.Errorf("assets: writing %s: %w", path, err)
	}
	return path, nil
}
