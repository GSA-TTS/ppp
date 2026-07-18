// Package catrust composes the CA bundle mitmproxy uses to verify UPSTREAM
// (real server) TLS. mitmproxy is Python/OpenSSL and does not read the macOS
// Keychain, and OpenSSL 3 strict-rejects a CA certificate whose BasicConstraints
// extension is not marked critical — which some corporate interception roots
// (e.g. Zscaler's) violate. ppp therefore builds its own bundle at daemon start:
// export the OS trust store, DROP any CA cert with a non-critical
// BasicConstraints (OpenSSL would reject it and it poisons the whole
// verification), and append vendored interception roots as a fallback. See
// ADR-0006.
package catrust

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Compose returns a PEM CA bundle suitable for OpenSSL upstream verification.
//
// Precedence:
//  1. if override != "" it is returned as-is (the PPP_UPSTREAM_CA escape hatch);
//  2. otherwise: the host OS trust store, with non-critical-BasicConstraints CA
//     certs dropped, plus any extraPEM (vendored/probed interception roots and
//     intermediates).
//
// The composed PEM is returned as bytes; the caller writes it under $PPP_DATA.
func Compose(override string, extraPEM []byte) ([]byte, error) {
	if override != "" {
		data, err := os.ReadFile(override)
		if err != nil {
			return nil, fmt.Errorf("catrust: reading PPP_UPSTREAM_CA %q: %w", override, err)
		}
		return data, nil
	}

	osPEM, err := osTrustStorePEM()
	if err != nil {
		return nil, err
	}
	combined := append([]byte{}, osPEM...)
	if len(extraPEM) > 0 {
		combined = append(combined, '\n')
		combined = append(combined, extraPEM...)
	}
	return filterOpenSSLUsable(combined), nil
}

// ProbeInterceptionCAs opens a TLS connection to probeHost:443 and returns the
// PEM of the CA (non-leaf) certificates the server presents. On a TLS-inspecting
// network this yields the interception intermediate(s) — including a conformant
// one that can anchor the chain under partial-chain verification even when the
// interception ROOT is non-conformant (ADR-0006). It verifies nothing (it is
// only harvesting the presented chain), and returns an empty result (no error)
// when the presented chain is a normal public chain or the probe fails, so a
// normal network simply contributes nothing extra.
func ProbeInterceptionCAs(probeHost string) []byte {
	dialer := &net.Dialer{Timeout: 8 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", probeHost+":443", &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // harvesting the presented chain only; nothing is trusted from this probe
		ServerName:         probeHost,
	})
	if err != nil {
		return nil
	}
	defer func() { _ = conn.Close() }()

	var out []byte
	for i, cert := range conn.ConnectionState().PeerCertificates {
		if i == 0 {
			continue // skip the leaf; only CA certs may anchor
		}
		if !cert.IsCA {
			continue
		}
		out = append(out, pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: cert.Raw,
		})...)
	}
	return out
}

// filterOpenSSLUsable drops CA certificates whose BasicConstraints is present
// but not marked critical. OpenSSL 3 rejects such a cert during chain building
// ("Basic Constraints of CA cert not marked critical"), and including it makes
// verification fail even when a conformant intermediate could anchor the chain.
// Non-CA (leaf) certs and certs without BasicConstraints are kept unchanged.
func filterOpenSSLUsable(pemBytes []byte) []byte {
	var out []byte
	rest := pemBytes
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue // unparseable: drop it rather than risk poisoning the store
		}
		if cert.IsCA && cert.BasicConstraintsValid && !isBasicConstraintsCritical(cert) {
			continue // OpenSSL 3 would reject this CA cert; drop it
		}
		out = append(out, pem.EncodeToMemory(block)...)
	}
	return out
}

// isBasicConstraintsCritical reports whether the cert's BasicConstraints
// extension (OID 2.5.29.19) is marked critical.
func isBasicConstraintsCritical(cert *x509.Certificate) bool {
	for _, ext := range cert.Extensions {
		if ext.Id.String() == "2.5.29.19" {
			return ext.Critical
		}
	}
	return false
}

// osTrustStorePEM exports the host OS trust store as PEM. macOS keeps roots in
// the Keychain (not a PEM file), so we export via `security`; Linux ships a PEM
// bundle at a well-known path.
func osTrustStorePEM() ([]byte, error) {
	switch runtime.GOOS {
	case "darwin":
		return macOSTrustStorePEM()
	default:
		return linuxTrustStorePEM()
	}
}

// macOSTrustStorePEM concatenates the admin (System) and system-root keychains.
func macOSTrustStorePEM() ([]byte, error) {
	keychains := []string{
		"/Library/Keychains/System.keychain",
		"/System/Library/Keychains/SystemRootCertificates.keychain",
	}
	var out []byte
	for _, kc := range keychains {
		cmd := exec.Command("security", "find-certificate", "-a", "-p", kc)
		pemOut, err := cmd.Output()
		if err != nil {
			// A missing/unreadable keychain is non-fatal; other sources cover it.
			continue
		}
		out = append(out, pemOut...)
		out = append(out, '\n')
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("catrust: could not export any macOS keychain trust store")
	}
	return out, nil
}

// linuxTrustStorePEM reads the first present well-known CA bundle.
func linuxTrustStorePEM() ([]byte, error) {
	candidates := []string{
		"/etc/ssl/certs/ca-certificates.crt", // Debian/Ubuntu
		"/etc/pki/tls/certs/ca-bundle.crt",   // Fedora/RHEL
		"/etc/ssl/cert.pem",                  // some minimal distros
	}
	for _, p := range candidates {
		if data, err := os.ReadFile(p); err == nil && len(strings.TrimSpace(string(data))) > 0 {
			return data, nil
		}
	}
	return nil, fmt.Errorf("catrust: no system CA bundle found in %s", strings.Join(candidates, ", "))
}
