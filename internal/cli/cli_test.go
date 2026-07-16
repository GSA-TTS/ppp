package cli

import (
	"bytes"
	"testing"
)

// wantSubcommands is the v1 top-level subcommand surface (spec §6).
var wantSubcommands = []string{
	"completion", "cp", "create", "daemon", "diagnose", "exec", "kit", "ls",
	"policy", "ports", "reset", "rm", "run", "secret", "setup", "stop",
	"template", "tui", "version",
}

func TestRootHasAllSubcommands(t *testing.T) {
	root := NewRootCmd()
	have := make(map[string]bool)
	for _, c := range root.Commands() {
		have[c.Name()] = true
	}
	for _, name := range wantSubcommands {
		if !have[name] {
			t.Errorf("root command missing subcommand %q", name)
		}
	}
}

func TestVersionCmdPrintsVersion(t *testing.T) {
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("version command failed: %v", err)
	}
	if got := out.String(); got != "ppp dev\n" {
		t.Errorf("version output = %q, want %q", got, "ppp dev\n")
	}
}
