// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package deploy

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// testSSHServer is a single-session in-process SSH server used to
// exercise DialSSH/Exec/WriteFile without touching the host network
// config. It handles the "session" channel type, a single "exec"
// request (returning configured output) and serves cat for WriteFile.
type testSSHServer struct {
	t        *testing.T
	listener net.Listener
	config   *ssh.ServerConfig
	hostKey  ssh.Signer
	execOut  map[string]string
	execErr  map[string]string
	execFail map[string]bool
	writes   map[string][]byte
	ran      []string
	mu       sync.Mutex
	wg       sync.WaitGroup

	// streaming, when set, takes over the exec response for that command: the
	// server writes continuously and never sends an exit status, so the
	// client's copier goroutines stay busy for as long as the test needs.
	streaming *streamingExec

	// forward maps a direct-tcpip target ("host:port", as the client asked
	// for it) to the local address the server actually dials, so a test can
	// stand a local listener in for a remote service the "node" reaches.
	forward map[string]string
}

// streamingExec keeps a command's output flowing so a test can act while the
// client copiers are demonstrably mid-copy. flowing closes after the first
// chunk reaches the wire; stop ends the stream.
type streamingExec struct {
	cmd     string
	flowing chan struct{}
	stop    chan struct{}
	once    sync.Once
}

func newTestSSHServer(t *testing.T, allowedUser, allowedPass string) *testSSHServer {
	return newTestSSHServerKex(t, allowedUser, allowedPass, nil)
}

// newTestSSHServerKex is newTestSSHServer with an explicit server-side
// key-exchange set. A nil kex leaves x/crypto/ssh's defaults in place; a
// single-element kex lets a test assert the negotiated algorithm by
// construction (the peer offers nothing else) — which is how the FIPS tests
// prove the client used an approved key exchange rather than merely that no
// error occurred (#111). extraHostKeys are served alongside the ed25519
// host key, so a test can present a host with several keys — as OpenSSH
// does by default — and assert which one the client negotiated.
func newTestSSHServerKex(t *testing.T, allowedUser, allowedPass string, kex []string, extraHostKeys ...ssh.Signer) *testSSHServer {
	t.Helper()
	srv := &testSSHServer{
		t:        t,
		execOut:  make(map[string]string),
		execErr:  make(map[string]string),
		execFail: make(map[string]bool),
		writes:   make(map[string][]byte),
		forward:  make(map[string]string),
	}
	srv.hostKey = generateTestHostKey(t)
	srv.config = &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			if conn.User() == allowedUser && string(pw) == allowedPass {
				return nil, nil
			}
			return nil, errors.New("invalid credentials")
		},
	}
	srv.config.KeyExchanges = kex
	srv.config.AddHostKey(srv.hostKey)
	for _, key := range extraHostKeys {
		srv.config.AddHostKey(key)
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv.listener = l
	srv.wg.Add(1)
	go srv.serve()
	t.Cleanup(func() {
		_ = l.Close()
		srv.wg.Wait()
	})
	return srv
}

func (s *testSSHServer) setExec(cmd, stdout, stderr string, fail bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.execOut[cmd] = stdout
	s.execErr[cmd] = stderr
	s.execFail[cmd] = fail
}

// ranCommands returns every command the server was asked to execute, in
// order. Tests that assert on sequencing (lingering must be enabled
// before the unit starts) read this rather than inferring order from
// side effects.
func (s *testSSHServer) ranCommands() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ran...)
}

// executed reports whether cmd was run at least once.
func (s *testSSHServer) executed(cmd string) bool {
	for _, got := range s.ranCommands() {
		if got == cmd {
			return true
		}
	}
	return false
}

func (s *testSSHServer) written(path string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.writes[path]
	return b, ok
}

func (s *testSSHServer) addr() string { return s.listener.Addr().String() }

func (s *testSSHServer) hostKeyAuthorized() string {
	return string(ssh.MarshalAuthorizedKey(s.hostKey.PublicKey()))
}

func (s *testSSHServer) serve() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.wg.Add(1)
		go s.handleConn(conn)
	}
}

func (s *testSSHServer) handleConn(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()
	_, chans, reqs, err := ssh.NewServerConn(conn, s.config)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for newCh := range chans {
		if newCh.ChannelType() == "direct-tcpip" {
			s.wg.Add(1)
			go s.handleDirectTCPIP(newCh)
			continue
		}
		if newCh.ChannelType() != "session" {
			_ = newCh.Reject(ssh.UnknownChannelType, "only session supported")
			continue
		}
		ch, reqs, err := newCh.Accept()
		if err != nil {
			return
		}
		go s.handleSession(ch, reqs)
	}
}

// setForward routes the node's direct-tcpip dials of target to local.
func (s *testSSHServer) setForward(target, local string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.forward[target] = local
}

// handleDirectTCPIP serves an RFC 4254 direct-tcpip channel: the client asks
// the "node" to open a TCP connection, and the server pipes the channel to the
// address setForward mapped the target to. An unmapped target is refused, as
// a node with no route would.
func (s *testSSHServer) handleDirectTCPIP(newCh ssh.NewChannel) {
	defer s.wg.Done()
	var req struct {
		Host       string
		Port       uint32
		OriginHost string
		OriginPort uint32
	}
	if err := ssh.Unmarshal(newCh.ExtraData(), &req); err != nil {
		_ = newCh.Reject(ssh.ConnectionFailed, "bad payload")
		return
	}
	target := net.JoinHostPort(req.Host, strconv.Itoa(int(req.Port)))
	s.mu.Lock()
	local, ok := s.forward[target]
	s.mu.Unlock()
	if !ok {
		_ = newCh.Reject(ssh.ConnectionFailed, "connection refused")
		return
	}
	upstream, err := net.Dial("tcp", local)
	if err != nil {
		_ = newCh.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	ch, reqs, err := newCh.Accept()
	if err != nil {
		_ = upstream.Close()
		return
	}
	go ssh.DiscardRequests(reqs)
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(upstream, ch); done <- struct{}{} }()
	go func() { _, _ = io.Copy(ch, upstream); done <- struct{}{} }()
	<-done
	_ = upstream.Close()
	_ = ch.Close()
	<-done
}

func (s *testSSHServer) handleSession(ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer ch.Close()
	for req := range reqs {
		switch req.Type {
		case "exec":
			cmd := parseExecPayload(req.Payload)
			_ = req.Reply(true, nil)
			s.runExec(ch, cmd)
			return
		default:
			_ = req.Reply(false, nil)
		}
	}
}

// testUnquotePath reverses the quoting that shellQuotePath applies to
// a remote path so the mock server keyspace is stable regardless of
// whether the client used plain single quotes or "$HOME"'...'.
func testUnquotePath(s string) string {
	s = strings.TrimSpace(s)
	// Normalize "$HOME" → $HOME, then strip all single quotes the
	// quoter emitted. The path never contains a literal single quote
	// in this test surface so a global strip is safe.
	s = strings.ReplaceAll(s, `"$HOME"`, "$HOME")
	s = strings.ReplaceAll(s, "'", "")
	return s
}

func (s *testSSHServer) runExec(ch ssh.Channel, cmd string) {
	s.mu.Lock()
	s.ran = append(s.ran, cmd)
	s.mu.Unlock()

	// Intercept cat redirection for WriteFile semantics.
	if strings.HasPrefix(cmd, "cat > ") {
		remotePath := testUnquotePath(strings.TrimSpace(strings.TrimPrefix(cmd, "cat > ")))
		buf, _ := io.ReadAll(ch)
		s.mu.Lock()
		s.writes[remotePath] = buf
		s.mu.Unlock()
		sendExit(ch, 0)
		return
	}
	// Intercept mkdir -p so paths using shellQuotePath's mixed quoting
	// (e.g. `mkdir -p "$HOME"'/.quicbaas/agent'`) are recorded.
	if strings.HasPrefix(cmd, "mkdir -p ") {
		s.mu.Lock()
		stdout, stdoutOK := s.execOut[cmd]
		stderr, stderrOK := s.execErr[cmd]
		fail, failOK := s.execFail[cmd]
		s.mu.Unlock()
		if !stdoutOK && !stderrOK && !failOK {
			// Default: succeed silently so test setups don't have to
			// register every mkdir invocation explicitly.
			sendExit(ch, 0)
			return
		}
		_ = stdout
		_ = stderr
		if fail {
			sendExit(ch, 1)
		} else {
			sendExit(ch, 0)
		}
		return
	}
	s.mu.Lock()
	streaming := s.streaming
	stdout := s.execOut[cmd]
	stderr := s.execErr[cmd]
	fail := s.execFail[cmd]
	s.mu.Unlock()

	if streaming != nil && streaming.cmd == cmd {
		chunk := []byte(strings.Repeat("o", 32<<10))
		errChunk := []byte(strings.Repeat("e", 4<<10))
		for {
			if _, err := ch.Write(chunk); err != nil {
				return
			}
			if _, err := ch.Stderr().Write(errChunk); err != nil {
				return
			}
			streaming.once.Do(func() { close(streaming.flowing) })
			select {
			case <-streaming.stop:
				sendExit(ch, 0)
				return
			default:
			}
		}
	}

	if stdout != "" {
		_, _ = io.WriteString(ch, stdout)
	}
	if stderr != "" {
		_, _ = io.WriteString(ch.Stderr(), stderr)
	}
	if fail {
		sendExit(ch, 1)
		return
	}
	sendExit(ch, 0)
}

func sendExit(ch ssh.Channel, code uint32) {
	_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{code}))
}

// parseExecPayload decodes the ssh RFC 4254 exec payload which is a
// single string prefixed by its length.
func parseExecPayload(payload []byte) string {
	if len(payload) < 4 {
		return ""
	}
	n := int(payload[0])<<24 | int(payload[1])<<16 | int(payload[2])<<8 | int(payload[3])
	if 4+n > len(payload) {
		return ""
	}
	return string(payload[4 : 4+n])
}

// generateTestHostKey returns an ephemeral ed25519 host key suitable
// for in-process SSH servers. No bits are persisted anywhere.
func generateTestHostKey(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	return signer
}

// -----------------------------------------------------------------------------
// Tests
// -----------------------------------------------------------------------------

func TestDialSSH_Success(t *testing.T) {
	srv := newTestSSHServer(t, "alice", "s3cret")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	spec := &Spec{
		Host:            srv.addr(),
		SSHUser:         "alice",
		SSHPassword:     "s3cret",
		InsecureHostKey: true,
	}
	client, err := DialSSH(ctx, spec)
	if err != nil {
		t.Fatalf("DialSSH: %v", err)
	}
	defer client.Close()
	if client.Host() == "" {
		t.Fatalf("Host() empty")
	}
	if client.UnderlyingClient() == nil {
		t.Fatalf("UnderlyingClient() nil")
	}
}

func TestDialSSH_InvalidSpec(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		spec *Spec
	}{
		{"nil", nil},
		{"no host", &Spec{SSHUser: "u", SSHPassword: "p", InsecureHostKey: true}},
		{"no user", &Spec{Host: "127.0.0.1", SSHPassword: "p", InsecureHostKey: true}},
		{"no auth", &Spec{Host: "127.0.0.1", SSHUser: "u", InsecureHostKey: true}},
		{"no host key", &Spec{Host: "127.0.0.1", SSHUser: "u", SSHPassword: "p"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DialSSH(ctx, tc.spec)
			if err == nil {
				t.Fatal("expected error")
			}
			var specErr *InvalidSpecError
			if !errors.As(err, &specErr) {
				t.Fatalf("want InvalidSpecError, got %T: %v", err, err)
			}
		})
	}
}

func TestDialSSH_AuthFailure(t *testing.T) {
	srv := newTestSSHServer(t, "alice", "s3cret")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	spec := &Spec{
		Host:            srv.addr(),
		SSHUser:         "alice",
		SSHPassword:     "wrong",
		InsecureHostKey: true,
	}
	_, err := DialSSH(ctx, spec)
	if err == nil {
		t.Fatal("expected auth failure")
	}
	var dialErr *SSHDialError
	if !errors.As(err, &dialErr) {
		t.Fatalf("want SSHDialError, got %T: %v", err, err)
	}
}

func TestExec_Success(t *testing.T) {
	srv := newTestSSHServer(t, "u", "p")
	srv.setExec("echo hi", "hi\n", "", false)

	ctx := context.Background()
	client, err := DialSSH(ctx, &Spec{
		Host: srv.addr(), SSHUser: "u", SSHPassword: "p", InsecureHostKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	stdout, _, err := client.Exec(ctx, "echo hi")
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if !strings.Contains(stdout, "hi") {
		t.Fatalf("stdout=%q", stdout)
	}
}

func TestExec_Failure(t *testing.T) {
	srv := newTestSSHServer(t, "u", "p")
	srv.setExec("fail", "", "boom", true)

	ctx := context.Background()
	client, err := DialSSH(ctx, &Spec{
		Host: srv.addr(), SSHUser: "u", SSHPassword: "p", InsecureHostKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	_, _, err = client.Exec(ctx, "fail")
	if err == nil {
		t.Fatal("expected failure")
	}
	var execErr *RemoteExecError
	if !errors.As(err, &execErr) {
		t.Fatalf("want RemoteExecError, got %T", err)
	}
	if execErr.Stderr != "boom" {
		t.Fatalf("stderr=%q", execErr.Stderr)
	}
}

func TestExec_ClosedClient(t *testing.T) {
	var c *SSHClient
	_, _, err := c.Exec(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error on nil client")
	}
	c = &SSHClient{}
	_, _, err = c.Exec(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error on empty client")
	}
}

func TestWriteFile_Roundtrip(t *testing.T) {
	srv := newTestSSHServer(t, "u", "p")
	// chmod is the second command; configure it to succeed.
	srv.setExec("chmod 644 '/tmp/out'", "", "", false)

	ctx := context.Background()
	client, err := DialSSH(ctx, &Spec{
		Host: srv.addr(), SSHUser: "u", SSHPassword: "p", InsecureHostKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	data := []byte("hello world")
	if err := client.WriteFile(ctx, "/tmp/out", data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, ok := srv.written("/tmp/out")
	if !ok {
		t.Fatal("expected file to be written")
	}
	if string(got) != string(data) {
		t.Fatalf("payload mismatch: %q", got)
	}
}

func TestHostKeyCallbackPinning(t *testing.T) {
	srv := newTestSSHServer(t, "u", "p")

	// Wrong pinned key → dial must fail. Post-Phase-2a the deploy.Spec
	// no longer carries SSHHostKey; pinning is now an explicit policy
	// injected via DialSSHWithAuthAndHostKey.
	ctx := context.Background()
	otherKey := generateTestHostKey(t)
	_, err := DialSSHWithAuthAndHostKey(ctx, &Spec{
		Host:    srv.addr(),
		SSHUser: "u",
	}, []ssh.AuthMethod{ssh.Password("p")}, &HostKeyPolicy{Callback: ssh.FixedHostKey(otherKey.PublicKey())})
	if err == nil {
		t.Fatal("expected host key mismatch error")
	}
	var dialErr *SSHDialError
	if !errors.As(err, &dialErr) {
		t.Fatalf("want SSHDialError, got %T: %v", err, err)
	}
}

func TestInsecureHostKeyAllowed(t *testing.T) {
	srv := newTestSSHServer(t, "u", "p")
	ctx := context.Background()
	c, err := DialSSH(ctx, &Spec{
		Host: srv.addr(), SSHUser: "u", SSHPassword: "p", InsecureHostKey: true,
	})
	if err != nil {
		t.Fatalf("insecure dial should work: %v", err)
	}
	_ = c.Close()
}

// TestAuthWithInjectedSigner exercises the supported key-auth path
// post-Phase-2a: DialSSHWithAuth accepts a caller-built ssh.Signer.
// The legacy --ssh-key file path was removed; the xkms SSH SDK is
// the only source of signers in production. Tests mint an ed25519
// signer in-process to drive the server handshake.
func TestAuthWithInjectedSigner(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}

	srv := &testSSHServer{
		t:        t,
		execOut:  make(map[string]string),
		execErr:  make(map[string]string),
		execFail: make(map[string]bool),
		writes:   make(map[string][]byte),
	}
	srv.hostKey = generateTestHostKey(t)
	pubKey := signer.PublicKey()
	srv.config = &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if string(key.Marshal()) == string(pubKey.Marshal()) {
				return nil, nil
			}
			return nil, errors.New("unknown key")
		},
	}
	srv.config.AddHostKey(srv.hostKey)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv.listener = l
	srv.wg.Add(1)
	go srv.serve()
	defer func() { _ = l.Close(); srv.wg.Wait() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := DialSSHWithAuth(ctx, &Spec{
		Host: srv.addr(), SSHUser: "k", InsecureHostKey: true,
	}, []ssh.AuthMethod{ssh.PublicKeys(signer)})
	if err != nil {
		t.Fatalf("DialSSHWithAuth: %v", err)
	}
	_ = c.Close()
}

func TestModeOctal(t *testing.T) {
	cases := []struct {
		mode os.FileMode
		want string
	}{
		{0o755, "755"},
		{0o644, "644"},
		{0o600, "600"},
		{0o777, "777"},
	}
	for _, tc := range cases {
		if got := modeOctal(tc.mode); got != tc.want {
			t.Fatalf("modeOctal(%o)=%q want %q", tc.mode, got, tc.want)
		}
	}
}

func TestNormalizeHostPort(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"host", "host:22"},
		{"host:2222", "host:2222"},
		{"127.0.0.1", "127.0.0.1:22"},
		{"[::1]:22", "[::1]:22"},
	}
	for _, tc := range cases {
		if got := normalizeHostPort(tc.in); got != tc.want {
			t.Fatalf("normalizeHostPort(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestShellQuote(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", `'plain'`},
		{"has'quote", `'has'\''quote'`},
		{"", `''`},
	}
	for _, tc := range cases {
		if got := shellQuote(tc.in); got != tc.want {
			t.Fatalf("shellQuote(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

// =============================================================================
// A cancelled command's capture is read while it is still being written
// =============================================================================

func TestSyncBuffer_MayBeReadWhileStillBeingWritten(t *testing.T) {
	// THE RACE THIS TYPE EXISTS FOR, isolated so it is caught on purpose rather
	// than by luck.
	//
	// ssh.Session copies the remote's output into Exec's buffers on goroutines
	// started by Run, and Exec's cancel path reads those buffers WITHOUT
	// waiting for Run to return — so a plain bytes.Buffer is read concurrently
	// with a write. On the CI runner that surfaced as four DATA RACE reports
	// (one per buffer field); on a developer machine it did not reproduce in
	// fifty runs, because it needs the scheduler to interleave a read into a
	// write that is already in progress.
	//
	// This test does not depend on that luck. The race detector reports
	// unsynchronized concurrent access whenever it OBSERVES it, and a reader
	// and a writer both hammering the same buffer for a thousand operations
	// gives it nothing to miss. Swap syncBuffer for bytes.Buffer here and
	// `go test -race` fails every time.
	var b syncBuffer

	const ops = 1000
	written := make(chan struct{})
	go func() {
		defer close(written)
		for i := 0; i < ops; i++ {
			if _, err := b.Write([]byte("output from a command still running\n")); err != nil {
				t.Errorf("Write: %v", err)
				return
			}
		}
	}()

	for i := 0; i < ops; i++ {
		_ = b.String() // the cancel path's read, racing the copier's write
	}
	<-written

	// AND IT IS STILL A BUFFER: locking must not cost the contents.
	if got := b.String(); len(got) != ops*len("output from a command still running\n") {
		t.Errorf("captured %d bytes, want every write to have landed", len(got))
	}
}

func TestExec_ACancelledCommandReturnsItsPartialCaptureSafely(t *testing.T) {
	// The property at the level the defect lives: a cancelled Exec reads the
	// buffers while the session's copiers may still be filling them, and that
	// read must be safe AND must still answer with a typed error rather than
	// dropping the capture on the floor.
	//
	// Registering a large stdout is what keeps a copier actually busy at the
	// moment the cancel branch reads — a command whose output has already been
	// fully copied cannot exercise the window at all.
	srv := newTestSSHServer(t, "u", "p")
	srv.setExec("noisy", strings.Repeat("a line of remote output\n", 4096), "", false)

	c, err := DialSSH(context.Background(), &Spec{
		Host: srv.addr(), SSHUser: "u", SSHPassword: "p", InsecureHostKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	stdout, stderr, err := c.Exec(ctx, "noisy")
	if err != nil {
		// The cancel branch won: the capture is partial, and reading it is the
		// thing that used to race.
		var execErr *RemoteExecError
		if !errors.As(err, &execErr) {
			t.Fatalf("err = %T, want *RemoteExecError", err)
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want it to carry context.Canceled", err)
		}
		return
	}
	// The done branch won, which is equally legitimate — the command simply
	// finished first. Then the capture must be COMPLETE, not partial.
	if !strings.HasSuffix(stdout, "a line of remote output\n") {
		t.Errorf("uncancelled run returned a truncated capture (%d bytes)", len(stdout))
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}
