// Package secret resolves which credential to inject into an outbound request
// and where that credential comes from, behind a swappable Store (spec §5.6,
// ADR-0004).
//
// Resolution has two shapes:
//
//   - Service secrets. Resolve(service, sandbox) applies per-sandbox→global
//     precedence — the sandbox-scoped key "ppp.<sandbox>.<service>" always wins
//     over the global "ppp.<service>" (USAi charge-back support) — then maps the
//     provider to an HTTP header via a small, human-reviewable table
//     (anthropic → x-api-key, google/gemini → x-goog-api-key, everything else →
//     Authorization: Bearer). It returns an Injection{Header, Value}.
//   - Custom secrets. ResolveCustom(host) returns the {placeholder→value}
//     Substitutions whose host list matches the request host; the addon applies
//     them to outbound headers.
//
// The resolution logic is deliberately independent of any concrete Store, so it
// is unit-tested against a fake in-memory store — the real OS keychain is never
// touched in tests and no real key is ever embedded.
//
// Two Store implementations ship: KeyringStore (primary; the OS keychain via
// go-keyring) and AgeStore (fallback; an age-encrypted file used only where no
// keychain backend exists). AgeStore models a locked state: it is unlocked once
// at daemon start (via PPP_AGE_PASSPHRASE or an interactive prompt) and returns
// the ErrLocked sentinel until then, never decrypting per request.
//
// This package intentionally stops at resolution and the Store seam. The UDS
// RPC server that exposes Resolve/ResolveCustom to the mitmproxy addon is added
// separately (ticket T8); Resolver is shaped to be called cleanly by it.
package secret
