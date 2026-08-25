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
	mu       sync.Mutex
	wg       sync.WaitGroup

	// streaming, when set, takes over the exec response for that command: the
	// server writes continuously and never sends an exit status, so the
	// client's copier goroutines stay busy for as long as the test needs.
	streaming *streamingExec
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
// error occurred (#111).
func newTestSSHServerKex(t *testing.T, allowedUser, allowedPass string, kex []string) *testSSHServer {
	t.Helper()
	srv := &testSSHServer{
		t:        t,
		execOut:  make(map[string]string),
		execErr:  make(map[string]string),
		execFail: make(map[string]bool),
		writes:   make(map[string][]byte),
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
	// no longer carries SSHHostKey; pinning is now an explicit callback
	// injected via DialSSHWithAuthAndHostKey.
	ctx := context.Background()
	otherKey := generateTestHostKey(t)
	_, err := DialSSHWithAuthAndHostKey(ctx, &Spec{
		Host:    srv.addr(),
		SSHUser: "u",
	}, []ssh.AuthMethod{ssh.Password("p")}, ssh.FixedHostKey(otherKey.PublicKey()))
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

// TestExec_CancellationReadsOutputWhileCopiersAreStillRunning is the
// end-to-end half of the fix: it proves SSHClient.Exec's cancellation path is
// safe against the live ssh copier goroutines, not merely that syncBuffer is
// safe in isolation.
//
// The server streams without ever sending an exit status, so Session.Run never
// returns and the stdout/stderr copiers keep writing for the whole test. The
// flowing signal comes from the SERVER after the first chunk is on the wire --
// deliberately not from the client copier, because a signal out of the copier
// goroutine would establish a happens-before edge to the reader and the race
// detector would then correctly conclude there is no race. Signalling from the
// far end orders nothing on the client side, and because the stream continues,
// the copiers are demonstrably mid-copy when Exec takes its cancellation
// branch and reads what they have produced.
//
// Under -race this fails if Exec's sinks are unsynchronized.
func TestExec_CancellationReadsOutputWhileCopiersAreStillRunning(t *testing.T) {
	srv := newTestSSHServer(t, "u", "p")

	stream := &streamingExec{
		cmd:     "noisy",
		flowing: make(chan struct{}),
		stop:    make(chan struct{}),
	}
	srv.mu.Lock()
	srv.streaming = stream
	srv.mu.Unlock()
	t.Cleanup(func() { close(stream.stop) })

	c, err := DialSSH(context.Background(), &Spec{
		Host: srv.addr(), SSHUser: "u", SSHPassword: "p", InsecureHostKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	execDone := make(chan error, 1)
	go func() {
		_, _, execErr := c.Exec(ctx, "noisy")
		execDone <- execErr
	}()

	// Output is on the wire, so the copiers are running and will keep running:
	// the command never ends on its own.
	select {
	case <-stream.flowing:
	case <-time.After(30 * time.Second):
		t.Fatal("the server never started streaming; the copiers were never running")
	}

	cancel()

	select {
	case execErr := <-execDone:
		if execErr == nil {
			t.Fatal("Exec returned no error for a cancelled command")
		}
		var remoteErr *RemoteExecError
		if !errors.As(execErr, &remoteErr) {
			t.Fatalf("want RemoteExecError, got %T", execErr)
		}
		if !errors.Is(execErr, context.Canceled) {
			t.Fatalf("want a context.Canceled cause, got %v", execErr)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Exec did not return after its context was cancelled")
	}
}

// TestSyncBuffer_ReadableWhileBeingWritten is the deterministic half of the
// cancellation-race fix. The end-to-end test above depends on the ssh copiers
// and the race detector overlapping, which is a bet; this one drives the
// concurrent access directly.
//
// It also pins the snapshot property: String must never observe a torn write,
// so every length it reports must be a whole number of chunks. The reader is
// bounded by the writers finishing rather than by reaching an expected length,
// because an unguarded buffer does not reliably reach one -- it corrupts.
func TestSyncBuffer_ReadableWhileBeingWritten(t *testing.T) {
	const (
		writers = 4
		rounds  = 500
		chunk   = "0123456789abcdef"
	)

	var b syncBuffer

	stop := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			select {
			case <-stop:
				return
			default:
			}
			// Exec's cancellation branch reads exactly like this, while the
			// ssh copiers are still writing.
			if n := len(b.String()); n%len(chunk) != 0 {
				t.Errorf("String observed a torn write: length %d is not a multiple of %d", n, len(chunk))
				return
			}
		}
	}()

	var wg sync.WaitGroup
	wg.Add(writers)
	for range writers {
		go func() {
			defer wg.Done()
			for range rounds {
				if _, err := b.Write([]byte(chunk)); err != nil {
					t.Errorf("Write: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(stop)
	<-readerDone

	if got, want := len(b.String()), writers*rounds*len(chunk); got != want {
		t.Errorf("final length = %d, want %d", got, want)
	}
}
