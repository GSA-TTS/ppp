// Package policy implements the network policy rules engine shared between the
// CLI and the mitmproxy addon (spec §5.5).
//
// It will parse policy YAML, evaluate rules with deny-wins precedence, and
// support glob/regex host matching plus CIDR IP matching. No logic is
// implemented yet.
package policy
