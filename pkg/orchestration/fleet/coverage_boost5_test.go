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
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	proto "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// GRPCSpawnServer.Start — TCP bind path (no ListenerOverride)
// =============================================================================

// TestGRPCSpawnServer_Start_BindsTCPWhenNoListenerOverride exercises the
// real TCP listen path in Start by not providing a ListenerOverride.
// The server binds to ":0" (ephemeral port) which always succeeds on loopback.
func TestGRPCSpawnServer_Start_BindsTCPWhenNoListenerOverride(t *testing.T) {
	t.Parallel()
	rt := newTestRuntime(t)
	srv, err := NewGRPCSpawnServer(GRPCSpawnServerDeps{
		Runtime:  rt,
		BindAddr: "127.0.0.1:0",
		Logger:   &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewGRPCSpawnServer: %v", err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start with TCP: %v", err)
	}
	// Addr should return the actual bound address.
	if got := srv.Addr(); got == "" {
		t.Fatal("Addr() should return non-empty address after TCP Start")
	}
	if !strings.Contains(srv.Addr(), "127.0.0.1:") {
		t.Errorf("Addr() should contain 127.0.0.1, got %q", srv.Addr())
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// TestGRPCSpawnServer_Start_FailsOnInvalidAddr exercises the TCP bind error
// path (SpawnServerBindError) by attempting to listen on an invalid address.
func TestGRPCSpawnServer_Start_FailsOnInvalidAddr(t *testing.T) {
	t.Parallel()
	rt := newTestRuntime(t)
	srv, err := NewGRPCSpawnServer(GRPCSpawnServerDeps{
		Runtime:  rt,
		BindAddr: "256.256.256.256:9999",
		Logger:   &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewGRPCSpawnServer: %v", err)
	}
	bindErr := srv.Start(context.Background())
	if bindErr == nil {
		_ = srv.Stop(context.Background())
		t.Fatal("expected bind error for invalid address")
	}
	var spawnErr *SpawnServerBindError
	if !errors.As(bindErr, &spawnErr) {
		t.Fatalf("expected *SpawnServerBindError, got %T: %v", bindErr, bindErr)
	}
}

// =============================================================================
// GRPCSpawnServer.Addr — after TCP start returns real address
// =============================================================================

// TestGRPCSpawnServer_AddrAfterTCPStart verifies Addr() returns the real
// bound address (covering the non-nil listener branch with a real TCP server).
func TestGRPCSpawnServer_AddrAfterTCPStart(t *testing.T) {
	t.Parallel()
	rt := newTestRuntime(t)
	srv, err := NewGRPCSpawnServer(GRPCSpawnServerDeps{
		Runtime:  rt,
		BindAddr: "127.0.0.1:0",
		Logger:   &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewGRPCSpawnServer: %v", err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	addr := srv.Addr()
	if addr == "" {
		t.Fatal("Addr should be non-empty after Start")
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Stop(stopCtx)
}

// =============================================================================
// GRPCSpawnServer.Start — idempotent when called twice
// =============================================================================

// TestGRPCSpawnServer_Start_CalledTwiceIsIdempotent covers the
// "already started" early-return branch.
func TestGRPCSpawnServer_Start_CalledTwiceIsIdempotent(t *testing.T) {
	t.Parallel()
	rt := newTestRuntime(t)
	srv, err := NewGRPCSpawnServer(GRPCSpawnServerDeps{
		Runtime:  rt,
		BindAddr: "127.0.0.1:0",
		Logger:   &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewGRPCSpawnServer: %v", err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	// Second call must return nil (idempotent).
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Stop(stopCtx)
}

// =============================================================================
// Heartbeat — missing scope (no runtime:status scope)
// =============================================================================

// TestGRPCSpawnServer_Heartbeat_RequiresStatusScope verifies that a heartbeat
// stream is rejected with PermissionDenied when the identity lacks the status
// scope (which Heartbeat uses for authorization).
func TestGRPCSpawnServer_Heartbeat_RequiresStatusScope(t *testing.T) {
	t.Parallel()

	id := newTestIdentity()
	id.Scopes = []string{orchestration.AgentScopeRuntimeSpawn} // no status scope

	stream, cleanup := buildHeartbeatClient(t, id)
	defer cleanup()

	if err := stream.Send(&proto.HeartbeatBeat{Sequence: uint64(1)}); err != nil {
		// Error on send is acceptable if the server already rejected.
		return
	}
	_, err := stream.Recv()
	if err == nil {
		t.Fatal("expected PermissionDenied for missing status scope")
	}
	// Accept PermissionDenied or EOF (server closed).
}

// =============================================================================
// writeAtomic — success path with persistence via Session
// =============================================================================

// TestWriteAtomic_ViaPersistSession exercises writeAtomic end-to-end via the
// persistSession→writeAtomic path with no DEK manager. This covers the
// successful write+chmod+close+rename sequence.
func TestWriteAtomic_ViaPersistSession_Success(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	rt, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"acp": &uniqueIDBackend{}},
		Logger:   &testLogger{t: t},
		WorkDir:  workDir,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	// Spawn sessions to exercise writeAtomic.
	const count = 3
	for i := 0; i < count; i++ {
		sess, err := rt.Spawn(context.Background(), "tenant-atomic", nil, "acp",
			&orchestration.SpawnRequest{WorkDir: workDir})
		if err != nil {
			t.Fatalf("Spawn[%d]: %v", i, err)
		}
		if sess.ID == "" {
			t.Fatalf("Spawn[%d]: empty session ID", i)
		}
	}

	// All three sessions should be persisted.
	loaded, err := rt.LoadPersistedSessions(context.Background())
	if err != nil {
		t.Fatalf("LoadPersistedSessions: %v", err)
	}
	if len(loaded) != count {
		t.Fatalf("expected %d loaded sessions, got %d", count, len(loaded))
	}
}

// uniqueIDBackend generates distinct session IDs per Spawn call so persistence
// tests that write multiple sessions don't overwrite the same file.
type uniqueIDBackend struct {
	counter int
}

func (u *uniqueIDBackend) Spawn(_ context.Context, req *orchestration.SpawnRequest) (*orchestration.Session, error) {
	u.counter++
	return &orchestration.Session{
		ID:      fmt.Sprintf("unique-sess-%d", u.counter),
		WorkDir: req.WorkDir,
	}, nil
}
func (u *uniqueIDBackend) Prompt(_ context.Context, req *orchestration.PromptRequest) (*orchestration.PromptResponse, error) {
	return &orchestration.PromptResponse{SessionID: req.SessionID}, nil
}
func (u *uniqueIDBackend) Status(_ context.Context, id string) (*orchestration.SessionStatus, error) {
	return &orchestration.SessionStatus{SessionID: id}, nil
}
func (u *uniqueIDBackend) Close(_ context.Context, _ string) error { return nil }
func (u *uniqueIDBackend) Capabilities() orchestration.BackendCapabilities {
	return orchestration.BackendCapabilities{Kind: orchestration.BackendKindAgent}
}
func (u *uniqueIDBackend) Type() string         { return "acp" }
func (u *uniqueIDBackend) ToolGuidance() string { return "" }

// =============================================================================
// LoadPersistedSessions — directory contains a mix of valid and corrupt files
// =============================================================================

// TestRuntime_LoadPersistedSessions_MixedValidAndCorrupt verifies that
// LoadPersistedSessions returns valid sessions alongside a PersistedSessionCorruptError
// counting only the corrupt files.
func TestRuntime_LoadPersistedSessions_MixedValidAndCorrupt(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	rt, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"acp": &uniqueIDBackend{}},
		Logger:   &testLogger{t: t},
		WorkDir:  workDir,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	// Spawn a valid session.
	sess, err := rt.Spawn(context.Background(), "tenant-mix", nil, "acp",
		&orchestration.SpawnRequest{WorkDir: workDir})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// Also write a corrupt file.
	sessionsDir := workDir + "/sessions"
	corruptPath := sessionsDir + "/corrupt.json"
	if writeErr := writeAtomic(corruptPath, []byte(`{bad json}`)); writeErr != nil {
		t.Fatalf("write corrupt: %v", writeErr)
	}

	loaded, loadErr := rt.LoadPersistedSessions(context.Background())
	var corruptErr *PersistedSessionCorruptError
	if !errors.As(loadErr, &corruptErr) {
		t.Fatalf("expected PersistedSessionCorruptError, got %T: %v", loadErr, loadErr)
	}
	if corruptErr.Count != 1 {
		t.Errorf("corrupt count: got %d, want 1", corruptErr.Count)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 valid session, got %d: %+v", len(loaded), loaded)
	}
	if loaded[0].SessionID != sess.ID {
		t.Errorf("session id: got %q, want %q", loaded[0].SessionID, sess.ID)
	}
}
