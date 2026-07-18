package catrust

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"
)

// makeCA builds a self-signed CA cert PEM with BasicConstraints marked critical
// or not, per the argument, so we can assert the filter's behavior.
func makeCA(t *testing.T, cn string, bcCritical bool) string {
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
	// x509.CreateCertificate always marks BasicConstraints critical when IsCA is
	// set, so to simulate a non-critical one we post-process the DER: easier to
	// just build both and rely on the parser. For the non-critical case we craft
	// the extension manually.
	if !bcCritical {
		tmpl.BasicConstraintsValid = false
		tmpl.ExtraExtensions = []pkix.Extension{{
			Id:       []int{2, 5, 29, 19}, // basicConstraints
			Critical: false,
			Value:    []byte{0x30, 0x03, 0x01, 0x01, 0xff}, // SEQUENCE { BOOLEAN TRUE } => CA:TRUE
		}}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func TestFilterDropsNonCriticalBasicConstraintsCA(t *testing.T) {
	good := makeCA(t, "Good Critical Root", true)
	bad := makeCA(t, "Bad NonCritical Root", false)

	filtered := string(filterOpenSSLUsable([]byte(good + bad)))

	if !strings.Contains(filtered, "CERTIFICATE") {
		t.Fatal("filtered bundle unexpectedly empty")
	}
	// The good (critical BC) cert survives; the bad (non-critical BC) is dropped.
	if countCerts(t, filtered) != 1 {
		t.Fatalf("expected exactly 1 surviving cert, got %d", countCerts(t, filtered))
	}
	// Verify the survivor is the critical one by CN.
	block, _ := pem.Decode([]byte(filtered))
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Subject.CommonName != "Good Critical Root" {
		t.Errorf("wrong survivor: %q", cert.Subject.CommonName)
	}
}

func TestFilterKeepsCertWithoutBasicConstraints(t *testing.T) {
	// A cert with no BasicConstraints at all (e.g. a leaf) must be kept.
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "leaf.example"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	leaf := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))

	if countCerts(t, string(filterOpenSSLUsable([]byte(leaf)))) != 1 {
		t.Error("leaf cert (no BasicConstraints) should be kept")
	}
}

func TestComposeOverrideReturnsFileVerbatim(t *testing.T) {
	dir := t.TempDir()
	f := dir + "/override.pem"
	content := makeCA(t, "Override Root", true)
	if err := writeFile(f, content); err != nil {
		t.Fatal(err)
	}
	out, err := Compose(f, nil)
	if err != nil {
		t.Fatalf("Compose(override): %v", err)
	}
	if string(out) != content {
		t.Error("override bundle should be returned verbatim")
	}
}

func countCerts(t *testing.T, pemStr string) int {
	t.Helper()
	n := 0
	rest := []byte(pemStr)
	for {
		var b *pem.Block
		b, rest = pem.Decode(rest)
		if b == nil {
			break
		}
		if b.Type == "CERTIFICATE" {
			n++
		}
	}
	return n
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
