package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
)

// XDG-relative fallback segments used when neither the ppp-specific override
// nor the XDG base-directory variable is set (spec §5.8).
var (
	configFallback = filepath.Join(".config", "ppp")
	dataFallback   = filepath.Join(".local", "share", "ppp")
	cacheFallback  = filepath.Join(".cache", "ppp")
)

// resolveRoot resolves an XDG-style root directory with ppp overrides.
//
// Precedence (spec §5.8): the ppp-specific override (e.g. $PPP_DATA) wins; then
// the XDG base variable with "ppp" appended (e.g. $XDG_DATA_HOME/ppp); finally
// $HOME joined with the fallback segment (e.g. ~/.local/share/ppp).
func resolveRoot(pppEnv, xdgEnv, fallback string) (string, error) {
	if v := os.Getenv(pppEnv); v != "" {
		return v, nil
	}
	if v := os.Getenv(xdgEnv); v != "" {
		return filepath.Join(v, "ppp"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory for %s fallback: %w", pppEnv, err)
	}
	return filepath.Join(home, fallback), nil
}

// ResolveConfigDir returns the ppp config root ($PPP_CONFIG,
// $XDG_CONFIG_HOME/ppp, or ~/.config/ppp).
func ResolveConfigDir() (string, error) {
	return resolveRoot("PPP_CONFIG", "XDG_CONFIG_HOME", configFallback)
}

// ResolveDataDir returns the ppp data root ($PPP_DATA, $XDG_DATA_HOME/ppp, or
// ~/.local/share/ppp). This is the base for per-sandbox state and state.lock.
func ResolveDataDir() (string, error) {
	return resolveRoot("PPP_DATA", "XDG_DATA_HOME", dataFallback)
}

// ResolveCacheDir returns the ppp cache root ($PPP_CACHE, $XDG_CACHE_HOME/ppp,
// or ~/.cache/ppp).
func ResolveCacheDir() (string, error) {
	return resolveRoot("PPP_CACHE", "XDG_CACHE_HOME", cacheFallback)
}

// SandboxDir returns the per-sandbox state directory
// (<PPP_DATA>/sandboxes/<name>/).
func SandboxDir(name string) (string, error) {
	data, err := ResolveDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(data, "sandboxes", name), nil
}

// SandboxJSONPath returns the path to a sandbox's sandbox.json record.
func SandboxJSONPath(name string) (string, error) {
	dir, err := SandboxDir(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sandbox.json"), nil
}

// StateLockPath returns the path to the state.lock flock file
// (<PPP_DATA>/state.lock) guarding concurrent CLI operations.
func StateLockPath() (string, error) {
	data, err := ResolveDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(data, "state.lock"), nil
}
