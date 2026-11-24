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
	stderrors "errors"
	"net"
	"sync"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	xkmssession "github.com/jeremyhahn/go-xkms/pkg/ssh/session"
)

// startRekeyingSSHServer is startSSHServer with the lowest rekey threshold
// x/crypto allows, so a few commands force a key re-exchange, and with it a
// fresh host-key check, after Dial has returned.
func startRekeyingSSHServer(t *testing.T, hostKey gossh.Signer, userPub gossh.PublicKey) int {
	t.Helper()
	cfg := &gossh.ServerConfig{
		Config: gossh.Config{RekeyThreshold: 256},
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

// TestSessionDialHostKeyCheckSurvivesRekey proves that the host-key check a
// key re-exchange runs after Dial has returned still reaches the pins, and
// passes, rather than failing on the dial's cancelled context.
func TestSessionDialHostKeyCheckSurvivesRekey(t *testing.T) {
	host, user := newSigner(t), newSigner(t)
	port := startRekeyingSSHServer(t, host, user.PublicKey())
	tr := &dialTransport{user: user, pins: []*Pin{pinFor("127.0.0.1", host.PublicKey())}}

	sess, err := NewClient(tr).Session().Dial(context.Background(), SessionDialRequest{
		KeyName: "laptop", Host: "127.0.0.1", Port: port, User: "git", TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = sess.Close() }()

	for i := 0; i < 10; i++ {
		stdout, _, err := sess.Run(context.Background(), "whoami")
		if err != nil {
			t.Fatalf("Run %d: %v", i, err)
		}
		if string(stdout) != "ran: whoami" {
			t.Fatalf("Run %d: stdout = %q", i, stdout)
		}
	}

	tr.mu.Lock()
	checks := 0
	for _, m := range tr.methods {
		if m == MethodListKnownHosts {
			checks++
		}
	}
	tr.mu.Unlock()
	if checks < 2 {
		t.Fatalf("host-key pins read %d times, want a check on the handshake and on a rekey", checks)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestSessionDialCancellationStopsStalledHandshake proves that cancelling
// Dial stops it while a peer that accepted TCP withholds its SSH banner,
// rather than waiting out the dial timeout.
func TestSessionDialCancellationStopsStalledHandshake(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var held []net.Conn
	t.Cleanup(func() {
		_ = ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range held {
			_ = c.Close()
		}
	})
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			held = append(held, c)
			mu.Unlock()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	host, user := newSigner(t), newSigner(t)
	tr := &dialTransport{user: user, pins: []*Pin{pinFor("127.0.0.1", host.PublicKey())}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(200*time.Millisecond, cancel)

	start := time.Now()
	_, err = NewClient(tr).Session().Dial(ctx, SessionDialRequest{
		KeyName: "laptop", Host: "127.0.0.1", Port: port, User: "git", TimeoutSeconds: 60,
	})
	if err == nil {
		t.Fatal("Dial succeeded against a peer that sent no banner")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Dial took %s after cancellation", elapsed)
	}
	var te *xkmssession.TimeoutError
	if !stderrors.As(err, &te) {
		t.Fatalf("Dial error = %v (%T), want a *session.TimeoutError", err, err)
	}
}
