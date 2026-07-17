package secret

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// maxRequestBytes bounds a single request line so a client cannot exhaust the
// parent's memory with an unterminated or giant line. The wire protocol is a
// tiny JSON object; a few kilobytes is generous. Anything larger is rejected as
// a bad request (SI-10: input validation / fail-closed).
const maxRequestBytes = 64 * 1024

// request is one newline-delimited JSON message from the addon. All three
// fields are accepted; dispatch is by which are set (see handleLine). It is
// untrusted input crossing the addon↔parent boundary and is validated before
// use.
type request struct {
	Service string `json:"service"`
	Sandbox string `json:"sandbox"`
	Host    string `json:"host"`
}

// response is one newline-delimited JSON reply. Fields are omitted when empty
// so each documented shape is exact on the wire:
//
//	hit (service): {"ok":true,"header":"x-api-key","value":"..."}
//	custom:        {"ok":true,"substitutions":[{"placeholder":"...","value":"..."}]}
//	miss:          {"ok":false,"reason":"no-secret"}
//	locked:        {"ok":false,"reason":"locked"}
//	bad request:   {"ok":false,"reason":"bad-request"}
//	internal err:  {"ok":false,"reason":"internal"}
type response struct {
	OK            bool           `json:"ok"`
	Reason        string         `json:"reason,omitempty"`
	Header        string         `json:"header,omitempty"`
	Value         string         `json:"value,omitempty"`
	Substitutions []Substitution `json:"substitutions,omitempty"`
}

// Response reason codes, named once so server and any consumer stay in sync.
const (
	reasonNoSecret   = "no-secret"
	reasonLocked     = "locked"
	reasonBadRequest = "bad-request"
	reasonInternal   = "internal"
)

// Server is the addon↔parent secret-lookup IPC endpoint. It listens on a Unix
// domain socket (mode 0600, same-user only) and answers newline-delimited JSON
// requests by delegating to a Resolver. It holds no secret material itself; a
// value lives in memory only for the duration of one response.
//
// The socket path is injected (not derived) so the core server is testable
// against a temp dir; CLI wiring (T12) is expected to pass
// $PPP_DATA/secret.sock via DefaultSocketPath. One request/response per
// connection: read a line, reply, close.
type Server struct {
	resolver   *Resolver
	socketPath string

	mu       sync.Mutex
	listener net.Listener
	closed   bool
	wg       sync.WaitGroup
}

// NewServer builds a Server that resolves via r and listens at socketPath. It
// does not bind the socket; call Start.
func NewServer(r *Resolver, socketPath string) *Server {
	return &Server{resolver: r, socketPath: socketPath}
}

// Start binds the Unix socket, sets its mode to 0600, and launches the accept
// loop in the background. It returns once the listener is bound (so tests may
// connect immediately) or with an error if binding fails.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return errors.New("secret: server already started")
	}

	// Ensure the socket's parent directory exists and is private (0700) BEFORE
	// binding. This closes the window between net.Listen (which creates the
	// socket at the process umask) and the os.Chmod below: even for that brief
	// moment the socket is unreachable to other local users because its
	// directory is not traversable by them (code review S1).
	if dir := filepath.Dir(s.socketPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("secret: create socket dir %q: %w", dir, err)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("secret: chmod socket dir %q: %w", dir, err)
		}
	}

	// Belt-and-suspenders: also tighten the umask so the socket inode itself is
	// created without group/other bits, making the subsequent Chmod atomic in
	// effect rather than a widen-then-narrow.
	oldMask := syscall.Umask(0o177)
	ln, err := net.Listen("unix", s.socketPath)
	syscall.Umask(oldMask)
	if err != nil {
		return fmt.Errorf("secret: listen on %q: %w", s.socketPath, err)
	}
	if err := os.Chmod(s.socketPath, 0o600); err != nil {
		_ = ln.Close()
		_ = os.Remove(s.socketPath)
		return fmt.Errorf("secret: chmod socket %q: %w", s.socketPath, err)
	}

	s.listener = ln
	s.wg.Add(1)
	go s.acceptLoop(ln)
	return nil
}

// Stop closes the listener, waits for in-flight connections to finish, and
// removes the socket file. It is safe to call more than once.
func (s *Server) Stop() error {
	s.mu.Lock()
	if s.closed || s.listener == nil {
		s.closed = true
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	ln := s.listener
	s.mu.Unlock()

	err := ln.Close()
	s.wg.Wait()
	if rmErr := os.Remove(s.socketPath); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
		if err == nil {
			err = fmt.Errorf("secret: remove socket %q: %w", s.socketPath, rmErr)
		}
	}
	return err
}

// acceptLoop accepts connections until the listener is closed, handling each in
// its own goroutine so one bad or slow client cannot block or crash the loop.
func (s *Server) acceptLoop(ln net.Listener) {
	defer s.wg.Done()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if s.isClosed() {
				return
			}
			continue // transient accept error; keep serving
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConn(conn)
		}()
	}
}

func (s *Server) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// handleConn processes exactly one request/response on conn, then closes it. It
// never panics: any read/parse/resolve failure becomes a fail-closed response.
func (s *Server) handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	line, err := readLine(conn, maxRequestBytes)
	if err != nil {
		// Client disconnected or sent an oversized/unterminated line. If we
		// read nothing, there is nobody to answer; otherwise reply bad-request.
		if len(line) == 0 {
			return
		}
		writeResponse(conn, response{OK: false, Reason: reasonBadRequest})
		return
	}
	writeResponse(conn, s.handleLine(line))
}

// handleLine parses one request line and resolves it. Dispatch: a non-empty
// service is a service lookup; otherwise a non-empty host is a custom lookup.
// A request with neither is a bad request. This keeps the two response families
// (injection vs substitutions) cleanly separated and each independently tested.
func (s *Server) handleLine(line []byte) response {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return response{OK: false, Reason: reasonBadRequest}
	}
	switch {
	case req.Service != "":
		return s.resolveService(req.Service, req.Sandbox)
	case req.Host != "":
		return s.resolveCustom(req.Host)
	default:
		return response{OK: false, Reason: reasonBadRequest}
	}
}

// resolveService maps a Resolver.Resolve outcome onto the wire response.
func (s *Server) resolveService(service, sandbox string) response {
	inj, found, err := s.resolver.Resolve(service, sandbox)
	switch {
	case err != nil && errors.Is(err, ErrLocked):
		return response{OK: false, Reason: reasonLocked}
	case err != nil:
		return response{OK: false, Reason: reasonInternal}
	case !found:
		return response{OK: false, Reason: reasonNoSecret}
	default:
		return response{OK: true, Header: inj.Header, Value: inj.Value}
	}
}

// resolveCustom maps a Resolver.ResolveCustom outcome onto the wire response. A
// valid host that matches nothing is a successful, empty result (ok=true, no
// substitutions) — distinct from a bad request.
func (s *Server) resolveCustom(host string) response {
	subs, err := s.resolver.ResolveCustom(host)
	if err != nil {
		// The only ResolveCustom error is an empty host, which handleLine
		// already excludes; treat any residual error as bad-request.
		return response{OK: false, Reason: reasonBadRequest}
	}
	return response{OK: true, Substitutions: subs}
}

// readLine reads a single '\n'-terminated line, bounded to max bytes. It
// returns an error if no newline is seen within max bytes (fail-closed against
// giant/unterminated input) or on read failure. The returned slice excludes the
// newline.
func readLine(r io.Reader, max int) ([]byte, error) {
	br := bufio.NewReaderSize(r, 4096)
	buf := make([]byte, 0, 512)
	for {
		b, err := br.ReadByte()
		if err != nil {
			return buf, err
		}
		if b == '\n' {
			return buf, nil
		}
		if len(buf) >= max {
			return buf, fmt.Errorf("secret: request exceeds %d bytes", max)
		}
		buf = append(buf, b)
	}
}

// writeResponse encodes resp as one newline-delimited JSON line to w. Encoding
// a fixed struct cannot fail in practice; any write error is dropped because
// the connection is being closed regardless (no secret is logged).
func writeResponse(w io.Writer, resp response) {
	b, err := json.Marshal(resp)
	if err != nil {
		b = []byte(`{"ok":false,"reason":"internal"}`)
	}
	b = append(b, '\n')
	_, _ = w.Write(b)
}
