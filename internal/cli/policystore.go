package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/GSA-TTS/ppp/internal/policy"
	"github.com/GSA-TTS/ppp/internal/sandbox"
)

// policyDoc mirrors the on-disk policies.yaml schema (spec §5.5). It is a
// separate, cli-local shape used for editing (append/remove rules) and writing;
// evaluation goes through policy.LoadFile, which parses this same schema. The
// two must agree on field tags — see policyRuleDoc.
type policyDoc struct {
	Default string          `yaml:"default"`
	Rules   []policyRuleDoc `yaml:"rules"`
}

// policyRuleDoc is one rule in policies.yaml.
type policyRuleDoc struct {
	ID        string `yaml:"id"`
	Decision  string `yaml:"decision"`
	Type      string `yaml:"type"`
	Resource  string `yaml:"resource"`
	CreatedAt string `yaml:"created_at"`
	Source    string `yaml:"source"`
}

// preset default tokens for `policy init` (spec §5.5). balanced and deny-all
// both default to block; allow-all defaults to allow. v1 seeds no rules for any
// preset (the "balanced" seed list is a later enhancement; deferred and tracked
// so the preset defaults are honest rather than half-populated).
var presetDefaults = map[string]string{
	"allow-all": "allow",
	"balanced":  "block",
	"deny-all":  "block",
}

// globalPolicyPath returns $PPP_CONFIG/policies.yaml (spec §5.8).
func globalPolicyPath() (string, error) {
	cfg, err := sandbox.ResolveConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cfg, "policies.yaml"), nil
}

// sandboxPolicyPath returns $PPP_DATA/sandboxes/<name>/policy.yaml (spec §5.8).
func sandboxPolicyPath(name string) (string, error) {
	dir, err := sandbox.SandboxDir(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "policy.yaml"), nil
}

// policyPathFor returns the effective policy path: the per-sandbox policy when
// sandbox is non-empty, otherwise the global policy.
func policyPathFor(sandboxName string) (string, error) {
	if sandboxName != "" {
		if err := validateSandboxName(sandboxName); err != nil {
			return "", err
		}
		return sandboxPolicyPath(sandboxName)
	}
	return globalPolicyPath()
}

// writePreset writes a preset policy document to path, creating the parent
// directory. It is idempotent per spec §6.17: writing the same preset twice
// yields the same file.
func writePreset(path, preset string) error {
	def, ok := presetDefaults[preset]
	if !ok {
		return fmt.Errorf("unknown preset %q (want allow-all, balanced, or deny-all)", preset)
	}
	return writePolicyDoc(path, policyDoc{Default: def})
}

// readPolicyDoc reads and parses a policy document for editing. A missing file
// is not an error: it returns a fresh block-default document so append/remove
// operations create the file on first use.
func readPolicyDoc(path string) (policyDoc, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return policyDoc{Default: "block"}, nil
	}
	if err != nil {
		return policyDoc{}, fmt.Errorf("reading policy %q: %w", path, err)
	}
	var doc policyDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return policyDoc{}, fmt.Errorf("parsing policy %q: %w", path, err)
	}
	if doc.Default == "" {
		doc.Default = "block"
	}
	return doc, nil
}

// writePolicyDoc marshals and writes a policy document atomically (temp file +
// rename) with 0600 perms, creating the parent directory.
func writePolicyDoc(path string, doc policyDoc) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating policy dir: %w", err)
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshaling policy: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("writing temp policy: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming policy into place: %w", err)
	}
	return nil
}

// splitResources splits a comma-separated RESOURCES argument list into trimmed,
// non-empty tokens. Each token is validated by policy.NewRule when the rule is
// compiled, so this only handles splitting and empty-token rejection.
func splitResources(args []string) ([]string, error) {
	var out []string
	for _, arg := range args {
		for _, tok := range strings.Split(arg, ",") {
			t := strings.TrimSpace(tok)
			if t != "" {
				out = append(out, t)
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no resources given")
	}
	return out, nil
}

// ruleID generates a deterministic-per-call rule id from the decision, resource
// and a nanosecond timestamp. It avoids a uuid dependency (spec allows any
// unique id) while staying greppable and collision-resistant for local use.
func ruleID(decision, resource string, now time.Time) string {
	safe := strings.NewReplacer("*", "x", "/", "-", ":", "-", ".", "-", "?", "q").Replace(resource)
	return fmt.Sprintf("%s-%s-%d", decision, safe, now.UnixNano())
}

// appendRules adds one rule per resource with the given decision to the policy
// at path, validating each resource before writing so a malformed resource
// fails the whole operation closed (no partial write). It returns the ids added.
func appendRules(path, decision string, resources []string) ([]string, error) {
	doc, err := readPolicyDoc(path)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	created := now.Format(time.RFC3339)
	var added []string
	for _, res := range resources {
		// Validate up front via NewRule so a bad resource fails closed.
		if _, err := policy.NewRule("preflight", policy.Decision(decision), res, created, policy.SourceLocal); err != nil {
			return nil, err
		}
		id := ruleID(decision, res, now)
		doc.Rules = append(doc.Rules, policyRuleDoc{
			ID:        id,
			Decision:  decision,
			Type:      "network",
			Resource:  res,
			CreatedAt: created,
			Source:    string(policy.SourceLocal),
		})
		added = append(added, id)
		now = now.Add(time.Nanosecond) // keep ids distinct within one call
	}
	if err := writePolicyDoc(path, doc); err != nil {
		return nil, err
	}
	return added, nil
}
