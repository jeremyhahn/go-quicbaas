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

package main

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gossh "golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

func testAgentSigner(t *testing.T) gossh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	return signer
}

// runTestAgent serves signer until the returned cancel is called, and returns
// the announced socket path and a channel carrying serveSSHAgent's result.
func runTestAgent(t *testing.T, signer gossh.Signer, envFile string) (string, context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- serveSSHAgent(ctx, signer, "", envFile, writer)
		writer.Close()
	}()
	line, err := bufio.NewReader(reader).ReadString('\n')
	if err != nil {
		cancel()
		t.Fatalf("no socket announced: %v (serve returned %v)", err, <-done)
	}
	go func() { _, _ = io.Copy(io.Discard, reader) }()
	path, ok := strings.CutPrefix(strings.TrimSpace(line), "SSH_AUTH_SOCK=")
	if !ok {
		cancel()
		t.Fatalf("announcement %q is not SSH_AUTH_SOCK=<path>", line)
	}
	return path, cancel, done
}

// TestServeSSHAgent_SignsThroughTheSocket is the contract git relies on: the
// socket lists the key and produces signatures that verify against it.
func TestServeSSHAgent_SignsThroughTheSocket(t *testing.T) {
	signer := testAgentSigner(t)
	path, cancel, done := runTestAgent(t, signer, "")

	conn, err := net.Dial("unix", path)
	if err != nil {
		cancel()
		t.Fatalf("dial agent: %v", err)
	}
	client := sshagent.NewClient(conn)
	keys, err := client.List()
	if err != nil || len(keys) != 1 || string(keys[0].Blob) != string(signer.PublicKey().Marshal()) {
		t.Fatalf("List = %v, %v; want the one served key", keys, err)
	}
	data := []byte("commit to sign")
	sig, err := client.Sign(signer.PublicKey(), data)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := signer.PublicKey().Verify(data, sig); err != nil {
		t.Fatalf("signature does not verify: %v", err)
	}
	conn.Close()

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serve returned %v after cancel, want nil", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket %s survived the stop: %v", path, err)
	}
}

// TestServeSSHAgent_WritesTheEnvFilePrivately: the env file carries the same
// line and is readable only by its owner.
func TestServeSSHAgent_WritesTheEnvFilePrivately(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "agent.env")
	path, cancel, done := runTestAgent(t, testAgentSigner(t), envFile)
	defer func() { cancel(); <-done }()

	content, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	if string(content) != "SSH_AUTH_SOCK="+path+"\n" {
		t.Fatalf("env file = %q", content)
	}
	info, err := os.Stat(envFile)
	if err != nil {
		t.Fatalf("stat env file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("env file mode = %v, want 0600", info.Mode().Perm())
	}
}

// TestServeSSHAgent_RefusesAnExistingSocketDir: the agent never adopts a
// directory it did not create.
func TestServeSSHAgent_RefusesAnExistingSocketDir(t *testing.T) {
	err := serveSSHAgent(context.Background(), testAgentSigner(t), t.TempDir(), "", io.Discard)
	var agentErr *SSHAgentError
	if !errors.As(err, &agentErr) || agentErr.Op != "start" {
		t.Fatalf("serve into an existing dir returned %v, want an SSHAgentError on start", err)
	}
}

// TestServeSSHAgent_AnUnwritableEnvFileFails: an announcement that cannot be
// written is an error, and the agent does not keep running unannounced.
func TestServeSSHAgent_AnUnwritableEnvFileFails(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "missing-dir", "agent.env")
	err := serveSSHAgent(context.Background(), testAgentSigner(t), "", envFile, io.Discard)
	var agentErr *SSHAgentError
	if !errors.As(err, &agentErr) || agentErr.Op != "write env file" {
		t.Fatalf("serve returned %v, want an SSHAgentError on the env file", err)
	}
}

// TestNewSSHAgentCmd_RequiresAKeyName: the command resolves exactly one key.
func TestNewSSHAgentCmd_RequiresAKeyName(t *testing.T) {
	cmd := newSSHAgentCmd()
	if err := runSSHAgent(cmd, nil); !errors.Is(err, ErrSSHMissingKeyName) {
		t.Fatalf("runSSHAgent without --name returned %v, want ErrSSHMissingKeyName", err)
	}
}
