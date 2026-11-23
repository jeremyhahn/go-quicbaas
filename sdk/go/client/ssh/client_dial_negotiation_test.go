package ssh

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	stderrors "errors"
	"net"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

// startRSASHA2Server starts an SSH server that accepts userPub only when it
// signs with rsa-sha2-256 or rsa-sha2-512, never legacy ssh-rsa.
func startRSASHA2Server(t *testing.T, hostKey gossh.Signer, userPub gossh.PublicKey) int {
	t.Helper()
	cfg := &gossh.ServerConfig{
		PublicKeyAuthAlgorithms: []string{gossh.KeyAlgoRSASHA512, gossh.KeyAlgoRSASHA256},
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

func newRSASigner(t *testing.T) gossh.Signer {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	s, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSessionDialNegotiatesRSASHA2(t *testing.T) {
	host, user := newSigner(t), newRSASigner(t)
	port := startRSASHA2Server(t, host, user.PublicKey())
	tr := &dialTransport{user: user, pins: []*Pin{pinFor("127.0.0.1", host.PublicKey())}}

	sess, err := NewClient(tr).Session().Dial(context.Background(), SessionDialRequest{
		KeyName: "rsa", Host: "127.0.0.1", Port: port, User: "git", TimeoutSeconds: 10,
	})
	if err != nil {
		t.Fatalf("Dial with an RSA key against an RSA SHA-2 only server: %v", err)
	}
	_ = sess.Close()
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if len(tr.algorithms) == 0 {
		t.Fatal("no signing request reached the transport")
	}
	last := tr.algorithms[len(tr.algorithms)-1]
	if last != gossh.KeyAlgoRSASHA256 && last != gossh.KeyAlgoRSASHA512 {
		t.Fatalf("signed with algorithm %q, want rsa-sha2-256 or rsa-sha2-512", last)
	}
}

func TestRemoteSignerSignsWithRequestedAlgorithm(t *testing.T) {
	user := newRSASigner(t)
	tr := &dialTransport{user: user}
	signer, err := NewClient(tr).Keys().Signer(context.Background(), GetKeyRequest{Name: "rsa"})
	if err != nil {
		t.Fatalf("Signer: %v", err)
	}
	algSigner, ok := signer.(gossh.AlgorithmSigner)
	if !ok {
		t.Fatal("the server-held key's signer does not implement ssh.AlgorithmSigner")
	}
	data := []byte("session-data")
	sig, err := algSigner.SignWithAlgorithm(rand.Reader, data, gossh.KeyAlgoRSASHA512)
	if err != nil {
		t.Fatalf("SignWithAlgorithm: %v", err)
	}
	if sig.Format != gossh.KeyAlgoRSASHA512 {
		t.Fatalf("signature format %q, want %q", sig.Format, gossh.KeyAlgoRSASHA512)
	}
	if err := user.PublicKey().Verify(data, sig); err != nil {
		t.Fatalf("signature does not verify: %v", err)
	}
	if tr.algorithms[0] != gossh.KeyAlgoRSASHA512 {
		t.Fatalf("transport saw algorithm %q, want %q", tr.algorithms[0], gossh.KeyAlgoRSASHA512)
	}
}

func TestSessionDialDefaultsEachPinSelector(t *testing.T) {
	cases := []struct {
		name               string
		hostScope, hostOwn string
		wantScope, wantOwn string
	}{
		{name: "owner only", hostOwn: "bob", wantScope: "user", wantOwn: "bob"},
		{name: "scope only", hostScope: "tenant", wantScope: "tenant", wantOwn: "alice"},
		{name: "neither", wantScope: "user", wantOwn: "alice"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			host, user := newSigner(t), newSigner(t)
			port := startSSHServer(t, host, user.PublicKey())
			tr := &dialTransport{user: user, pins: []*Pin{pinFor("127.0.0.1", host.PublicKey())}}

			sess, err := NewClient(tr).Session().Dial(context.Background(), SessionDialRequest{
				KeyScope: "user", KeyOwner: "alice", KeyName: "laptop",
				HostKeyScope: tc.hostScope, HostKeyOwner: tc.hostOwn,
				Host: "127.0.0.1", Port: port, User: "git", TimeoutSeconds: 5,
			})
			if err != nil {
				t.Fatalf("Dial: %v", err)
			}
			_ = sess.Close()
			got := tr.pinScopes[0]
			if got.Scope != tc.wantScope || got.Owner != tc.wantOwn {
				t.Fatalf("pins read from %s/%s, want %s/%s", got.Scope, got.Owner, tc.wantScope, tc.wantOwn)
			}
		})
	}
}

func TestSessionDialTimeoutBoundsBlockedRPCs(t *testing.T) {
	for _, method := range []string{MethodListKnownHosts, MethodSignDigest} {
		t.Run(method, func(t *testing.T) {
			host, user := newSigner(t), newSigner(t)
			port := startSSHServer(t, host, user.PublicKey())
			tr := &dialTransport{
				user:  user,
				pins:  []*Pin{pinFor("127.0.0.1", host.PublicKey())},
				block: method,
			}

			start := time.Now()
			_, err := NewClient(tr).Session().Dial(context.Background(), SessionDialRequest{
				KeyName: "laptop", Host: "127.0.0.1", Port: port, User: "git", TimeoutSeconds: 1,
			})
			elapsed := time.Since(start)
			if err == nil {
				t.Fatal("Dial succeeded while its RPC was blocked")
			}
			if elapsed > 10*time.Second {
				t.Fatalf("Dial took %s with a 1s timeout", elapsed)
			}
			tr.mu.Lock()
			defer tr.mu.Unlock()
			blocked := false
			for _, m := range tr.methods {
				blocked = blocked || m == method
			}
			if !blocked {
				t.Fatalf("Dial never called %s; methods %v", method, tr.methods)
			}
		})
	}
}

func TestSessionDialCancellationStopsBlockedSigning(t *testing.T) {
	host, user := newSigner(t), newSigner(t)
	port := startSSHServer(t, host, user.PublicKey())
	tr := &dialTransport{
		user:  user,
		pins:  []*Pin{pinFor("127.0.0.1", host.PublicKey())},
		block: MethodSignDigest,
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)

	start := time.Now()
	_, err := NewClient(tr).Session().Dial(ctx, SessionDialRequest{
		KeyName: "laptop", Host: "127.0.0.1", Port: port, User: "git", TimeoutSeconds: 60,
	})
	if err == nil {
		t.Fatal("Dial succeeded after its context was cancelled")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("Dial took %s after cancellation", elapsed)
	}
}
