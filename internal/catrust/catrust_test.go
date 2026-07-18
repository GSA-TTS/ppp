package catrust

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"testing"
	"time"
)

// makeCA builds a self-signed CA cert PEM (used to build override fixtures).
func makeCA(t *testing.T, cn string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func TestComposeOverrideReturnsFileVerbatim(t *testing.T) {
	dir := t.TempDir()
	f := dir + "/override.pem"
	content := makeCA(t, "Override Root")
	if err := os.WriteFile(f, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := Compose(f)
	if err != nil {
		t.Fatalf("Compose(override): %v", err)
	}
	if string(out) != content {
		t.Error("override bundle should be returned verbatim")
	}
}

func TestComposeOverrideMissingFileErrors(t *testing.T) {
	if _, err := Compose("/no/such/upstream-ca.pem"); err == nil {
		t.Error("expected an error for a missing PPP_UPSTREAM_CA override file")
	}
}

// TestComposeDefaultReturnsHostStore exercises the default (no-override) path:
// it must return a non-empty PEM containing at least one certificate. This runs
// against the real host trust store, so it asserts only the shape, not contents.
func TestComposeDefaultReturnsHostStore(t *testing.T) {
	out, err := Compose("")
	if err != nil {
		t.Skipf("no host trust store available in this environment: %v", err)
	}
	if len(out) == 0 || !containsCert(out) {
		t.Errorf("expected a non-empty PEM bundle with >=1 certificate, got %d bytes", len(out))
	}
}

func containsCert(pemBytes []byte) bool {
	rest := pemBytes
	for {
		var b *pem.Block
		b, rest = pem.Decode(rest)
		if b == nil {
			return false
		}
		if b.Type == "CERTIFICATE" {
			return true
		}
	}
}
