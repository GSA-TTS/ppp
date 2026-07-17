package podman_test

import (
	"context"
	"errors"
	"testing"

	"github.com/GSA-TTS/ppp/internal/podman"
)

func TestRunner_ValidatesBeforeNotImplemented(t *testing.T) {
	ctx := context.Background()
	r := podman.NewRunner()

	// Invalid names must fail with the validation error, NOT ErrNotImplemented:
	// no un-vetted argv should ever reach exec.
	if err := r.Start(ctx, "podman-machine-default"); !errors.Is(err, podman.ErrDefaultMachine) {
		t.Errorf("Start(default): expected ErrDefaultMachine, got %v", err)
	}
	if err := r.Init(ctx, podman.InitOptions{Name: "myvm"}); !errors.Is(err, podman.ErrInvalidName) {
		t.Errorf("Init(non-namespaced): expected ErrInvalidName, got %v", err)
	}
}

func TestRunner_ValidArgsReturnNotImplemented(t *testing.T) {
	ctx := context.Background()
	r := podman.NewRunner()

	checks := map[string]error{
		"Init":  r.Init(ctx, podman.InitOptions{Name: "ppp-a"}),
		"Start": r.Start(ctx, "ppp-a"),
		"Stop":  r.Stop(ctx, "ppp-a"),
		"Rm":    r.Rm(ctx, "ppp-a", true),
		"Cp":    r.Cp(ctx, "ppp-a", "/a", "/b"),
	}
	for name, err := range checks {
		if !errors.Is(err, podman.ErrNotImplemented) {
			t.Errorf("%s: expected ErrNotImplemented, got %v", name, err)
		}
	}
	if _, err := r.SSH(ctx, "ppp-a", "ls"); !errors.Is(err, podman.ErrNotImplemented) {
		t.Errorf("SSH: expected ErrNotImplemented, got %v", err)
	}
	if _, err := r.List(ctx); !errors.Is(err, podman.ErrNotImplemented) {
		t.Errorf("List: expected ErrNotImplemented, got %v", err)
	}
	if _, err := r.Inspect(ctx, "ppp-a"); !errors.Is(err, podman.ErrNotImplemented) {
		t.Errorf("Inspect: expected ErrNotImplemented, got %v", err)
	}
}

func TestRunner_ProviderNonEmpty(t *testing.T) {
	r := podman.NewRunner()
	if r.Provider() == "" {
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
