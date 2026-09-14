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

package orchestration

import (
	"context"
	"io"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// envFileContainerRuntime is a ContainerRuntime fake that hands back an
// InteractiveProcess carrying a Cleanup, exactly as ExecContainerRuntime does
// for the private --env-file it wrote. It records how many times that Cleanup
// ran so a test can prove the session — not the runtime — owns the file's
// lifetime, and that it is released exactly once.
type envFileContainerRuntime struct {
	t *testing.T

	// cleanupCalls counts Cleanup invocations.
	cleanupCalls atomic.Int32

	// failHandshake makes the fake return pipes that never answer the ACP
	// handshake, so the spawn fails after RunInteractive succeeded — the path
	// that must still release the env file.
	failHandshake bool
}

func (r *envFileContainerRuntime) RunInteractive(ctx context.Context, spec *ContainerSpec) (*InteractiveProcess, error) {
	serverRead, clientWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	clientRead, serverWrite, err := os.Pipe()
	if err != nil {
		_ = clientWrite.Close()
		_ = serverRead.Close()
		return nil, err
	}
	done := make(chan struct{})
	if !r.failHandshake {
		go acpMockResponder(r.t, serverRead, serverWrite, done)
	}
	go func() {
		<-ctx.Done()
		close(done)
		_ = serverRead.Close()
		_ = clientWrite.Close()
		_ = clientRead.Close()
	}()

	return &InteractiveProcess{
		ContainerID: "test-ctr",
		Stdin:       clientWrite,
		Stdout:      clientRead,
		Cleanup:     func() { r.cleanupCalls.Add(1) },
	}, nil
}

func (r *envFileContainerRuntime) Run(context.Context, *ContainerSpec) (string, error) {
	return "test-ctr", nil
}
func (r *envFileContainerRuntime) Stop(context.Context, string) error { return nil }

// Exec satisfies the runtime seam. Nothing in this test runs a command
// inside a container; a call would be a test asserting behaviour it never
// arranged, so it refuses rather than answering.
func (r *envFileContainerRuntime) Exec(context.Context, string, *ExecSpec) (*ExecResult, error) {
	return nil, &ContainerExecError{ContainerID: "fake", Cause: errNoExecInThisTest}
}
func (r *envFileContainerRuntime) Remove(context.Context, string) error { return nil }
func (r *envFileContainerRuntime) Logs(context.Context, string) (io.ReadCloser, error) {
	return nil, nil
}
func (r *envFileContainerRuntime) IsRunning(context.Context, string) (bool, error) {
	return true, nil
}
func (r *envFileContainerRuntime) IsAvailable() bool                  { return true }
func (r *envFileContainerRuntime) Pull(context.Context, string) error { return nil }

// TestACPBackend_SpawnContainer_EnvFileReleasedOnClose proves the sandboxed
// session owns the runtime's env file for exactly as long as the container
// runs: not released while the agent is live (the `docker run` process may
// still be reading it), released on Close.
func TestACPBackend_SpawnContainer_EnvFileReleasedOnClose(t *testing.T) {
	runtime := &envFileContainerRuntime{t: t}
	backend := NewACPBackend(ACPBackendConfig{Command: "opencode"})
	backend.SetContainerRuntime(runtime)

	ctx, cancel := context.WithTimeout(ContextWithTenant(context.Background(), "system"), 2*time.Second)
	defer cancel()

	sess, err := backend.Spawn(ctx, &SpawnRequest{
		WorkDir:        "/tmp",
		SandboxMode:    SandboxModeRequired,
		ContainerImage: "opencode:test",
		Env:            map[string]string{"ANTHROPIC_API_KEY": credentialFixture},
	})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	if got := runtime.cleanupCalls.Load(); got != 0 {
		t.Fatalf("env file released while the container is still running: %d calls", got)
	}

	if closeErr := backend.Close(context.Background(), sess.ID); closeErr != nil {
		t.Fatalf("Close() error = %v", closeErr)
	}
	if got := runtime.cleanupCalls.Load(); got != 1 {
		t.Fatalf("env file cleanup ran %d times on Close, want 1", got)
	}

	// Closing an already-closed session must not fire it a second time.
	_ = backend.Close(context.Background(), sess.ID)
	if got := runtime.cleanupCalls.Load(); got != 1 {
		t.Fatalf("env file cleanup ran %d times after a repeat Close, want 1", got)
	}
}

// TestACPBackend_SpawnContainer_EnvFileReleasedOnFailedHandshake proves a spawn
// that dies between RunInteractive and a working session does not leave the
// credential file on disk for the daemon's lifetime.
func TestACPBackend_SpawnContainer_EnvFileReleasedOnFailedHandshake(t *testing.T) {
	runtime := &envFileContainerRuntime{t: t, failHandshake: true}
	backend := NewACPBackend(ACPBackendConfig{Command: "opencode"})
	backend.SetContainerRuntime(runtime)

	ctx, cancel := context.WithTimeout(ContextWithTenant(context.Background(), "system"), 2*time.Second)
	defer cancel()

	_, err := backend.Spawn(ctx, &SpawnRequest{
		WorkDir:        "/tmp",
		SandboxMode:    SandboxModeRequired,
		ContainerImage: "opencode:test",
		Env:            map[string]string{"ANTHROPIC_API_KEY": credentialFixture},
	})
	if err == nil {
		t.Fatal("expected the spawn to fail when the agent never answers the handshake")
	}
	if got := runtime.cleanupCalls.Load(); got != 1 {
		t.Fatalf("env file cleanup ran %d times on a failed spawn, want 1", got)
	}
}

// TestACPSession_CleanupSlotsAreIndependent proves the three per-session
// release slots stay separate: the MCP cert directory, the pooled-account
// credential directory, and the container env file are released by Close
// without one slot shadowing another. Fusing them would make an account
// rotation either leak the cert directory or delete a file the running
// container still needs.
func TestACPSession_CleanupSlotsAreIndependent(t *testing.T) {
	backend := NewACPBackend(ACPBackendConfig{Command: "opencode"})

	var mcp, account, envFile atomic.Int32
	sess := &acpSession{
		id:             "sess-1",
		cleanup:        func() { mcp.Add(1) },
		accountCleanup: func() { account.Add(1) },
		envFileCleanup: func() { envFile.Add(1) },
	}
	backend.storeSession(sess)

	if err := backend.Close(context.Background(), "sess-1"); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	for name, counter := range map[string]*atomic.Int32{
		"mcp cert": &mcp,
		"account":  &account,
		"env file": &envFile,
	} {
		if got := counter.Load(); got != 1 {
			t.Errorf("%s cleanup ran %d times, want 1", name, got)
		}
	}
}

// TestACPBackend_RotateAccount_KeepsEnvFile proves an account rotation replaces
// only the account's credential material: the container env file belongs to the
// still-running `docker run` process and must survive.
func TestACPBackend_RotateAccount_KeepsEnvFile(t *testing.T) {
	backend := NewACPBackend(ACPBackendConfig{Command: "opencode"})

	var account, envFile atomic.Int32
	sess := &acpSession{
		id:             "sess-1",
		accountCleanup: func() { account.Add(1) },
		envFileCleanup: func() { envFile.Add(1) },
	}
	backend.storeSession(sess)

	backend.attachAccount("sess-1", &resolvedSpawnAccount{
		AccountID: "acct-2",
		Strategy:  "least-usage",
		EnvKeys:   []string{"ANTHROPIC_API_KEY"},
		cleanup:   func() {},
	})

	if got := account.Load(); got != 1 {
		t.Errorf("the replaced account's material was not released: %d calls", got)
	}
	if got := envFile.Load(); got != 0 {
		t.Fatalf("the rotation released the container env file: %d calls", got)
	}
}
