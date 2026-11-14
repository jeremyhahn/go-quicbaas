// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package ssh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	stderrors "errors"
	"net"
	"strconv"
	"sync"
	"testing"

	gossh "golang.org/x/crypto/ssh"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// dialTransport plays the QuicBaaS server for Session.Dial: it holds the
// user's private key, signs digests, and serves the host-key pins. It records
// every method so the tests can prove the SSH stream never crosses it.
type dialTransport struct {
	transport.Transport

	user gossh.Signer
	pins []*Pin

	mu        sync.Mutex
	methods   []string
	pinScopes []ListKnownHostsRequest
}

func (d *dialTransport) Request(_ context.Context, _, method string, req interface{}) (interface{}, error) {
	d.mu.Lock()
	d.methods = append(d.methods, method)
	d.mu.Unlock()
	switch method {
	case MethodExportPublicKey:
		return &PublicKey{Line: string(gossh.MarshalAuthorizedKey(d.user.PublicKey()))}, nil
	case MethodSignDigest:
		sig, err := d.user.Sign(rand.Reader, req.(*SignRequest).Digest)
		if err != nil {
			return nil, err
		}
		return &SignResponse{Format: sig.Format, Blob: sig.Blob, Rest: sig.Rest}, nil
	case MethodListKnownHosts:
		d.mu.Lock()
		d.pinScopes = append(d.pinScopes, *req.(*ListKnownHostsRequest))
		d.mu.Unlock()
		return &ListKnownHostsResponse{Items: d.pins}, nil
	}
	return nil, errors.NewUnimplemented(method, "not served by the test transport")
}

func (d *dialTransport) seen(method string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, m := range d.methods {
		if m == method {
			return true
		}
	}
	return false
}

func newSigner(t *testing.T) gossh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// startSSHServer runs an SSH server on loopback that accepts only userPub
// and answers every exec request with "ran: <command>".
func startSSHServer(t *testing.T, hostKey gossh.Signer, userPub gossh.PublicKey) int {
	t.Helper()
	cfg := &gossh.ServerConfig{
		PublicKeyCallback: func(_ gossh.ConnMetadata, k gossh.PublicKey) (*gossh.Permissions, error) {
			if string(k.Marshal()) == string(userPub.Marshal()) {
				return nil, nil
			}
			return nil, stderrors.New("unknown key")
		},
	}
	cfg.AddHostKey(hostKey)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go serveSSHConn(c, cfg)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func serveSSHConn(c net.Conn, cfg *gossh.ServerConfig) {
	sconn, chans, reqs, err := gossh.NewServerConn(c, cfg)
	if err != nil {
		_ = c.Close()
		return
	}
	defer func() { _ = sconn.Close() }()
	go gossh.DiscardRequests(reqs)
	for nc := range chans {
		ch, creqs, err := nc.Accept()
		if err != nil {
			return
		}
		go func() {
			defer func() { _ = ch.Close() }()
			for r := range creqs {
				if r.Type != "exec" {
					_ = r.Reply(false, nil)
					continue
				}
				var p struct{ Command string }
				_ = gossh.Unmarshal(r.Payload, &p)
				_ = r.Reply(true, nil)
				_, _ = ch.Write([]byte("ran: " + p.Command))
				_, _ = ch.SendRequest("exit-status", false, gossh.Marshal(struct{ Status uint32 }{0}))
				return
			}
		}()
	}
}

func pinFor(host string, key gossh.PublicKey) *Pin {
	return &Pin{Host: host, Fingerprint: gossh.FingerprintSHA256(key)}
}

// TestSessionDialRunsWithServerHeldKey proves Dial opens a working session:
// the key stays behind the transport (only digests are signed there), the
// host key is checked against the server's pins of the key's own scope and
// owner, and the command runs over a connection the caller made.
func TestSessionDialRunsWithServerHeldKey(t *testing.T) {
	host, user := newSigner(t), newSigner(t)
	port := startSSHServer(t, host, user.PublicKey())
	tr := &dialTransport{user: user, pins: []*Pin{pinFor("127.0.0.1", host.PublicKey())}}

	sess, err := NewClient(tr).Session().Dial(context.Background(), SessionDialRequest{
		KeyScope: "user", KeyOwner: "alice", KeyName: "laptop",
		Host: "127.0.0.1", Port: port, User: "git", TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = sess.Close() }()

	stdout, _, err := sess.Run(context.Background(), "whoami")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if string(stdout) != "ran: whoami" {
		t.Fatalf("stdout = %q", stdout)
	}
	if !tr.seen(MethodSignDigest) {
		t.Fatal("the signature was not made behind the transport")
	}
	if len(tr.pinScopes) == 0 || tr.pinScopes[0].Scope != "user" || tr.pinScopes[0].Owner != "alice" {
		t.Fatalf("pins read from %+v, want the key's scope and owner", tr.pinScopes)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestSessionDialUsesHostKeyScope proves an explicit pin-store scope wins
// over the key's own.
func TestSessionDialUsesHostKeyScope(t *testing.T) {
	host, user := newSigner(t), newSigner(t)
	port := startSSHServer(t, host, user.PublicKey())
	tr := &dialTransport{user: user, pins: []*Pin{pinFor("127.0.0.1", host.PublicKey())}}

	sess, err := NewClient(tr).Session().Dial(context.Background(), SessionDialRequest{
		KeyScope: "user", KeyOwner: "alice", KeyName: "laptop",
		HostKeyScope: "tenant", HostKeyOwner: "acme",
		Host: "127.0.0.1", Port: port, User: "git", TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	_ = sess.Close()
	if tr.pinScopes[0].Scope != "tenant" || tr.pinScopes[0].Owner != "acme" {
		t.Fatalf("pins read from %+v, want tenant/acme", tr.pinScopes[0])
	}
}

// TestSessionDialFailsClosedOnHostKey proves there is no trust on first use:
// an unpinned host and a changed host key both refuse the session.
func TestSessionDialFailsClosedOnHostKey(t *testing.T) {
	host, user := newSigner(t), newSigner(t)
	port := startSSHServer(t, host, user.PublicKey())
	req := SessionDialRequest{
		KeyName: "laptop", Host: "127.0.0.1", Port: port, User: "git", TimeoutSeconds: 5,
	}

	unpinned := &dialTransport{user: user}
	if _, err := NewClient(unpinned).Session().Dial(context.Background(), req); err == nil {
		t.Fatal("an unpinned host was trusted")
	}

	changed := &dialTransport{user: user, pins: []*Pin{pinFor("127.0.0.1", newSigner(t).PublicKey())}}
	_, err := NewClient(changed).Session().Dial(context.Background(), req)
	var mismatch *HostKeyMismatchError
	if !stderrors.As(err, &mismatch) {
		t.Fatalf("a changed host key gave %v, want *HostKeyMismatchError", err)
	}
	if changed.seen(MethodSignDigest) {
		t.Fatal("the key signed for a host that failed verification")
	}
}

func TestSessionDialValidatesRequest(t *testing.T) {
	ok := SessionDialRequest{KeyName: "k", Host: "h", User: "u"}
	cases := map[string]func(r *SessionDialRequest){
		"host":    func(r *SessionDialRequest) { r.Host = "" },
		"user":    func(r *SessionDialRequest) { r.User = "" },
		"key":     func(r *SessionDialRequest) { r.KeyName = "" },
		"port":    func(r *SessionDialRequest) { r.Port = 70000 },
		"timeout": func(r *SessionDialRequest) { r.TimeoutSeconds = -1 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			tr := &dialTransport{user: newSigner(t)}
			req := ok
			mutate(&req)
			_, err := NewClient(tr).Session().Dial(context.Background(), req)
			if errors.Code(err) != errors.CodeInvalidInput {
				t.Fatalf("err = %v, want InvalidInput", err)
			}
			if len(tr.methods) != 0 {
				t.Fatalf("an invalid request reached the transport: %v", tr.methods)
			}
		})
	}
	//nolint:staticcheck // a nil context is the case under test
	_, err := NewClient(&dialTransport{}).Session().Dial(nil, ok)
	if errors.Code(err) != errors.CodeInvalidInput {
		t.Fatalf("nil context: err = %v, want InvalidInput", err)
	}
}

// TestSessionDialReportsUnreachableHost proves a refused TCP connect is an
// error, not a hang, after the signer resolved.
func TestSessionDialReportsUnreachableHost(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	tr := &dialTransport{user: newSigner(t)}
	_, err = NewClient(tr).Session().Dial(context.Background(), SessionDialRequest{
		KeyName: "k", Host: "127.0.0.1", Port: port, User: "u", TimeoutSeconds: 2,
	})
	if err == nil {
		t.Fatal("dial to a closed port succeeded on " + strconv.Itoa(port))
	}
}
