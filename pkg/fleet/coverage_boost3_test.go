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
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	proto "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/agentcert"
)

// =============================================================================
// bootstrap — bad server URL (joinURL failure path)
// =============================================================================

// TestProductionEnroller_Bootstrap_BadServerAddr exercises the joinURL
// failure path inside bootstrap when the ServerAddr is not parseable.
func TestProductionEnroller_Bootstrap_BadServerAddr(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	cert, key, ca := agentcert.Write(t, tmp)
	cfg := &Config{
		ServerAddr:   "://invalid",
		TenantID:     "tenant-a",
		BindAddr:     "127.0.0.1:0",
		WorkDir:      tmp,
		NodeCertFile: cert,
		NodeKeyFile:  key,
		ServerCAFile: ca,
	}

	enroller := newProdEnroller(t, http.DefaultClient)
	_, err := enroller.Enroll(context.Background(), cfg)
	if !errors.Is(err, ErrEnrollmentRejected) {
		t.Fatalf("expected ErrEnrollmentRejected for bad server addr, got %v", err)
	}
	var reqErr *EnrollmentRequestError
	if !errors.As(err, &reqErr) {
		t.Fatalf("expected *EnrollmentRequestError, got %T: %v", err, err)
	}
}

// =============================================================================
// bootstrap — HTTP transport failure (connection refused)
// =============================================================================

// TestProductionEnroller_Bootstrap_TransportError exercises the
// httpClient.Do failure path by pointing at an address where nothing listens.
func TestProductionEnroller_Bootstrap_TransportError(t *testing.T) {
	t.Parallel()

	// Use a TLS server and immediately close it so the port is no longer listening.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	addr := srv.URL
	srv.Close() // Close immediately — port is now unavailable.

	cfg := newEnrollerConfig(t, addr)
	if err := os.MkdirAll(cfg.WorkDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	enroller := newProdEnroller(t, srv.Client())
	_, err := enroller.Enroll(context.Background(), cfg)
	if !errors.Is(err, ErrEnrollmentRejected) {
		t.Fatalf("expected ErrEnrollmentRejected for transport error, got %v", err)
	}
	var rejErr *EnrollmentRejectedError
	if !errors.As(err, &rejErr) {
		t.Fatalf("expected *EnrollmentRejectedError, got %T", err)
	}
	// Status should be 0 (no HTTP response received).
	if rejErr.Status != 0 {
		t.Errorf("status: got %d, want 0 (transport failure)", rejErr.Status)
	}
}

// =============================================================================
// Spawn with MCP servers — exercises protoMCPServersToRuntime in Spawn path
// =============================================================================

// TestGRPCSpawnServer_Spawn_WithMCPServers exercises the protoMCPServersToRuntime
// conversion called inside the Spawn handler, covering the non-nil slice path.
func TestGRPCSpawnServer_Spawn_WithMCPServers(t *testing.T) {
	t.Parallel()

	var capturedMCP []orchestration.MCPServerConfig
	backend := &spawnBackend{
		spawnFn: func(ctx context.Context, req *orchestration.SpawnRequest) (*orchestration.Session, error) {
			capturedMCP = req.MCPServers
			return &orchestration.Session{
				ID:      "sess-mcp",
				Backend: "acp",
				Status:  orchestration.SessionStateActive,
			}, nil
		},
	}
	_, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
	defer cleanup()

	_, err := client.Spawn(context.Background(), &proto.SpawnSessionRequest{
		Backend:  "acp",
		TenantId: "tenant-a",
		McpServers: []*proto.MCPServerConfig{
			{Name: "server-1", Url: "http://mcp1:8080"},
			{Name: "server-2", Url: "http://mcp2:9090"},
		},
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if len(capturedMCP) != 2 {
		t.Fatalf("MCP servers: got %d, want 2", len(capturedMCP))
	}
	if capturedMCP[0].Name != "server-1" {
		t.Errorf("MCP[0].Name: got %q, want %q", capturedMCP[0].Name, "server-1")
	}
}

// =============================================================================
// Daemon.Stop — spawn server stop failure is logged but returned
// =============================================================================

// TestDaemon_Stop_SpawnServerStopErrorIsReturned verifies that when the
// spawn server's Stop returns an error, Daemon.Stop propagates it.
func TestDaemon_Stop_SpawnServerStopErrorIsReturned(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t)
	stopErr := errors.New("graceful shutdown failed")
	server := &fakeSpawnServer{
		addr:    "127.0.0.1:0",
		stopErr: stopErr,
	}
	d, err := NewDaemon(cfg, DaemonDeps{
		Runtime:     newTestRuntime(t),
		Enroller:    &fakeEnroller{identity: &orchestration.AgentIdentity{AgentID: "a", TenantID: cfg.TenantID}},
		SpawnServer: server,
		Logger:      &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}
	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	got := d.Stop(context.Background())
	if !errors.Is(got, stopErr) {
		t.Fatalf("Stop error: got %v, want wraps %v", got, stopErr)
	}
}

// =============================================================================
// NewDaemon — ensureWorkDir called with empty path
// =============================================================================

// TestNewDaemon_WorkDir_EnsuredOnConstruction verifies that WorkDir is
// created during NewDaemon and that a populated WorkDir on the config leads
// to a successful daemon construction.
func TestNewDaemon_WorkDir_EnsuredOnConstruction(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig(t)
	cfg.WorkDir = t.TempDir() + "/agent-workdir" // does not exist yet

	d, err := NewDaemon(cfg, DaemonDeps{
		Runtime:     newTestRuntime(t),
		Enroller:    &fakeEnroller{identity: &orchestration.AgentIdentity{AgentID: "a", TenantID: cfg.TenantID}},
		SpawnServer: &fakeSpawnServer{},
		Logger:      &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}
	if d == nil {
		t.Fatal("expected non-nil daemon")
	}
	// The workdir should now exist.
	if _, err := os.Stat(cfg.WorkDir); err != nil {
		t.Fatalf("WorkDir not created: %v", err)
	}
}

// =============================================================================
// GRPCSpawnServer.Status — session found in second backend
// =============================================================================

// TestGRPCSpawnServer_Status_FoundInAnyBackend verifies the multi-backend
// scan loop in Status returns success when the session is found.
func TestGRPCSpawnServer_Status_FoundInAnyBackend(t *testing.T) {
	t.Parallel()
	backend := &spawnBackend{
		statusFn: func(_ context.Context, id string) (*orchestration.SessionStatus, error) {
			return &orchestration.SessionStatus{
				SessionID: id,
				State:     orchestration.SessionStateActive,
			}, nil
		},
	}
	_, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
	defer cleanup()

	resp, err := client.Status(context.Background(), &proto.SessionStatusRequest{
		TenantId:  "tenant-a",
		SessionId: "sess-found",
	})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if resp.GetSessionId() != "sess-found" {
		t.Errorf("session_id: got %q, want %q", resp.GetSessionId(), "sess-found")
	}
}

// =============================================================================
// GRPCSpawnServer — Spawn with sandbox mode set
// =============================================================================

// TestGRPCSpawnServer_Spawn_WithSandboxModeNone covers the sandboxMode proto
// translation inside Spawn, exercising the protoMCPServersToRuntime nil path.
func TestGRPCSpawnServer_Spawn_WithSandboxModeNone(t *testing.T) {
	t.Parallel()
	_, client, cleanup := buildSpawnServer(t, &spawnBackend{}, newTestIdentity())
	defer cleanup()

	resp, err := client.Spawn(context.Background(), &proto.SpawnSessionRequest{
		Backend:     "acp",
		TenantId:    "tenant-a",
		SandboxMode: proto.SandboxMode_SANDBOX_MODE_NONE,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if resp.GetSessionId() == "" {
		t.Fatal("expected non-empty session_id")
	}
}
