package secret

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// startTestServer builds a Resolver over the given fake store, starts a Server
// on a socket inside a temp dir, and returns the server and its socket path.
// It registers cleanup so the listener is closed at test end.
func startTestServer(t *testing.T, fs *fakeStore, customs []CustomSecret) (*Server, string) {
	t.Helper()
	r := NewResolver(fs)
	if customs != nil {
		r.SetCustom(customs)
	}
	sockPath := filepath.Join(t.TempDir(), "secret.sock")
	srv := NewServer(r, sockPath)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	return srv, sockPath
}

// roundTrip connects to the socket, writes one request line, reads one response
// line, and returns the raw response bytes (without the trailing newline).
func roundTrip(t *testing.T, sockPath, request string) []byte {
	t.Helper()
	conn, err := net.DialTimeout("unix", sockPath, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write([]byte(request + "\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil && line == "" {
		t.Fatalf("read: %v", err)
	}
	return []byte(strings.TrimRight(line, "\n"))
}

// resp mirrors the wire response for assertion in tests.
type resp struct {
	OK            bool           `json:"ok"`
	Reason        string         `json:"reason,omitempty"`
	Header        string         `json:"header,omitempty"`
	Value         string         `json:"value,omitempty"`
	Substitutions []Substitution `json:"substitutions,omitempty"`
}

func decode(t *testing.T, b []byte) resp {
	t.Helper()
	var r resp
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("decode response %q: %v", string(b), err)
	}
	return r
}

func TestServer_ServiceHit(t *testing.T) {
	fs := newFakeStore()
	fs.set("ppp.anthropic", "FAKE-KEY-123")
	_, sock := startTestServer(t, fs, nil)

	got := decode(t, roundTrip(t, sock, `{"service":"anthropic","sandbox":"","host":""}`))
	if !got.OK {
		t.Fatalf("expected ok=true, got %+v", got)
	}
	if got.Header != "x-api-key" || got.Value != "FAKE-KEY-123" {
		t.Errorf("wrong injection: header=%q value=%q", got.Header, got.Value)
	}
}

func TestServer_ServiceMiss(t *testing.T) {
	_, sock := startTestServer(t, newFakeStore(), nil)

	got := decode(t, roundTrip(t, sock, `{"service":"anthropic","sandbox":"nope","host":""}`))
	if got.OK || got.Reason != "no-secret" {
		t.Errorf("expected ok=false reason=no-secret, got %+v", got)
	}
}

func TestServer_Locked(t *testing.T) {
	fs := newFakeStore()
	fs.set("ppp.anthropic", "FAKE-KEY-123")
	fs.locked = true
	_, sock := startTestServer(t, fs, nil)

	got := decode(t, roundTrip(t, sock, `{"service":"anthropic","sandbox":"","host":""}`))
	if got.OK || got.Reason != "locked" {
		t.Errorf("expected ok=false reason=locked, got %+v", got)
	}
}

func TestServer_Custom(t *testing.T) {
	customs := []CustomSecret{
		{Name: "jira", Placeholder: "__PPP_JIRA__", Value: "FAKE-JIRA-TOKEN", Hosts: []string{"jira.example.com"}},
	}
	_, sock := startTestServer(t, newFakeStore(), customs)

	got := decode(t, roundTrip(t, sock, `{"service":"","sandbox":"","host":"jira.example.com"}`))
	if !got.OK {
		t.Fatalf("expected ok=true, got %+v", got)
	}
	if len(got.Substitutions) != 1 {
		t.Fatalf("expected 1 substitution, got %d: %+v", len(got.Substitutions), got.Substitutions)
	}
	if got.Substitutions[0].Placeholder != "__PPP_JIRA__" || got.Substitutions[0].Value != "FAKE-JIRA-TOKEN" {
		t.Errorf("wrong substitution: %+v", got.Substitutions[0])
	}
}

func TestServer_CustomNoMatchIsOKEmpty(t *testing.T) {
	customs := []CustomSecret{
		{Name: "jira", Placeholder: "__PPP_JIRA__", Value: "FAKE-JIRA-TOKEN", Hosts: []string{"jira.example.com"}},
	}
	_, sock := startTestServer(t, newFakeStore(), customs)

	got := decode(t, roundTrip(t, sock, `{"service":"","sandbox":"","host":"nope.example.com"}`))
	if !got.OK {
		t.Fatalf("expected ok=true for empty-but-valid custom lookup, got %+v", got)
	}
	if len(got.Substitutions) != 0 {
		t.Errorf("expected zero substitutions, got %+v", got.Substitutions)
	}
}

func TestServer_MalformedJSON(t *testing.T) {
	_, sock := startTestServer(t, newFakeStore(), nil)

	got := decode(t, roundTrip(t, sock, `this is not json`))
	if got.OK || got.Reason != "bad-request" {
		t.Errorf("expected ok=false reason=bad-request, got %+v", got)
	}
}

func TestServer_MissingFieldsIsBadRequest(t *testing.T) {
	_, sock := startTestServer(t, newFakeStore(), nil)

	// Neither service nor host provided: nothing to resolve.
	got := decode(t, roundTrip(t, sock, `{"service":"","sandbox":"sb","host":""}`))
	if got.OK || got.Reason != "bad-request" {
		t.Errorf("expected ok=false reason=bad-request, got %+v", got)
	}
}

func TestServer_StaysUpAfterMalformed(t *testing.T) {
	fs := newFakeStore()
	fs.set("ppp.anthropic", "FAKE-KEY-123")
	_, sock := startTestServer(t, fs, nil)

	// A bad connection must not take down the listener.
	_ = roundTrip(t, sock, `garbage{`)

	got := decode(t, roundTrip(t, sock, `{"service":"anthropic","sandbox":"","host":""}`))
	if !got.OK || got.Value != "FAKE-KEY-123" {
		t.Errorf("server should still serve after a malformed request, got %+v", got)
	}
}

func TestServer_ClientDisconnectMidRequest(t *testing.T) {
	fs := newFakeStore()
	fs.set("ppp.anthropic", "FAKE-KEY-123")
	_, sock := startTestServer(t, fs, nil)

	// Connect, write a partial line with no newline, then close abruptly.
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_, _ = conn.Write([]byte(`{"service":"anthr`))
	_ = conn.Close()

	// Server must still answer a subsequent valid request.
	got := decode(t, roundTrip(t, sock, `{"service":"anthropic","sandbox":"","host":""}`))
	if !got.OK {
		t.Errorf("server should survive a mid-request disconnect, got %+v", got)
	}
}

func TestServer_OversizedLineRejected(t *testing.T) {
	fs := newFakeStore()
	fs.set("ppp.anthropic", "FAKE-KEY-123")
	_, sock := startTestServer(t, fs, nil)

	// A line far larger than the bound must not OOM or hang; the server
	// rejects it as bad-request (or closes), and stays up for the next client.
	big := strings.Repeat("A", maxRequestBytes+4096)
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	_, _ = conn.Write([]byte(`{"host":"` + big + `"}` + "\n"))
	// Read whatever the server returns (a bad-request line or EOF); either is fine.
	line, _ := bufio.NewReader(conn).ReadString('\n')
	_ = conn.Close()
	if line != "" {
		got := decode(t, []byte(strings.TrimRight(line, "\n")))
		if got.OK {
			t.Errorf("oversized request must not be ok, got %+v", got)
		}
	}

	// Listener still alive.
	got := decode(t, roundTrip(t, sock, `{"service":"anthropic","sandbox":"","host":""}`))
	if !got.OK {
		t.Errorf("server should survive an oversized request, got %+v", got)
	}
}

func TestServer_SocketPerms0600(t *testing.T) {
	_, sock := startTestServer(t, newFakeStore(), nil)

	info, err := os.Stat(sock)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("socket mode: got %o want 600", perm)
	}
}

func TestServer_StopRemovesSocket(t *testing.T) {
	srv, sock := startTestServer(t, newFakeStore(), nil)
	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if _, err := os.Stat(sock); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected socket removed after Stop, stat err=%v", err)
	}
}

func TestServer_StartTwiceOnSamePathFails(t *testing.T) {
	fs := newFakeStore()
	_, sock := startTestServer(t, fs, nil)

	srv2 := NewServer(NewResolver(newFakeStore()), sock)
	if err := srv2.Start(); err == nil {
		_ = srv2.Stop()
		t.Error("expected Start to fail when the socket path is already bound")
	}
}
