package secret

import (
	"fmt"
	"strings"
)

// keyPrefix namespaces every ppp secret key so keychain entries and the age
// store share one flat, greppable namespace (spec §5.6).
const keyPrefix = "ppp"

// serviceKey builds the keychain/age lookup key for a service secret. When
// sandbox is non-empty it produces the per-sandbox key ("ppp.<sandbox>.<svc>");
// otherwise the global key ("ppp.<svc>"). The Resolver tries the sandbox key
// first, so this function encodes only naming, not precedence.
func serviceKey(service, sandbox string) string {
	if sandbox == "" {
		return keyPrefix + "." + service
	}
	return keyPrefix + "." + sandbox + "." + service
}

// normalizeService lower-cases and trims a service name so provider lookups and
// key construction are case-insensitive and whitespace-tolerant.
func normalizeService(service string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(service))
	if s == "" {
		return "", fmt.Errorf("secret: service name is empty")
	}
	return s, nil
}
