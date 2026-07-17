package capture_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GSA-TTS/ppp/internal/proxy/capture"
)

// goldenPath is the mitmproxy 12.2.3 capture fixture: two WG instances (ports
// 51820 and 51821), emitted in NON-flag order (the :51821 block appears first).
const goldenPath = "testdata/mitmdump-wg-12.2.3.log"

// readGolden loads the committed golden fixture.
func readGolden(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden fixture: %v", err)
	}
	return b
}

// byPort indexes parsed configs by their listen port for order-independent
// assertions.
func byPort(t *testing.T, cfgs []capture.Config) map[int]capture.Config {
	t.Helper()
	m := make(map[int]capture.Config, len(cfgs))
	for _, c := range cfgs {
		if _, dup := m[c.ListenPort]; dup {
			t.Fatalf("duplicate port %d in parse result", c.ListenPort)
		}
		m[c.ListenPort] = c
	}
	return m
}

func TestParseCorrelatesByEndpointPort(t *testing.T) {
	cfgs, err := capture.Parse(readGolden(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfgs) != 2 {
		t.Fatalf("Parse returned %d configs, want 2", len(cfgs))
	}

	m := byPort(t, cfgs)
	// The fixture emits :51821 FIRST, then :51820. Correlation is by the
	// Endpoint line inside each block, never emission order.
	tests := []struct {
		port          int
		wantPublicKey string
	}{
		{51820, "7Id4Kg5mB4fNSomvYT0T5EzkYk8X0QGlO6XKUzNP9V0="},
		{51821, "sq9NmzZr+dpm9uWPu81sCN6T4pb9qDdr2xm+RO10MQs="},
	}
	for _, tc := range tests {
		c, ok := m[tc.port]
		if !ok {
			t.Fatalf("no config for port %d", tc.port)
		}
		if c.PublicKey != tc.wantPublicKey {
			t.Errorf("port %d PublicKey = %q, want %q", tc.port, c.PublicKey, tc.wantPublicKey)
		}
		if c.Address != "10.0.0.1/32" {
			t.Errorf("port %d Address = %q, want 10.0.0.1/32", tc.port, c.Address)
		}
		if c.ListenPort != tc.port {
			t.Errorf("ListenPort = %d, want %d", c.ListenPort, tc.port)
		}
	}
}

func TestRewriteProducesWgConf(t *testing.T) {
	cfgs, err := capture.Parse(readGolden(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	m := byPort(t, cfgs)

	const octet = 7
	c := m[51820]
	out, err := c.Rewrite(octet)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}

	mustContain := []string{
		"Address = 10.0.0.7/32",
		"Endpoint = 192.168.127.254:51820",
		"Table = off",
		"PublicKey = 7Id4Kg5mB4fNSomvYT0T5EzkYk8X0QGlO6XKUzNP9V0=",
	}
	for _, want := range mustContain {
		if !strings.Contains(out, want) {
			t.Errorf("Rewrite output missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, "DNS =") {
		t.Errorf("Rewrite output must not contain a DNS line\n---\n%s", out)
	}
	// Endpoint host must be the gvproxy alias, never the captured LAN host.
	if strings.Contains(out, "172.17.0.3") {
		t.Errorf("Rewrite output leaked original endpoint host\n---\n%s", out)
	}
	// Port must be preserved from the parsed block.
	if strings.Contains(out, ":51821") {
		t.Errorf("Rewrite for :51820 leaked the other port :51821\n---\n%s", out)
	}
	// The private key from the capture must not survive into wg0.conf: it is
	// the client's private key generated on the guest, not the server's.
	if strings.Contains(out, "nnYdql") || strings.Contains(out, "faRSX8") {
		t.Errorf("Rewrite output leaked a captured PrivateKey\n---\n%s", out)
	}
}

func TestRewriteInvalidOctet(t *testing.T) {
	cfgs, err := capture.Parse(readGolden(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	c := cfgs[0]
	for _, octet := range []int{0, -1, 256, 1000} {
		if _, err := c.Rewrite(octet); err == nil {
			t.Errorf("Rewrite(%d) = nil error, want error for out-of-range octet", octet)
		}
	}
}

// mutate returns a copy of the golden fixture with a single-line transformation
// applied, used to build drift fixtures.
func mutate(t *testing.T, transform func(lines []string) []string) []byte {
	t.Helper()
	lines := strings.Split(string(readGolden(t)), "\n")
	return []byte(strings.Join(transform(lines), "\n"))
}

func TestParseFailsClosedOnDrift(t *testing.T) {
	tests := []struct {
		name      string
		transform func(lines []string) []string
	}{
		{
			name: "truncated opening fence (59 hyphens)",
			transform: func(lines []string) []string {
				// line 1 (index 0) is the prefixed opening fence.
				lines[0] = "[21:43:50.552] " + strings.Repeat("-", 59)
				return lines
			},
		},
		{
			name: "missing Endpoint line",
			transform: func(lines []string) []string {
				out := make([]string, 0, len(lines))
				for _, l := range lines {
					if strings.HasPrefix(strings.TrimSpace(l), "Endpoint =") {
						continue
					}
					out = append(out, l)
				}
				return out
			},
		},
		{
			name: "dropped PublicKey line",
			transform: func(lines []string) []string {
				out := make([]string, 0, len(lines))
				for _, l := range lines {
					if strings.HasPrefix(strings.TrimSpace(l), "PublicKey =") {
						continue
					}
					out = append(out, l)
				}
				return out
			},
		},
		{
			name: "unparseable port in Endpoint",
			transform: func(lines []string) []string {
				for i, l := range lines {
					if strings.HasPrefix(strings.TrimSpace(l), "Endpoint =") {
						lines[i] = "Endpoint = 172.17.0.3:notaport"
						break
					}
				}
				return lines
			},
		},
		{
			name: "missing Address line",
			transform: func(lines []string) []string {
				out := make([]string, 0, len(lines))
				for _, l := range lines {
					if strings.HasPrefix(strings.TrimSpace(l), "Address =") {
						continue
					}
					out = append(out, l)
				}
				return out
			},
		},
		{
			name: "unterminated block (drop a closing fence)",
			transform: func(lines []string) []string {
				out := make([]string, 0, len(lines))
				dropped := false
				fence := strings.Repeat("-", 60)
				for _, l := range lines {
					if !dropped && strings.TrimSpace(l) == fence {
						// skip the FIRST closing fence (the opening one carries
						// a timestamp prefix so is not a bare fence).
						dropped = true
						continue
					}
					out = append(out, l)
				}
				return out
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			corrupt := mutate(t, tc.transform)
			if _, err := capture.Parse(corrupt); err == nil {
				t.Errorf("Parse(%s) = nil error, want a loud drift error", tc.name)
			}
		})
	}
}

func TestParseEmptyLogIsError(t *testing.T) {
	if _, err := capture.Parse(nil); err == nil {
		t.Error("Parse(nil) = nil error, want error (no configs found)")
	}
	if _, err := capture.Parse([]byte("no fences here\njust log lines\n")); err == nil {
		t.Error("Parse(no fences) = nil error, want error (no configs found)")
	}
}

// fixtureIsFor keeps the committed fixture honest against the pinned mitmproxy
// version. If mitmdump is not installed, the check is skipped rather than
// failing, since CI and other environments may lack it.
func TestFixtureMatchesPinnedMitmproxy(t *testing.T) {
	const pinned = "12.2.3"

	path, err := exec.LookPath("mitmdump")
	if err != nil {
		t.Skip("mitmdump not on PATH; skipping version-pin check")
	}
	out, err := exec.Command(path, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("mitmdump --version: %v\n%s", err, out)
	}

	got := parseMitmproxyVersion(t, string(out))
	if got != pinned {
		t.Errorf("mitmdump version = %q, want pinned %q; regenerate %s", got, pinned, filepath.Base(goldenPath))
	}
}

// parseMitmproxyVersion extracts the version from a `mitmdump --version` line of
// the form "Mitmproxy: 12.2.3".
func parseMitmproxyVersion(t *testing.T, out string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "Mitmproxy:"); ok {
			return strings.TrimSpace(rest)
		}
	}
	t.Fatalf("no 'Mitmproxy:' version line in output:\n%s", out)
	return ""
}
