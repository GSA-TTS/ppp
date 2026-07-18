// Package catrust composes the CA bundle mitmproxy uses to verify UPSTREAM
// (real server) TLS.
//
// mitmproxy verifies the upstream leg against a PEM bundle only
// (ssl_verify_upstream_trusted_ca, defaulting to certifi) and never consults the
// OS trust store — not the macOS Keychain, not the Windows store. On a
// TLS-inspecting network (e.g. Zscaler) the presented chain therefore has no
// anchor in certifi and verification fails with
// X509_V_ERR_UNABLE_TO_GET_ISSUER_CERT_LOCALLY, even though the host itself
// trusts the interception CA (which is why the host's browser/curl succeed).
//
// The fix is simply to hand mitmproxy the host OS trust store: export it to PEM
// and point ssl_verify_upstream_trusted_ca at it. OpenSSL's default (non-strict)
// verification then anchors the intercepted chain at the interception root the
// host already trusts, while still rejecting genuinely bad certs (expired,
// self-signed, untrusted-root, hostname mismatch). No cert filtering, no
// partial-chain flag, and no custom verify callback are required — see ADR-0006.
package catrust

import (
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
//  2. otherwise: the host OS trust store, exported verbatim.
//
// The bundle is the host trust store as-is: whatever the host trusts (including
// a corporate interception root) becomes a valid upstream anchor, and nothing
// else. The composed PEM is returned as bytes; the caller writes it under
// $PPP_DATA.
func Compose(override string) ([]byte, error) {
	if override != "" {
		data, err := os.ReadFile(override)
		if err != nil {
			return nil, fmt.Errorf("catrust: reading PPP_UPSTREAM_CA %q: %w", override, err)
		}
		return data, nil
	}

	return osTrustStorePEM()
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
