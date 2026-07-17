package policy

import (
	"fmt"
	"net/netip"
	"regexp"
	"strings"
)

// Decision is the outcome of evaluating a target against a policy: either the
// traffic is allowed or it is denied.
type Decision string

const (
	// Allow permits the traffic to the target.
	Allow Decision = "allow"
	// Deny blocks the traffic to the target.
	Deny Decision = "deny"
)

// Source records where a rule came from: authored locally by the operator, or
// supplied by a kit. It is metadata only and does not affect matching.
type Source string

const (
	// SourceLocal marks a rule authored locally by the operator.
	SourceLocal Source = "local"
	// SourceKit marks a rule supplied by a kit.
	SourceKit Source = "kit"
)

// blockAll is the resource token that matches any host (spec §5.5).
const blockAll = "**"

// regexMetachars are the regular-expression metacharacters whose presence in a
// resource host causes it to be treated as a regex rather than a glob. Glob's
// own metacharacters ('*' and '?') are deliberately excluded so a plain
// wildcard resource such as "*.example.com" is NOT misdetected as a regex
// (mirrors pi-container's allowlist.py auto-detection).
const regexMetachars = `^$()[]{}+|\`

// Rule is a single compiled network policy rule. Construct it with NewRule so
// the host matcher and optional port are parsed and validated once, up front;
// a Rule with a nil matcher never matches anything (fail closed).
type Rule struct {
	ID        string
	Decision  Decision
	Resource  string
	CreatedAt string
	Source    Source

	matcher hostMatcher
	port    int // 0 means the rule does not constrain the port
}

// hostMatcher decides whether a target host matches a rule's resource host.
type hostMatcher interface {
	matchHost(host string) bool
}

// NewRule compiles a rule's resource into a matcher. The decision must be Allow
// or Deny and the resource must be non-empty and parseable; otherwise an error
// is returned so callers can fail closed.
func NewRule(id string, decision Decision, resource, createdAt string, source Source) (Rule, error) {
	if decision != Allow && decision != Deny {
		return Rule{}, fmt.Errorf("policy: rule %q has invalid decision %q", id, decision)
	}
	res := strings.TrimSpace(resource)
	if res == "" {
		return Rule{}, fmt.Errorf("policy: rule %q has empty resource", id)
	}
	host, port, err := splitResource(res)
	if err != nil {
		return Rule{}, fmt.Errorf("policy: rule %q: %w", id, err)
	}
	matcher, err := compileHostMatcher(host)
	if err != nil {
		return Rule{}, fmt.Errorf("policy: rule %q: %w", id, err)
	}
	return Rule{
		ID:        id,
		Decision:  decision,
		Resource:  res,
		CreatedAt: createdAt,
		Source:    source,
		matcher:   matcher,
		port:      port,
	}, nil
}

// splitResource splits a resource into its host part and optional port. The
// block-all token and CIDR ranges are returned whole (never port-split), since
// a "/" or a bare "**" cannot carry a ":port" suffix in this schema.
func splitResource(res string) (host string, port int, err error) {
	if res == blockAll || strings.Contains(res, "/") {
		return res, 0, nil
	}
	h, portStr, hasPort := splitHostPort(res)
	if h == "" {
		return "", 0, fmt.Errorf("resource %q has empty host", res)
	}
	if !hasPort {
		return h, 0, nil
	}
	p, err := parsePort(portStr)
	if err != nil {
		return "", 0, fmt.Errorf("resource %q: %w", res, err)
	}
	return h, p, nil
}

// Matches reports whether the target matches this rule: the host must match the
// rule's compiled matcher and, if the rule constrains a port, the target's port
// must equal it. A rule with no compiled matcher never matches.
func (r Rule) Matches(t Target) bool {
	if r.matcher == nil {
		return false
	}
	if r.port != 0 && r.port != t.Port {
		return false
	}
	return r.matcher.matchHost(t.Host)
}

// compileHostMatcher chooses the matching strategy for a resource host:
// block-all, CIDR/IP, regex (auto-detected), or glob.
func compileHostMatcher(host string) (hostMatcher, error) {
	if host == blockAll {
		return allMatcher{}, nil
	}
	if m, ok, err := compileIPMatcher(host); ok || err != nil {
		return m, err
	}
	if isRegex(host) {
		return compileRegexMatcher(host)
	}
	return globMatcher{pattern: host}, nil
}

// isRegex reports whether host contains a regex metacharacter that is not part
// of simple glob syntax, meaning it should be matched as a regular expression.
func isRegex(host string) bool {
	return strings.ContainsAny(host, regexMetachars)
}

// allMatcher matches any host (the "**" block-all token).
type allMatcher struct{}

func (allMatcher) matchHost(string) bool { return true }

// globMatcher matches a host against a glob pattern using '*' and '?'
// wildcards, case-insensitively. Per spec §5.5, a "*.example.com" wildcard
// matches subdomains only ("api.example.com"), NOT the bare parent
// ("example.com"); allowing the apex would over-allow in a deny/allow engine.
type globMatcher struct{ pattern string }

func (g globMatcher) matchHost(host string) bool {
	pat := strings.ToLower(g.pattern)
	h := strings.ToLower(host)
	return globMatch(pat, h)
}

// regexMatcher matches a host against a compiled anchored regular expression.
type regexMatcher struct{ re *regexp.Regexp }

func (m regexMatcher) matchHost(host string) bool { return m.re.MatchString(host) }

func compileRegexMatcher(host string) (hostMatcher, error) {
	re, err := regexp.Compile(anchor(host))
	if err != nil {
		return nil, fmt.Errorf("invalid regex resource %q: %w", host, err)
	}
	return regexMatcher{re: re}, nil
}

// anchor wraps a pattern so it must match the whole host, not a substring.
func anchor(pat string) string {
	if !strings.HasPrefix(pat, "^") {
		pat = "^" + pat
	}
	if !strings.HasSuffix(pat, "$") {
		pat += "$"
	}
	return pat
}

// ipMatcher matches a host that is an IP literal against a single IP or a CIDR
// range (IPv4 or IPv6).
type ipMatcher struct {
	prefix netip.Prefix
	single netip.Addr
}

func (m ipMatcher) matchHost(host string) bool {
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	if m.prefix.IsValid() {
		return m.prefix.Contains(addr)
	}
	return m.single.IsValid() && m.single == addr
}

// compileIPMatcher returns an ipMatcher when host is a CIDR range or a single
// IP literal. ok is false when host is not an IP form (so the caller falls back
// to glob/regex). err is non-nil only when host looks like a CIDR but is
// malformed.
func compileIPMatcher(host string) (hostMatcher, bool, error) {
	if strings.Contains(host, "/") {
		prefix, err := netip.ParsePrefix(host)
		if err != nil {
			return nil, false, fmt.Errorf("invalid CIDR resource %q: %w", host, err)
		}
		return ipMatcher{prefix: prefix.Masked()}, true, nil
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return ipMatcher{single: addr}, true, nil
	}
	return nil, false, nil
}
