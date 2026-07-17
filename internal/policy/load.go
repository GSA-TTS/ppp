package policy

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// defaultAction is the YAML token for a policy's default decision. "block" maps
// to Deny; "allow" (or the "allow-all" preset shorthand) maps to Allow.
const (
	actionBlock    = "block"
	actionAllow    = "allow"
	actionAllowAll = "allow-all"
)

// policyDoc is the on-disk YAML schema for a policy document (spec §5.5).
type policyDoc struct {
	Default string    `yaml:"default"`
	Rules   []ruleDoc `yaml:"rules"`
}

// ruleDoc is the on-disk YAML schema for a single rule.
type ruleDoc struct {
	ID        string `yaml:"id"`
	Decision  string `yaml:"decision"`
	Type      string `yaml:"type"`
	Resource  string `yaml:"resource"`
	CreatedAt string `yaml:"created_at"`
	Source    string `yaml:"source"`
}

// denyAll is a policy that denies every target. It is returned alongside any
// load error so a caller that mishandles the error still fails closed.
func denyAll() Policy { return Policy{defaultDecn: Deny} }

// Load parses a policy document from YAML bytes. It fails closed: on any error
// (malformed YAML, unknown default, or an unparseable rule) it returns a
// deny-all policy together with a non-nil error. A missing "default" is treated
// as block (deny), never as allow.
func Load(data []byte) (Policy, error) {
	var doc policyDoc
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil && err != io.EOF {
		return denyAll(), fmt.Errorf("policy: parse YAML: %w", err)
	}
	return fromDoc(doc)
}

// LoadFile reads and parses a policy YAML file. A read error also fails closed
// with a deny-all policy.
func LoadFile(path string) (Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return denyAll(), fmt.Errorf("policy: read %q: %w", path, err)
	}
	return Load(data)
}

// fromDoc converts a parsed document into a compiled Policy, validating the
// default and every rule. Any invalid rule fails the whole load closed.
func fromDoc(doc policyDoc) (Policy, error) {
	def, err := parseDefault(doc.Default)
	if err != nil {
		return denyAll(), err
	}
	rules := make([]Rule, 0, len(doc.Rules))
	for i, rd := range doc.Rules {
		r, err := rd.compile()
		if err != nil {
			return denyAll(), fmt.Errorf("policy: rule %d: %w", i, err)
		}
		rules = append(rules, r)
	}
	return NewPolicy(rules, def)
}

// parseDefault maps the YAML default token to a Decision. An empty default is
// block (deny) so an omitted default never opens traffic.
func parseDefault(s string) (Decision, error) {
	switch s {
	case "", actionBlock:
		return Deny, nil
	case actionAllow, actionAllowAll:
		return Allow, nil
	default:
		return Deny, fmt.Errorf("policy: invalid default action %q (want %q or %q)", s, actionAllow, actionBlock)
	}
}

// compile validates a rule document and compiles it into a Rule. The type, when
// present, must be "network"; the decision and resource are validated by
// NewRule; the source, when present, must be local or kit.
func (rd ruleDoc) compile() (Rule, error) {
	if rd.Type != "" && rd.Type != "network" {
		return Rule{}, fmt.Errorf("unsupported type %q", rd.Type)
	}
	src, err := parseSource(rd.Source)
	if err != nil {
		return Rule{}, err
	}
	return NewRule(rd.ID, Decision(rd.Decision), rd.Resource, rd.CreatedAt, src)
}

// parseSource maps the YAML source token to a Source. An empty source defaults
// to local.
func parseSource(s string) (Source, error) {
	switch s {
	case "", string(SourceLocal):
		return SourceLocal, nil
	case string(SourceKit):
		return SourceKit, nil
	default:
		return "", fmt.Errorf("invalid source %q (want %q or %q)", s, SourceLocal, SourceKit)
	}
}
