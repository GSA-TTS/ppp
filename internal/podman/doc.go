// Package podman wraps the podman CLI for per-sandbox Podman Machine
// lifecycle management and host-provider detection (spec §5.1).
//
// It provides init/start/stop/rm/ssh/cp/list/inspect operations against the
// one dedicated Podman Machine that each sandbox owns (the strict 1:1
// sandbox↔machine isolation invariant, ADR-0001), plus provider selection
// (libkrun on macOS, wsl on Windows, qemu on Linux).
//
// # Single boundary
//
// PodmanRunner is the sole boundary for all podman interaction, so callers are
// decoupled from the podman CLI and can be tested against the exported Fake.
// Two implementations exist: Runner (real shell-out; the process-execution
// body is host-only and lands in T13) and Fake (in-memory, records every
// operation's argv and returns canned List/Inspect results).
//
// # Argv, never shell strings
//
// Every operation is built as an argv slice ([]string) by a pure *Args
// function (InitArgs, StartArgs, StopArgs, RmArgs, SSHArgs, CpArgs, ListArgs,
// InspectArgs). Those functions are the single source of truth for the argv
// contract; both the Fake and the real Runner call them, and they are tested
// directly. The slice is intended to be passed to exec.Command(argv[0],
// argv[1:]...) so no shell string is ever constructed and no shell
// metacharacter is ever interpreted.
//
// # Isolation invariant (ADR-0001)
//
// Machine names must be ppp-namespaced: they MUST match ppp-<segment>(-<segment>)*
// where each segment is one or more lowercase letters or digits (regexp
// ^ppp-[a-z0-9]+(-[a-z0-9]+)*$). Podman's implicit machine
// "podman-machine-default" is refused by name in addition to failing the
// pattern. Name-taking operations return ErrDefaultMachine or ErrInvalidName
// rather than building argv, so ppp never touches a machine it does not own.
//
// # Unit translation (spec §5.1)
//
// InitOptions carries MemoryMiB and DiskGiB as plain integers (MiB and GiB).
// ppp's sbx-style CLI flags accept binary-unit strings (e.g. "8g"); the caller
// converts those to integer MiB/GiB before populating InitOptions. The emitted
// argv therefore always contains bare integers (--memory 8192, --disk-size
// 100), never unit-suffixed strings.
package podman
