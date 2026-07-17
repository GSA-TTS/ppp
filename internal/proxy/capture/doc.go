// Package capture parses mitmdump's emitted WireGuard client configs from its
// stdout log and rewrites them per sandbox for writing to the guest's wg0.conf
// (spec §3.1/§5.3, ADR-0003/0005).
//
// # Parsing
//
// Each WireGuard instance emits its client config once at startup, fenced by a
// line of exactly 60 hyphens. The opening fence carries a "[HH:MM:SS.mmm] "
// timestamp prefix; the closing fence is a bare 60-hyphen line. Between the
// fences is an INI-like block with [Interface] and [Peer] sections. Non-config
// log lines are interleaved and skipped.
//
// Blocks are NOT emitted in --mode flag order — emission order is
// non-deterministic — so [Parse] correlates each block to its port by the
// "Endpoint = host:<port>" line inside the block, never by position. [Parse]
// uses a strict line scanner and fails closed with a descriptive error on any
// format drift (malformed fence, missing or unparseable fields), so a mitmproxy
// format change is loud rather than silent.
//
// # Rewriting
//
// [Config.Rewrite] renders the guest wg0.conf for a sandbox: it sets
// Address = 10.0.0.N/32 for the sandbox inner-IP octet N, points Endpoint at the
// gvproxy host alias 192.168.127.254 while preserving the listen port, adds
// Table = off, and omits the DNS line entirely (ADR-0005). The peer public key
// is taken from the emitted block (the [Peer] PublicKey is the server key) and is
// never invented.
package capture
