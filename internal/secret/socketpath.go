package secret

import (
	"path/filepath"

	"github.com/GSA-TTS/ppp/internal/sandbox"
)

// DefaultSocketPath returns the conventional secret UDS path,
// $PPP_DATA/secret.sock (spec §5.6). CLI wiring (T12) uses it to construct the
// Server; the Server itself takes the path as a parameter so tests can point at
// a temp dir and never touch the real data dir. It is deliberately isolated in
// its own file so the core server (server.go) carries no dependency on
// internal/sandbox.
func DefaultSocketPath() (string, error) {
	dataDir, err := sandbox.ResolveDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "secret.sock"), nil
}
