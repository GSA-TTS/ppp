// Package proxy supervises the single mitmdump process and manages the WireGuard
// port pool (spec §5.3).
//
// It will start one mitmdump process running many WireGuard instances, capture
// each instance's client config from the proxy log (fenced by 60 hyphens),
// rewrite Address/Endpoint per sandbox, and allocate ports and inner IPs. No
// logic is implemented yet.
package proxy
