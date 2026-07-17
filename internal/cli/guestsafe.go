package cli

import (
	"fmt"
	"path/filepath"
	"strings"
)

// shellUnsafe lists characters that could let a token break out of the guest
// login shell's word-splitting/expansion when `podman machine ssh` re-parses
// the forwarded command. We fail closed on any of them. Note this deliberately
// does NOT include ':' '/' '=' '.' '-' '_' or alphanumerics, which are needed
// by legitimate absolute paths, `-v host:container` mounts, `KEY=VALUE` env
// assignments, and OCI image references.
const shellUnsafe = " \t\n\r;&|<>$`\\\"'(){}#*?![]~"

// validateWorkspacePath enforces that a sandbox workspace is a clean absolute
// path with no shell-metacharacter or whitespace content. This is the ingress
// gate that makes the guest-side command construction safe: `podman machine
// ssh <name> -- <argv...>` forwards the argv to the guest's sshd, which joins
// and re-parses it through the guest login shell, so a raw path like
// "/tmp/x:/ws; rm -rf /" would otherwise inject commands in the guest. We reject
// such input here rather than trying to quote it downstream.
func validateWorkspacePath(path string) error {
	if path == "" {
		return fmt.Errorf("a workspace path is required")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("workspace path %q must be absolute", path)
	}
	if strings.ContainsAny(path, shellUnsafe) {
		return fmt.Errorf("workspace path %q contains disallowed characters (whitespace or shell metacharacters)", path)
	}
	return nil
}

// guestArg validates a single token destined to be forwarded to the guest shell
// via `podman machine ssh`. It rejects whitespace and shell metacharacters so
// the re-parse on the guest side cannot split or expand the token. Used for
// values ppp itself controls (image refs, env assignments) as defense in depth.
func guestArg(kind, val string) error {
	if val == "" {
		return fmt.Errorf("%s must not be empty", kind)
	}
	if strings.ContainsAny(val, shellUnsafe) {
		return fmt.Errorf("%s %q contains disallowed characters", kind, val)
	}
	return nil
}
