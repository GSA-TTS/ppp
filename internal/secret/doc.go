// Package secret manages credential storage and the addon IPC server (spec §5.6).
//
// It will store secrets in the OS keychain (with an age-encrypted fallback) and
// serve them to the mitmproxy addon over a Unix domain socket so credentials are
// injected host-side and never enter a sandbox. No logic is implemented yet.
package secret
