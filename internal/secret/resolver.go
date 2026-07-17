package secret

import (
	"errors"
	"fmt"
	"strings"
)

// Injection is the header a request must carry for an authenticated service:
// the header name and its fully-formed value (already including any scheme
// prefix such as "Bearer "). The addon sets Header to Value on the outbound
// request; the raw key never leaves the host.
type Injection struct {
	Header string
	Value  string
}

// Substitution is a placeholder→value replacement the addon applies to
// outbound headers for a custom secret whose host list matches the request.
type Substitution struct {
	Placeholder string
	Value       string
}

// CustomSecret is a {placeholder, value, host[]} tuple (spec §5.6). When a
// request's host matches one of Hosts, the addon substitutes Placeholder with
// Value in outbound headers. Name is the human label (stored conceptually under
// "ppp.custom.<name>").
type CustomSecret struct {
	Name        string
	Placeholder string
	Value       string
	Hosts       []string
}

// Resolver decides which secret to inject and where it comes from. It holds a
// Store for service secrets and an in-memory set of custom-secret tuples. The
// resolution logic (per-sandbox→global precedence, provider→header mapping,
// custom host matching) is independent of any concrete Store so it can be
// unit-tested against a fake. The Resolver is the surface the T8 UDS server
// calls; it holds no socket or transport concern itself.
type Resolver struct {
	store   Store
	customs []CustomSecret
}

// NewResolver builds a Resolver over the given service-secret Store.
func NewResolver(store Store) *Resolver {
	return &Resolver{store: store}
}

// SetCustom replaces the resolver's custom-secret set. It copies the slice so
// the caller cannot mutate the resolver's state after the fact.
func (r *Resolver) SetCustom(customs []CustomSecret) {
	r.customs = append([]CustomSecret(nil), customs...)
}

// Resolve returns the Injection for a service in a sandbox, applying
// per-sandbox→global precedence: the sandbox-scoped key ("ppp.<sandbox>.<svc>")
// is tried first and always wins; it falls back to the global key
// ("ppp.<svc>") when absent.
//
// The boolean is true when a secret was found. A missing secret is not an
// error: it returns (Injection{}, false, nil). A locked backing store returns
// ErrLocked so the caller can report reason:"locked". An empty service is an
// error.
func (r *Resolver) Resolve(service, sandbox string) (Injection, bool, error) {
	svc, err := normalizeService(service)
	if err != nil {
		return Injection{}, false, err
	}

	rawKey, found, err := r.lookup(svc, sandbox)
	if err != nil {
		return Injection{}, false, err
	}
	if !found {
		return Injection{}, false, nil
	}
	return schemeFor(svc).inject(rawKey), true, nil
}

// lookup implements the precedence order against the store. It treats
// ErrNotFound as "try the next candidate" and propagates any other error
// (notably ErrLocked) unchanged.
func (r *Resolver) lookup(service, sandbox string) (string, bool, error) {
	candidates := make([]string, 0, 2)
	if sandbox != "" {
		candidates = append(candidates, serviceKey(service, sandbox))
	}
	candidates = append(candidates, serviceKey(service, ""))

	for _, key := range candidates {
		v, err := r.store.Get(key)
		switch {
		case err == nil:
			return v, true, nil
		case errors.Is(err, ErrNotFound):
			continue
		default:
			return "", false, fmt.Errorf("secret: lookup %q: %w", key, err)
		}
	}
	return "", false, nil
}

// ResolveCustom returns every custom substitution whose host list matches the
// request host, in the order the tuples were registered. A host that matches no
// tuple yields an empty slice (not an error). An empty host is an error.
func (r *Resolver) ResolveCustom(host string) ([]Substitution, error) {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" {
		return nil, fmt.Errorf("secret: request host is empty")
	}

	var subs []Substitution
	for _, c := range r.customs {
		if hostMatches(c.Hosts, h) {
			subs = append(subs, Substitution{Placeholder: c.Placeholder, Value: c.Value})
		}
	}
	return subs, nil
}

// hostMatches reports whether the normalized request host equals any host in
// the tuple's list, case-insensitively.
func hostMatches(hosts []string, host string) bool {
	for _, candidate := range hosts {
		if strings.ToLower(strings.TrimSpace(candidate)) == host {
			return true
		}
	}
	return false
}
