// Package policy implements the network policy rules engine shared between the
// CLI and the mitmproxy addon (spec §5.5).
//
// A Policy is an ordered set of Rules plus a default Decision. Evaluate applies
// deny-wins precedence — deny > allow > default — and first-match-wins within
// each precedence class. Host matching supports the "**" block-all token, glob
// wildcards ("*.example.com"), auto-detected regular expressions (triggered by
// regex metacharacters), and CIDR/IP matching for IPv4 and IPv6. A rule may
// constrain a destination port via a ":port" suffix on its resource.
//
// Loading is fail-closed: Load and LoadFile return a deny-all Policy together
// with a non-nil error on malformed YAML, an unknown default, or any
// unparseable rule, so a caller that mishandles the error still denies.
package policy
