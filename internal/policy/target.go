package policy

import (
	"fmt"
	"strconv"
	"strings"
)

// Target is the network destination a policy decision is made about: the host
// (a domain name or IP literal) and the destination port. A zero Port means the
// request did not specify one, so a rule that constrains a port cannot match.
type Target struct {
	Host string
	Port int
}

// ParseTarget parses a "host" or "host:port" string into a Target. IPv6 hosts
// must be bracketed when a port is present, e.g. "[::1]:443"; a bare "::1" is
// treated as a host with no port. The input is untrusted and validated: an
// empty host or an out-of-range port yields an error so callers can fail
// closed rather than evaluate against a malformed target.
func ParseTarget(s string) (Target, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Target{}, fmt.Errorf("policy: empty target")
	}
	host, portStr, hasPort := splitHostPort(s)
	if host == "" {
		return Target{}, fmt.Errorf("policy: target %q has empty host", s)
	}
	if !hasPort {
		return Target{Host: host}, nil
	}
	port, err := parsePort(portStr)
	if err != nil {
		return Target{}, fmt.Errorf("policy: target %q: %w", s, err)
	}
	return Target{Host: host, Port: port}, nil
}

// splitHostPort separates an optional ":port" suffix from a host, handling
// bracketed IPv6 literals and leaving bare (colon-containing) IPv6 hosts intact.
func splitHostPort(s string) (host, port string, hasPort bool) {
	if strings.HasPrefix(s, "[") {
		if end := strings.Index(s, "]"); end >= 0 {
			host = s[1:end]
			rest := s[end+1:]
			if strings.HasPrefix(rest, ":") {
				return host, rest[1:], true
			}
			return host, "", false
		}
		return s, "", false
	}
	if strings.Count(s, ":") == 1 {
		i := strings.IndexByte(s, ':')
		return s[:i], s[i+1:], true
	}
	return s, "", false
}

func parsePort(s string) (int, error) {
	port, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid port %q", s)
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("port %d out of range", port)
	}
	return port, nil
}
