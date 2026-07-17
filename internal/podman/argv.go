package podman

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// defaultMachineName is Podman's implicit machine. ADR-0001 forbids ppp from
// ever operating on it: every machine ppp manages is ppp-named and ppp-owned.
const defaultMachineName = "podman-machine-default"

// namePattern is the ppp machine-name namespacing rule (ADR-0001, spec §5.1):
// names MUST be lowercase, start with the literal "ppp-" prefix, and contain
// only lowercase letters, digits, and hyphens, with no leading/trailing or
// doubled hyphen after the prefix. This keeps every ppp-managed machine
// unambiguously ppp-owned so ppp never touches a user's own machines, and it
// stays within Podman's safe machine-name character set.
var namePattern = regexp.MustCompile(`^ppp-[a-z0-9]+(-[a-z0-9]+)*$`)

// ErrDefaultMachine is returned when an operation targets Podman's implicit
// default machine, which ppp must never touch (ADR-0001).
var ErrDefaultMachine = errors.New("podman: refusing to operate on podman-machine-default (ADR-0001)")

// ErrInvalidName is returned when a machine name is not ppp-namespaced.
var ErrInvalidName = errors.New("podman: machine name is not ppp-namespaced (must match ppp-<lowercase-alphanumeric-hyphen>)")

// validateName enforces the ppp namespacing rule and the explicit
// default-machine guard (defense in depth: the default name also fails the
// pattern, but we reject it by name regardless).
func validateName(name string) error {
	if name == defaultMachineName {
		return ErrDefaultMachine
	}
	if !namePattern.MatchString(name) {
		return fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	return nil
}

// validateArg rejects arguments that could break out of the argv contract.
// argv elements are passed to exec.Command as separate arguments (never a
// shell string), but a NUL byte cannot appear in an exec argument and an empty
// path/command is always a caller bug, so we fail closed on both.
func validateArg(kind, arg string) error {
	if arg == "" {
		return fmt.Errorf("podman: empty %s argument", kind)
	}
	if strings.ContainsRune(arg, 0) {
		return fmt.Errorf("podman: %s argument contains NUL byte", kind)
	}
	return nil
}

// InitOptions configures a `podman machine init` invocation.
//
// Unit contract: MemoryMiB and DiskGiB are plain integers in MiB and GiB
// respectively — NOT binary-unit strings. ppp's sbx-style CLI flags accept
// binary-unit strings (e.g. "8g"); the CALLER converts those to integer
// MiB/GiB before populating this struct (spec §5.1 unit translation). The
// argv this package builds therefore always contains bare integers
// (e.g. `--memory 8192`, `--disk-size 100`), never `8g`.
type InitOptions struct {
	// Name is the ppp-namespaced machine name (required, validated).
	Name string
	// CPUs is the vCPU count; 0 means "omit the flag" (podman auto/default).
	CPUs uint
	// MemoryMiB is memory in MiB; 0 means "omit the flag" (podman default 2048).
	MemoryMiB uint
	// DiskGiB is disk size in GiB; 0 means "omit the flag" (podman default 100).
	DiskGiB uint
	// ImportNativeCA adds --import-native-ca (spec §5.1, verified podman 6.0.1).
	ImportNativeCA bool
	// Provider optionally overrides provider autodetection (--provider);
	// empty means autodetect (libkrun/wsl/qemu). Spec §5.1.
	Provider string
	// Now folds the start step in via --now (spec §6.1 step 3e).
	Now bool
}

// InitArgs builds the argv for `podman machine init`.
func InitArgs(opts InitOptions) ([]string, error) {
	if err := validateName(opts.Name); err != nil {
		return nil, err
	}
	if opts.Provider != "" {
		if err := validateProvider(opts.Provider); err != nil {
			return nil, err
		}
	}
	argv := []string{"podman", "machine", "init"}
	if opts.Provider != "" {
		argv = append(argv, "--provider", opts.Provider)
	}
	if opts.CPUs != 0 {
		argv = append(argv, "--cpus", strconv.FormatUint(uint64(opts.CPUs), 10))
	}
	if opts.MemoryMiB != 0 {
		argv = append(argv, "--memory", strconv.FormatUint(uint64(opts.MemoryMiB), 10))
	}
	if opts.DiskGiB != 0 {
		argv = append(argv, "--disk-size", strconv.FormatUint(uint64(opts.DiskGiB), 10))
	}
	if opts.ImportNativeCA {
		argv = append(argv, "--import-native-ca")
	}
	if opts.Now {
		argv = append(argv, "--now")
	}
	argv = append(argv, opts.Name)
	return argv, nil
}

// StartArgs builds the argv for `podman machine start`.
func StartArgs(name string) ([]string, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	return []string{"podman", "machine", "start", name}, nil
}

// StopArgs builds the argv for `podman machine stop`.
func StopArgs(name string) ([]string, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	return []string{"podman", "machine", "stop", name}, nil
}

// RmArgs builds the argv for `podman machine rm`. --force skips the
// interactive confirmation prompt so ppp can drive it non-interactively.
func RmArgs(name string, force bool) ([]string, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	argv := []string{"podman", "machine", "rm"}
	if force {
		argv = append(argv, "--force")
	}
	return append(argv, name), nil
}

// SSHArgs builds the argv for `podman machine ssh <name> -- <command...>`.
// The command is passed as separate argv elements after the `--` separator;
// no shell string is ever constructed (spec §5.1/§5.2).
func SSHArgs(name string, command ...string) ([]string, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	argv := []string{"podman", "machine", "ssh", name}
	if len(command) > 0 {
		argv = append(argv, "--")
		for _, c := range command {
			if err := validateArg("ssh command", c); err != nil {
				return nil, err
			}
			argv = append(argv, c)
		}
	}
	return argv, nil
}

// CpArgs builds the argv for `podman machine cp <localPath> <name>:<remotePath>`
// (spec §5.2, verified podman 6.0.1). Both paths are validated and passed as
// distinct argv elements.
func CpArgs(name, localPath, remotePath string) ([]string, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	if err := validateArg("local path", localPath); err != nil {
		return nil, err
	}
	if err := validateArg("remote path", remotePath); err != nil {
		return nil, err
	}
	return []string{"podman", "machine", "cp", localPath, name + ":" + remotePath}, nil
}

// ListArgs builds the argv for `podman machine list --format json` (spec §5.1).
func ListArgs() []string {
	return []string{"podman", "machine", "list", "--format", "json"}
}

// InspectArgs builds the argv for `podman machine inspect <name>`.
func InspectArgs(name string) ([]string, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	return []string{"podman", "machine", "inspect", name}, nil
}
