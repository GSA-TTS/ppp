package podman_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/GSA-TTS/ppp/internal/podman"
)

// shQuote single-quotes a string for safe embedding in the POSIX stub script.
func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func itoa(i int) string { return strconv.Itoa(i) }

func TestRunner_ValidatesBeforeExec(t *testing.T) {
	ctx := context.Background()
	r := podman.NewRunner()

	// Invalid names must fail with the validation error BEFORE any exec: no
	// un-vetted argv should ever reach the process boundary (ADR-0001).
	if err := r.Start(ctx, "podman-machine-default"); !errors.Is(err, podman.ErrDefaultMachine) {
		t.Errorf("Start(default): expected ErrDefaultMachine, got %v", err)
	}
	if err := r.Init(ctx, podman.InitOptions{Name: "myvm"}); !errors.Is(err, podman.ErrInvalidName) {
		t.Errorf("Init(non-namespaced): expected ErrInvalidName, got %v", err)
	}
	if err := r.Cp(ctx, "ppp-a", "", "/b"); err == nil {
		t.Error("Cp(empty local path): expected validation error, got nil")
	}
}

// stubPodman writes a fake `podman` executable onto PATH so the Runner's
// real exec path can be exercised as a unit test — no real podman, no VM.
// The script echoes the given stdout, writes stderrMsg to stderr, and exits
// with code. It records the argv it was called with to argvFile.
func stubPodman(t *testing.T, stdout, stderrMsg string, code int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub uses a POSIX shell script")
	}
	dir := t.TempDir()
	argvFile := filepath.Join(dir, "argv")
	script := "#!/bin/sh\n" +
		"printf '%s' \"$*\" > " + shQuote(argvFile) + "\n" +
		"printf '%s' " + shQuote(stdout) + "\n" +
		"printf '%s' " + shQuote(stderrMsg) + " >&2\n" +
		"exit " + itoa(code) + "\n"
	path := filepath.Join(dir, "podman")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argvFile
}

func TestRunner_ListDecodesStubbedJSON(t *testing.T) {
	stubPodman(t, `[{"Name":"ppp-a","Running":true,"VMType":"libkrun"}]`, "", 0)
	ms, err := podman.NewRunner().List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(ms) != 1 || ms[0].Name != "ppp-a" || !ms[0].Running || ms[0].VMType != "libkrun" {
		t.Fatalf("List decoded unexpected machines: %+v", ms)
	}
}

func TestRunner_ListEmptyOutputIsNoMachines(t *testing.T) {
	stubPodman(t, "", "", 0)
	ms, err := podman.NewRunner().List(context.Background())
	if err != nil {
		t.Fatalf("List(empty): %v", err)
	}
	if len(ms) != 0 {
		t.Fatalf("List(empty): expected no machines, got %+v", ms)
	}
}

func TestRunner_NonZeroExitWrapsStderr(t *testing.T) {
	stubPodman(t, "", "boom: machine does not exist", 1)
	err := podman.NewRunner().Start(context.Background(), "ppp-a")
	if err == nil {
		t.Fatal("Start: expected error from non-zero exit, got nil")
	}
	var ce *podman.CommandError
	if !errors.As(err, &ce) {
		t.Fatalf("Start: expected *CommandError, got %T: %v", err, err)
	}
	if ce.Stderr == "" || ce.Argv[0] != "podman" {
		t.Errorf("CommandError missing detail: %+v", ce)
	}
}

func TestRunner_SSHReturnsStdout(t *testing.T) {
	stubPodman(t, "hello from guest\n", "", 0)
	out, err := podman.NewRunner().SSH(context.Background(), "ppp-a", "echo", "hi")
	if err != nil {
		t.Fatalf("SSH: %v", err)
	}
	if string(out) != "hello from guest\n" {
		t.Errorf("SSH stdout = %q", out)
	}
}

func TestRunner_ProviderNonEmpty(t *testing.T) {
	if podman.NewRunner().Provider() == "" {
		t.Error("Runner.Provider returned empty")
	}
}

func TestDetectProvider_KnownValue(t *testing.T) {
	switch p := podman.DetectProvider(); p {
	case podman.ProviderLibkrun, podman.ProviderWSL, podman.ProviderQEMU:
		// ok
	default:
		t.Errorf("DetectProvider returned unknown provider %q", p)
	}
}
