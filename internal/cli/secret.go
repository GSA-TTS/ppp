package cli

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/GSA-TTS/ppp/internal/secret"
)

// newSecretCmd manages keychain-backed secrets (spec §6.18).
//
// Secret VALUES are never taken from a positional CLI argument (they would land
// in shell history and the process table); `set` reads the value from
// --from-env VAR or, absent that, from stdin. Values are never printed.
func newSecretCmd(d deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Manage keychain-backed secrets",
	}
	cmd.AddCommand(
		newSecretSetCmd(d),
		newSecretSetCustomCmd(d),
		newSecretImportCmd(d),
		newSecretLsCmd(d),
		newSecretRmCmd(d),
	)
	return cmd
}

// mutableStore is the write side of a secret store. The Store interface (T7) is
// read-only (Get); writes/deletes/listing are wired here through this optional
// extension so command tests inject a fake that records mutations. The real
// KeyringStore write path (which prompts for keychain approval) lands with the
// host-only work; T12 fully wires the state/behavior through this seam.
type mutableStore interface {
	secret.Store
	Set(key, value string) error
	Delete(key string) error
	Keys() ([]string, error)
}

// requireMutable adapts the injected Store to a mutableStore, erroring clearly
// when the store does not support writes. The primary KeyringStore is mutable;
// the age fallback store is read-only at runtime (it is populated out of band),
// so `set`/`rm` against an age-only host report this rather than silently
// no-op'ing.
func requireMutable(s secret.Store) (mutableStore, error) {
	if m, ok := s.(mutableStore); ok {
		return m, nil
	}
	return nil, fmt.Errorf("this secret store is read-only on this host; use the OS keychain, or populate the age store out of band")
}

// newSecretSetCmd stores a service secret. Value comes from --from-env or stdin.
func newSecretSetCmd(d deps) *cobra.Command {
	var (
		sandboxName string
		fromEnv     string
	)
	cmd := &cobra.Command{
		Use:   "set SERVICE",
		Short: "Store a service secret (value from --from-env VAR or stdin; never a CLI arg)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := normalizeServiceName(args[0])
			if err != nil {
				return err
			}
			if sandboxName != "" {
				if err := validateSandboxName(sandboxName); err != nil {
					return err
				}
			}
			value, err := readSecretValue(cmd, fromEnv)
			if err != nil {
				return err
			}
			store, err := requireMutable(d.newStore())
			if err != nil {
				return err
			}
			key := secretKey(svc, sandboxName)
			if err := store.Set(key, value); err != nil {
				return err
			}
			return outf(cmd.OutOrStdout(), "stored secret %s\n", key)
		},
	}
	cmd.Flags().StringVar(&sandboxName, "sandbox", "", "scope the secret to a sandbox (per-sandbox precedence)")
	cmd.Flags().StringVar(&fromEnv, "from-env", "", "read the secret value from this environment variable")
	return cmd
}

// newSecretSetCustomCmd stores a custom placeholder secret (experimental).
//
// A custom secret is conceptually a {placeholder, value, host[]} tuple (spec
// §5.6). T12 stores the secret VALUE under ppp.custom.<name> through the Store
// seam; wiring the placeholder + host list into the running addon's
// Resolver.SetCustom is a runtime concern that needs the live proxy and is
// deferred to T13. The --placeholder flag is required now so the stored record
// is unambiguous once that wiring lands.
func newSecretSetCustomCmd(d deps) *cobra.Command {
	var (
		name        string
		placeholder string
		fromEnv     string
	)
	cmd := &cobra.Command{
		Use:   "set-custom --name NAME --placeholder TOKEN [--from-env VAR]",
		Short: "Store a custom placeholder secret (experimental)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if name == "" || placeholder == "" {
				return fmt.Errorf("both --name and --placeholder are required")
			}
			value, err := readSecretValue(cmd, fromEnv)
			if err != nil {
				return err
			}
			store, err := requireMutable(d.newStore())
			if err != nil {
				return err
			}
			key := "ppp.custom." + strings.ToLower(name)
			if err := store.Set(key, value); err != nil {
				return err
			}
			return outf(cmd.OutOrStdout(), "stored custom secret %s\n", key)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "custom secret label")
	cmd.Flags().StringVar(&placeholder, "placeholder", "", "placeholder token substituted on outbound headers")
	cmd.Flags().StringVar(&fromEnv, "from-env", "", "read the secret value from this environment variable")
	return cmd
}

// newSecretImportCmd imports secrets from host env vars into the global store.
func newSecretImportCmd(d deps) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "import [SERVICE...]",
		Short: "Import secrets from host env vars",
		RunE: func(cmd *cobra.Command, args []string) error {
			candidates := importCandidates(args)
			var found []struct{ svc, env string }
			for svc, env := range candidates {
				if _, ok := os.LookupEnv(env); ok {
					found = append(found, struct{ svc, env string }{svc, env})
				}
			}
			sort.Slice(found, func(i, j int) bool { return found[i].svc < found[j].svc })
			if dryRun {
				for _, f := range found {
					if err := outf(cmd.OutOrStdout(), "would import %s from $%s\n", f.svc, f.env); err != nil {
						return err
					}
				}
				return nil
			}
			store, err := requireMutable(d.newStore())
			if err != nil {
				return err
			}
			for _, f := range found {
				key := secretKey(f.svc, "")
				if err := store.Set(key, os.Getenv(f.env)); err != nil {
					return err
				}
				if err := outf(cmd.OutOrStdout(), "imported %s from $%s\n", f.svc, f.env); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what would be imported without storing")
	return cmd
}

// newSecretLsCmd lists stored secret KEYS (redacted — values never printed).
func newSecretLsCmd(d deps) *cobra.Command {
	return &cobra.Command{
		Use:   "ls [SANDBOX]",
		Short: "List stored secrets (names only, redacted)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := requireMutable(d.newStore())
			if err != nil {
				return err
			}
			keys, err := store.Keys()
			if err != nil {
				return err
			}
			sort.Strings(keys)
			scope := firstArg(args)
			shown := 0
			for _, key := range keys {
				if scope != "" && !strings.HasPrefix(key, "ppp."+scope+".") {
					continue
				}
				if err := outf(cmd.OutOrStdout(), "%s\t(set)\n", key); err != nil {
					return err
				}
				shown++
			}
			if shown == 0 {
				return outf(cmd.OutOrStdout(), "no secrets stored\n")
			}
			return nil
		},
	}
}

// newSecretRmCmd deletes a secret by service (and optional sandbox scope).
func newSecretRmCmd(d deps) *cobra.Command {
	var sandboxName string
	cmd := &cobra.Command{
		Use:   "rm SERVICE",
		Short: "Delete a secret",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := normalizeServiceName(args[0])
			if err != nil {
				return err
			}
			store, err := requireMutable(d.newStore())
			if err != nil {
				return err
			}
			key := secretKey(svc, sandboxName)
			if err := store.Delete(key); err != nil {
				return err
			}
			return outf(cmd.OutOrStdout(), "removed secret %s\n", key)
		},
	}
	cmd.Flags().StringVar(&sandboxName, "sandbox", "", "scope to a sandbox")
	return cmd
}

// readSecretValue reads a secret value from the named env var, or from stdin
// when no env var is given. It never accepts a value as a CLI argument. A blank
// value is rejected.
func readSecretValue(cmd *cobra.Command, fromEnv string) (string, error) {
	if fromEnv != "" {
		v, ok := os.LookupEnv(fromEnv)
		if !ok {
			return "", fmt.Errorf("environment variable %q is not set", fromEnv)
		}
		if strings.TrimSpace(v) == "" {
			return "", fmt.Errorf("environment variable %q is empty", fromEnv)
		}
		return v, nil
	}
	scanner := bufio.NewScanner(cmd.InOrStdin())
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("reading secret from stdin: %w", err)
		}
		return "", fmt.Errorf("no secret value provided on stdin")
	}
	v := scanner.Text()
	if strings.TrimSpace(v) == "" {
		return "", fmt.Errorf("empty secret value")
	}
	return v, nil
}

// secretKey builds the storage key for a service secret, mirroring the T7
// naming (ppp.<svc> global, ppp.<sandbox>.<svc> per-sandbox).
func secretKey(service, sandboxName string) string {
	if sandboxName == "" {
		return "ppp." + service
	}
	return "ppp." + sandboxName + "." + service
}

// normalizeServiceName lower-cases, trims, and validates a service name so keys
// are consistent and free of separator characters that would corrupt the key.
func normalizeServiceName(service string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(service))
	if s == "" {
		return "", fmt.Errorf("service name is empty")
	}
	if strings.ContainsAny(s, ". \t/:") {
		return "", fmt.Errorf("invalid service name %q (no dots, spaces, slashes, or colons)", service)
	}
	return s, nil
}

// importEnvVars maps a v1 service name to the host env var it is imported from
// (spec §6.9/§6.18).
var importEnvVars = map[string]string{
	"anthropic":  "ANTHROPIC_API_KEY",
	"openai":     "OPENAI_API_KEY",
	"github":     "GH_TOKEN",
	"google":     "GOOGLE_API_KEY",
	"groq":       "GROQ_API_KEY",
	"mistral":    "MISTRAL_API_KEY",
	"openrouter": "OPENROUTER_API_KEY",
	"xai":        "XAI_API_KEY",
}

// importCandidates returns the service→env map to consider for import: the
// requested services when args is non-empty, otherwise all known services.
func importCandidates(args []string) map[string]string {
	if len(args) == 0 {
		return importEnvVars
	}
	out := map[string]string{}
	for _, a := range args {
		svc := strings.ToLower(strings.TrimSpace(a))
		if env, ok := importEnvVars[svc]; ok {
			out[svc] = env
		}
	}
	return out
}
