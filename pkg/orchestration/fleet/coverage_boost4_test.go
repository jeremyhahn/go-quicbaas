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

package fleet

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// sendHeartbeat — nil identity early-return path
// =============================================================================

// TestDaemon_SendHeartbeat_NilIdentityIsNoOp verifies that sendHeartbeat
// returns immediately without calling the heartbeat sender when no identity
// has been enrolled yet.
func TestDaemon_SendHeartbeat_NilIdentityIsNoOp(t *testing.T) {
	t.Parallel()

	hb := &fakeHeartbeat{}
	cfg := newTestConfig(t)
	d, err := NewDaemon(cfg, DaemonDeps{
		Runtime:     newTestRuntime(t),
		Enroller:    &fakeEnroller{identity: &orchestration.AgentIdentity{AgentID: "a", TenantID: cfg.TenantID}},
		Heartbeat:   hb,
		SpawnServer: &fakeSpawnServer{addr: "127.0.0.1:0"},
		Logger:      &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}

	// Do NOT call Start — the identity is still nil.
	d.sendHeartbeat(context.Background())

	// Heartbeat sender must NOT have been called.
	if got := hb.ticks.Load(); got != 0 {
		t.Errorf("expected 0 heartbeat ticks with nil identity, got %d", got)
	}
}

// =============================================================================
// removePersistedSession — empty sessionID no-op path
// =============================================================================

// TestRuntime_RemovePersistedSession_EmptyIDIsNoOp verifies that
// removePersistedSession with an empty session ID is a no-op.
func TestRuntime_RemovePersistedSession_EmptyIDIsNoOp(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	rt, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"acp": &mockBackend{}},
		Logger:   &testLogger{t: t},
		WorkDir:  workDir,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	// Must not panic.
	rt.removePersistedSession("")
}

// TestRuntime_RemovePersistedSession_UnremovableFileIsLogged verifies that
// a file that can't be removed (e.g., due to permissions) does not return
// an error but logs a warning. This exercises the os.Remove error branch.
func TestRuntime_RemovePersistedSession_UnremovableFileIsLogged(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root can remove any file")
	}

	workDir := t.TempDir()
	sessionsDir := filepath.Join(workDir, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create a session file and make the parent dir read-only so removal fails.
	sessID := "unremovable-sess"
	path := filepath.Join(sessionsDir, sessID+".json")
	if err := os.WriteFile(path, []byte(`{"session_id":"unremovable-sess"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Chmod(sessionsDir, 0o555); err != nil {
		t.Fatalf("chmod sessions dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(sessionsDir, 0o700) })

	rt, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"acp": &mockBackend{}},
		Logger:   &testLogger{t: t},
		WorkDir:  workDir,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	// Must not panic — the warning is logged internally.
	rt.removePersistedSession(sessID)
}

// =============================================================================
// persistSession — MkdirAll failure path
// =============================================================================

// TestRuntime_PersistSession_SessionsDirFailure exercises the MkdirAll
// failure inside persistSession when the sessions directory cannot be created.
func TestRuntime_PersistSession_SessionsDirFailure(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root bypasses permission checks")
	}

	workDir := t.TempDir()
	// Make workDir itself read-only so creating the sessions subdirectory fails.
	if err := os.Chmod(workDir, 0o555); err != nil {
		t.Fatalf("chmod workDir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(workDir, 0o755) })

	rt, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"acp": &mockBackend{}},
		Logger:   &testLogger{t: t},
		WorkDir:  workDir,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	// persistSession is called inside Spawn and errors are non-fatal.
	sess, spawnErr := rt.Spawn(context.Background(), "tenant-dir-fail", nil, "acp",
		&orchestration.SpawnRequest{WorkDir: workDir})
	if spawnErr != nil {
		t.Fatalf("Spawn: expected non-fatal persist error, got %v", spawnErr)
	}
	if sess.ID == "" {
		t.Fatal("expected non-empty session ID")
	}
}

// =============================================================================
// NewDaemon — Heartbeat can be nil (valid — optional dependency)
// =============================================================================

// TestNewDaemon_NilHeartbeatIsAccepted verifies that a nil Heartbeat dep
// is accepted (heartbeat is optional — the daemon runs without one).
func TestNewDaemon_NilHeartbeatIsAccepted(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	d, err := NewDaemon(cfg, DaemonDeps{
		Runtime:     newTestRuntime(t),
		Enroller:    &fakeEnroller{identity: &orchestration.AgentIdentity{AgentID: "a", TenantID: cfg.TenantID}},
		SpawnServer: &fakeSpawnServer{addr: "127.0.0.1:0"},
		Logger:      &testLogger{t: t},
		Heartbeat:   nil, // optional
	})
	if err != nil {
		t.Fatalf("NewDaemon with nil Heartbeat: %v", err)
	}
	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := d.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}
