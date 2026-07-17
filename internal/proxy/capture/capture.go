package capture

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// fenceLen is the exact number of hyphens in a fence line. mitmdump emits
	// each WireGuard client config fenced by a line of exactly 60 hyphens.
	fenceLen = 60

	// gvproxyHostAlias is the host alias reachable from inside the guest via
	// gvproxy; the rewritten Endpoint always points here (spec §3.1), never the
	// captured LAN host and never the host LAN IP.
	gvproxyHostAlias = "192.168.127.254"
)

// fence is the bare 60-hyphen closing-fence line.
var fence = strings.Repeat("-", fenceLen)

// Config is a parsed WireGuard client config emitted by one mitmdump instance.
// It is indexed by ListenPort, which is the sandbox's identity (ADR-0003); the
// port is parsed from the block's Endpoint line, never from emission order.
type Config struct {
	// ListenPort is the WireGuard listen port, parsed from the Endpoint line.
	ListenPort int
	// Address is the client tunnel address as emitted (e.g. "10.0.0.1/32").
	Address string
	// PrivateKey is the [Interface] PrivateKey — the CLIENT's private key the
	// guest authenticates as. It MUST be carried into the rewritten wg0.conf:
	// without it wg-quick generates a random key and the mitmproxy server
	// rejects the handshake (InvalidPacket). It is the guest's own key, not a
	// server secret, so retaining it does not leak server key material.
	PrivateKey string
	// PublicKey is the [Peer] PublicKey — the SERVER public key the guest must
	// use.
	PublicKey string
	// EndpointHost is the host portion of the emitted Endpoint (rewritten away).
	EndpointHost string
	// AllowedIPs is the [Peer] AllowedIPs as emitted (e.g. "0.0.0.0/0").
	AllowedIPs string
}

// Parse scans mitmdump's stdout capture and returns one Config per fenced
// WireGuard client-config block. It uses a strict line scanner and fails closed
// with a descriptive error on any format drift (malformed fence, missing or
// unparseable fields), so a mitmproxy format change is loud rather than silent.
func Parse(log []byte) ([]Config, error) {
	lines := strings.Split(string(log), "\n")
	var cfgs []Config
	for i := 0; i < len(lines); i++ {
		if lines[i] == fence {
			// A bare 60-hyphen fence outside a recognized (timestamp-prefixed)
			// opening fence means a malformed opening fence or a stray closing
			// fence — drift, not a log line to skip.
			return nil, fmt.Errorf("capture: unexpected bare fence at line %d (no matching opening fence)", i+1)
		}
		if !isOpeningFence(lines[i]) {
			continue
		}
		body, next, err := readBlock(lines, i+1)
		if err != nil {
			return nil, fmt.Errorf("capture: block opened at line %d: %w", i+1, err)
		}
		cfg, err := parseBlock(body)
		if err != nil {
			return nil, fmt.Errorf("capture: block opened at line %d: %w", i+1, err)
		}
		cfgs = append(cfgs, cfg)
		i = next
	}
	if len(cfgs) == 0 {
		return nil, fmt.Errorf("capture: no WireGuard config blocks found in log")
	}
	return cfgs, nil
}

// isOpeningFence reports whether a line is a timestamp-prefixed opening fence:
// "[HH:MM:SS.mmm] " followed by exactly 60 hyphens.
func isOpeningFence(line string) bool {
	rest, ok := strings.CutPrefix(line, "[")
	if !ok {
		return false
	}
	ts, after, ok := strings.Cut(rest, "] ")
	if !ok || !isTimestamp(ts) {
		return false
	}
	return after == fence
}

// isTimestamp reports whether s looks like "HH:MM:SS.mmm".
func isTimestamp(s string) bool {
	hms, ms, ok := strings.Cut(s, ".")
	if !ok || len(ms) != 3 || !allDigits(ms) {
		return false
	}
	parts := strings.Split(hms, ":")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if len(p) != 2 || !allDigits(p) {
			return false
		}
	}
	return true
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}

// readBlock collects lines from start until the bare closing fence, returning
// the block body and the index of the closing fence. A missing closing fence is
// drift.
func readBlock(lines []string, start int) (body []string, closeIdx int, err error) {
	for j := start; j < len(lines); j++ {
		if lines[j] == fence {
			return lines[start:j], j, nil
		}
	}
	return nil, 0, fmt.Errorf("unterminated block: no closing 60-hyphen fence")
}

// parseBlock turns the INI-like body between fences into a Config, requiring the
// [Interface] Address and the [Peer] PublicKey/Endpoint/AllowedIPs fields.
func parseBlock(body []string) (Config, error) {
	fields, err := scanFields(body)
	if err != nil {
		return Config{}, err
	}
	address, ok := fields["Address"]
	if !ok {
		return Config{}, fmt.Errorf("missing Address field")
	}
	privateKey, ok := fields["PrivateKey"]
	if !ok {
		return Config{}, fmt.Errorf("missing PrivateKey field")
	}
	publicKey, ok := fields["PublicKey"]
	if !ok {
		return Config{}, fmt.Errorf("missing PublicKey field")
	}
	endpoint, ok := fields["Endpoint"]
	if !ok {
		return Config{}, fmt.Errorf("missing Endpoint field")
	}
	host, port, err := splitEndpoint(endpoint)
	if err != nil {
		return Config{}, err
	}
	return Config{
		ListenPort:   port,
		Address:      address,
		PrivateKey:   privateKey,
		PublicKey:    publicKey,
		EndpointHost: host,
		AllowedIPs:   fields["AllowedIPs"],
	}, nil
}

// scanFields parses "key = value" lines from a block body, tracking the current
// INI section so a stray line without a section is rejected. Blank lines are
// allowed; any non-blank, non-section line that is not "key = value" is drift.
func scanFields(body []string) (map[string]string, error) {
	fields := make(map[string]string)
	section := ""
	for _, raw := range body {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = line
			continue
		}
		if section == "" {
			return nil, fmt.Errorf("field %q before any section header", line)
		}
		key, val, ok := strings.Cut(line, " = ")
		if !ok {
			return nil, fmt.Errorf("malformed config line %q (want 'key = value')", line)
		}
		fields[strings.TrimSpace(key)] = strings.TrimSpace(val)
	}
	return fields, nil
}

// splitEndpoint splits "host:port" and parses the port, which is the sandbox's
// identity. An empty host or unparseable port is drift.
func splitEndpoint(endpoint string) (host string, port int, err error) {
	h, p, ok := strings.Cut(endpoint, ":")
	if !ok || h == "" {
		return "", 0, fmt.Errorf("malformed Endpoint %q (want host:port)", endpoint)
	}
	port, err = strconv.Atoi(p)
	if err != nil {
		return "", 0, fmt.Errorf("unparseable Endpoint port in %q: %w", endpoint, err)
	}
	if port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("endpoint port %d out of range in %q", port, endpoint)
	}
	return h, port, nil
}

// Rewrite renders the guest's wg0.conf for a sandbox whose inner-IP octet is N
// (10.0.0.N). It carries the client PrivateKey through unchanged (the guest must
// authenticate as the key mitmproxy expects — omitting it makes wg-quick invent
// a random key and the server rejects the handshake), sets the Address to
// 10.0.0.N/32, points the Endpoint at the gvproxy host alias while preserving
// the listen port, adds Table = off, and omits the DNS line entirely (ADR-0005).
// The peer public key comes from the parsed block and is never invented.
func (c Config) Rewrite(innerIPOctet int) (string, error) {
	if innerIPOctet < 1 || innerIPOctet > 255 {
		return "", fmt.Errorf("capture: inner-IP octet %d out of range [1,255]", innerIPOctet)
	}
	if c.PublicKey == "" || c.ListenPort == 0 {
		return "", fmt.Errorf("capture: Rewrite on incomplete Config (port=%d)", c.ListenPort)
	}
	if c.PrivateKey == "" {
		return "", fmt.Errorf("capture: Rewrite on Config missing client PrivateKey (port=%d)", c.ListenPort)
	}
	allowedIPs := c.AllowedIPs
	if allowedIPs == "" {
		allowedIPs = "0.0.0.0/0"
	}
	var b strings.Builder
	fmt.Fprintln(&b, "[Interface]")
	fmt.Fprintf(&b, "PrivateKey = %s\n", c.PrivateKey)
	fmt.Fprintf(&b, "Address = 10.0.0.%d/32\n", innerIPOctet)
	fmt.Fprintln(&b, "Table = off")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "[Peer]")
	fmt.Fprintf(&b, "PublicKey = %s\n", c.PublicKey)
	fmt.Fprintf(&b, "AllowedIPs = %s\n", allowedIPs)
	fmt.Fprintf(&b, "Endpoint = %s:%d\n", gvproxyHostAlias, c.ListenPort)
	return b.String(), nil
}
