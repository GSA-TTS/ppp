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
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Compose returns a PEM CA bundle suitable for OpenSSL upstream verification.
//
// Precedence:
//  1. if override != "" it is returned as-is (the PPP_UPSTREAM_CA escape hatch);
//  2. otherwise: the host OS trust store, with non-critical-BasicConstraints CA
//     certs dropped (OpenSSL 3 rejects them and they poison chain building).
//
// Normal public chains verify against this bundle directly. Interception chains
// (whose non-conformant root was dropped here) are handled at handshake time by
// the addon's verify callback, which authorizes them against the host trust
// store (ADR-0006) — so no interception cert is baked into this bundle.
//
// The composed PEM is returned as bytes; the caller writes it under $PPP_DATA.
func Compose(override string) ([]byte, error) {
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
	return filterOpenSSLUsable(osPEM), nil
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
