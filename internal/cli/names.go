package cli

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"regexp"
)

// sandboxNamePattern is the ppp sandbox/machine naming rule (ADR-0001): a
// lowercase, "ppp-"-prefixed, hyphen-separated alphanumeric name. It matches
// the podman package's machine-name rule so a name that passes here is always
// a legal machine name too (the sandbox owns a same-named machine 1:1).
var sandboxNamePattern = regexp.MustCompile(`^ppp-[a-z0-9]+(-[a-z0-9]+)*$`)

// validateSandboxName rejects any name that is not ppp-namespaced, treating the
// name as untrusted CLI input. This is the single gate every command applies
// before touching per-sandbox state or the PodmanRunner.
func validateSandboxName(name string) error {
	if !sandboxNamePattern.MatchString(name) {
		return fmt.Errorf("invalid sandbox name %q (must match ppp-<lowercase-alphanumeric-hyphen>)", name)
	}
	return nil
}

// nameAdjectives and nameNouns seed the ppp-<adjective>-<noun> auto-name
// generator (spec §6.1 step 3a). Kept short and neutral; every combination
// yields a valid sandbox name.
var (
	nameAdjectives = []string{
		"amber", "brave", "calm", "clever", "eager", "gentle", "happy",
		"jolly", "keen", "lively", "mellow", "nimble", "proud", "quiet",
		"red", "swift", "teal", "vivid", "witty", "zesty",
	}
	nameNouns = []string{
		"badger", "bird", "cedar", "comet", "delta", "ember", "falcon",
		"garden", "harbor", "island", "jaguar", "kite", "lynx", "meadow",
		"nebula", "otter", "pine", "quartz", "river", "sparrow",
	}
)

// generateName produces a random ppp-<adjective>-<noun> name using
// crypto/rand. The result always satisfies validateSandboxName.
func generateName() (string, error) {
	adj, err := pick(nameAdjectives)
	if err != nil {
		return "", err
	}
	noun, err := pick(nameNouns)
	if err != nil {
		return "", err
	}
	return "ppp-" + adj + "-" + noun, nil
}

// pick returns a cryptographically-random element of words.
func pick(words []string) (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(words))))
	if err != nil {
		return "", fmt.Errorf("generating random name: %w", err)
	}
	return words[n.Int64()], nil
}
