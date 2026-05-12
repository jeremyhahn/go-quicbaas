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

package agent

import (
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	proto "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

const spawnServerBufSize = 1024 * 1024

// spawnBackend is a configurable AgentBackend for RPC-level tests. Each
// test installs only the callbacks it needs; the rest return zero-value
// responses.
type spawnBackend struct {
	spawnCalls  atomic.Int32
	promptCalls atomic.Int32
	statusCalls atomic.Int32
	closeCalls  atomic.Int32

	spawnFn  func(ctx context.Context, req *orchestration.SpawnRequest) (*orchestration.Session, error)
	promptFn func(ctx context.Context, req *orchestration.PromptRequest) (*orchestration.PromptResponse, error)
	statusFn func(ctx context.Context, id string) (*orchestration.SessionStatus, error)
	closeFn  func(ctx context.Context, id string) error
}

func (m *spawnBackend) Spawn(ctx context.Context, req *orchestration.SpawnRequest) (*orchestration.Session, error) {
	m.spawnCalls.Add(1)
	if m.spawnFn != nil {
		return m.spawnFn(ctx, req)
	}
	return &orchestration.Session{
		ID:        "sess-1",
		ProjectID: req.ProjectID,
		Backend:   "acp",
		Model:     req.Model,
		Status:    orchestration.SessionStateActive,
		WorkDir:   req.WorkDir,
		CreatedAt: time.Now().UnixNano(),
	}, nil
}

func (m *spawnBackend) Prompt(ctx context.Context, req *orchestration.PromptRequest) (*orchestration.PromptResponse, error) {
	m.promptCalls.Add(1)
	if m.promptFn != nil {
		return m.promptFn(ctx, req)
	}
	return &orchestration.PromptResponse{
		SessionID:  req.SessionID,
		Content:    []orchestration.ContentBlock{{Type: "text", Text: "ok"}},
		Model:      req.Model,
		StopReason: "end_turn",
	}, nil
}

func (m *spawnBackend) Status(ctx context.Context, id string) (*orchestration.SessionStatus, error) {
	m.statusCalls.Add(1)
	if m.statusFn != nil {
		return m.statusFn(ctx, id)
	}
	return &orchestration.SessionStatus{
		SessionID:    id,
		State:        orchestration.SessionStateActive,
		LastActivity: time.Now().Unix(),
		Uptime:       42,
	}, nil
}

func (m *spawnBackend) Close(ctx context.Context, id string) error {
	m.closeCalls.Add(1)
	if m.closeFn != nil {
		return m.closeFn(ctx, id)
	}
	return nil
}

func (m *spawnBackend) Capabilities() orchestration.BackendCapabilities {
	return orchestration.BackendCapabilities{Kind: orchestration.BackendKindAgent}
}

func (m *spawnBackend) Type() string         { return "acp" }
func (m *spawnBackend) ToolGuidance() string { return "" }

// buildSpawnServer stands up a GRPCSpawnServer over bufconn with the given
// backend and identity wired in. Returns the server, a connected gRPC client,
// and a cleanup function.
func buildSpawnServer(t *testing.T, backend orchestration.AgentBackend, identity *orchestration.AgentIdentity) (*GRPCSpawnServer, proto.AgentRuntimeClient, func()) {
	t.Helper()

	rt, err := NewRuntime(RuntimeConfig{
		Backends:      map[string]orchestration.AgentBackend{"acp": backend},
		SandboxPolicy: orchestration.SandboxPolicyOptIn,
		Logger:        &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}

	lis := bufconn.Listen(spawnServerBufSize)
	srv, err := NewGRPCSpawnServer(GRPCSpawnServerDeps{
		Runtime:          rt,
		BindAddr:         "bufnet",
		Logger:           &testLogger{t: t},
		ListenerOverride: lis,
	})
	if err != nil {
		t.Fatalf("NewGRPCSpawnServer: %v", err)
	}
	if identity != nil {
		srv.SetIdentity(identity)
	}

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
			return lis.Dial()
		}),
	)
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}

	client := proto.NewAgentRuntimeClient(conn)
	cleanup := func() {
		conn.Close()
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Stop(stopCtx)
	}
	return srv, client, cleanup
}

func newTestIdentity() *orchestration.AgentIdentity {
	return &orchestration.AgentIdentity{
		AgentID:          "agent-test",
		ServiceAccountID: "sa-test",
		TenantID:         "tenant-a",
		Role:             "worker",
		Scopes: []string{
			orchestration.AgentScopeRuntimeSpawn,
			orchestration.AgentScopeRuntimePrompt,
			orchestration.AgentScopeRuntimeStatus,
		},
		ClientID: "client-test",
	}
}

// =============================================================================
// Constructor guard tests
// =============================================================================

func TestNewGRPCSpawnServer_RequiresRuntime(t *testing.T) {
	t.Parallel()
	_, err := NewGRPCSpawnServer(GRPCSpawnServerDeps{
		BindAddr: "127.0.0.1:0",
		Logger:   &testLogger{t: t},
	})
	if !errors.Is(err, ErrSpawnServerNoRuntime) {
		t.Fatalf("expected ErrSpawnServerNoRuntime, got %v", err)
	}
}

func TestNewGRPCSpawnServer_RequiresLogger(t *testing.T) {
	t.Parallel()
	rt, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"acp": &spawnBackend{}},
		Logger:   &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	_, err = NewGRPCSpawnServer(GRPCSpawnServerDeps{
		Runtime:  rt,
		BindAddr: "127.0.0.1:0",
	})
	if !errors.Is(err, ErrSpawnServerNoLogger) {
		t.Fatalf("expected ErrSpawnServerNoLogger, got %v", err)
	}
}

func TestNewGRPCSpawnServer_RequiresBindAddrOrListener(t *testing.T) {
	t.Parallel()
	rt, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"acp": &spawnBackend{}},
		Logger:   &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	_, err = NewGRPCSpawnServer(GRPCSpawnServerDeps{
		Runtime: rt,
		Logger:  &testLogger{t: t},
	})
	if !errors.Is(err, ErrSpawnServerNoBindAddr) {
		t.Fatalf("expected ErrSpawnServerNoBindAddr, got %v", err)
	}
}

// =============================================================================
// Authz boundary tests — enrollment, tenant, scope
// =============================================================================

func TestGRPCSpawnServer_RejectsRPCsBeforeEnrollment(t *testing.T) {
	t.Parallel()
	_, client, cleanup := buildSpawnServer(t, &spawnBackend{}, nil)
	defer cleanup()

	_, err := client.Spawn(context.Background(), &proto.SpawnSessionRequest{
		Backend:  "acp",
		TenantId: "tenant-a",
	})
	if err == nil {
		t.Fatal("expected error before enrollment")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Unauthenticated {
		t.Fatalf("expected Unauthenticated, got %v", err)
	}
}

func TestGRPCSpawnServer_RejectsTenantMismatch(t *testing.T) {
	t.Parallel()
	_, client, cleanup := buildSpawnServer(t, &spawnBackend{}, newTestIdentity())
	defer cleanup()

	_, err := client.Spawn(context.Background(), &proto.SpawnSessionRequest{
		Backend:  "acp",
		TenantId: "wrong-tenant",
	})
	if err == nil {
		t.Fatal("expected tenant mismatch error")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied, got %v", st.Code())
	}
}

func TestGRPCSpawnServer_RejectsMissingScope(t *testing.T) {
	t.Parallel()
	// Identity is missing the spawn scope.
	id := newTestIdentity()
	id.Scopes = []string{orchestration.AgentScopeRuntimePrompt, orchestration.AgentScopeRuntimeStatus}

	_, client, cleanup := buildSpawnServer(t, &spawnBackend{}, id)
	defer cleanup()

	_, err := client.Spawn(context.Background(), &proto.SpawnSessionRequest{
		Backend:  "acp",
		TenantId: "tenant-a",
	})
	if err == nil {
		t.Fatal("expected missing-scope error")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied, got %v (%s)", st.Code(), st.Message())
	}
}

// =============================================================================
// Happy-path round-trip tests
// =============================================================================

func TestGRPCSpawnServer_SpawnRoundTrip(t *testing.T) {
	t.Parallel()
	backend := &spawnBackend{}
	_, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
	defer cleanup()

	resp, err := client.Spawn(context.Background(), &proto.SpawnSessionRequest{
		Backend:   "acp",
		TenantId:  "tenant-a",
		ProjectId: "proj-1",
		Model:     "claude-3.5",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if resp.GetSessionId() == "" {
		t.Fatal("expected session_id in response")
	}
	if backend.spawnCalls.Load() != 1 {
		t.Fatalf("expected 1 backend spawn call, got %d", backend.spawnCalls.Load())
	}
}

func TestGRPCSpawnServer_PromptRoundTrip(t *testing.T) {
	t.Parallel()
	backend := &spawnBackend{
		promptFn: func(ctx context.Context, req *orchestration.PromptRequest) (*orchestration.PromptResponse, error) {
			return &orchestration.PromptResponse{
				SessionID:  req.SessionID,
				Content:    []orchestration.ContentBlock{{Type: "text", Text: "hello"}},
				Model:      "claude-3.5",
				StopReason: "end_turn",
			}, nil
		},
	}
	_, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
	defer cleanup()

	stream, err := client.Prompt(context.Background(), &proto.PromptSessionRequest{
		Backend:   "acp",
		TenantId:  "tenant-a",
		SessionId: "sess-1",
		Prompt:    "hi",
	})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	frame, err := stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv: %v", err)
	}
	if got := frame.GetSessionId(); got != "sess-1" {
		t.Fatalf("session_id: got %q want sess-1", got)
	}
	if len(frame.GetContent()) != 1 || frame.GetContent()[0].GetText() != "hello" {
		t.Fatalf("content mismatch: %+v", frame.GetContent())
	}

	// Server should now close the stream.
	if _, err := stream.Recv(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF after single frame, got %v", err)
	}
	if backend.promptCalls.Load() != 1 {
		t.Fatalf("expected 1 backend prompt call, got %d", backend.promptCalls.Load())
	}
}

func TestGRPCSpawnServer_StatusRoundTrip(t *testing.T) {
	t.Parallel()
	backend := &spawnBackend{}
	_, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
	defer cleanup()

	resp, err := client.Status(context.Background(), &proto.SessionStatusRequest{
		SessionId: "sess-1",
		TenantId:  "tenant-a",
	})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if resp.GetState() != proto.SessionState_SESSION_STATE_ACTIVE {
		t.Fatalf("expected active, got %v", resp.GetState())
	}
}

func TestGRPCSpawnServer_CloseRoundTrip(t *testing.T) {
	t.Parallel()
	backend := &spawnBackend{}
	_, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
	defer cleanup()

	_, err := client.Close(context.Background(), &proto.CloseSessionRequest{
		SessionId: "sess-1",
		TenantId:  "tenant-a",
	})
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if backend.closeCalls.Load() == 0 {
		t.Fatal("expected backend close to be called")
	}
}

// =============================================================================
// Domain error mapping
// =============================================================================

func TestGRPCSpawnServer_SandboxViolationMapsToFailedPrecondition(t *testing.T) {
	t.Parallel()
	backend := &spawnBackend{}
	// Configure the runtime with SandboxPolicyRequired but no container
	// runtime — any Spawn with SandboxModeRequired will violate.
	rt, err := NewRuntime(RuntimeConfig{
		Backends:      map[string]orchestration.AgentBackend{"acp": backend},
		SandboxPolicy: orchestration.SandboxPolicyRequired,
		Logger:        &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}

	lis := bufconn.Listen(spawnServerBufSize)
	srv, err := NewGRPCSpawnServer(GRPCSpawnServerDeps{
		Runtime:          rt,
		BindAddr:         "bufnet",
		Logger:           &testLogger{t: t},
		ListenerOverride: lis,
	})
	if err != nil {
		t.Fatalf("spawn server: %v", err)
	}
	srv.SetIdentity(newTestIdentity())
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Stop(ctx)
	}()

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
			return lis.Dial()
		}),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	client := proto.NewAgentRuntimeClient(conn)
	_, err = client.Spawn(context.Background(), &proto.SpawnSessionRequest{
		Backend:     "acp",
		TenantId:    "tenant-a",
		SandboxMode: proto.SandboxMode_SANDBOX_MODE_REQUIRED,
	})
	if err == nil {
		t.Fatal("expected sandbox violation error")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.FailedPrecondition {
		t.Fatalf("expected FailedPrecondition, got %v (%s)", st.Code(), st.Message())
	}
}

func TestGRPCSpawnServer_UnknownBackendMapsToNotFound(t *testing.T) {
	t.Parallel()
	_, client, cleanup := buildSpawnServer(t, &spawnBackend{}, newTestIdentity())
	defer cleanup()

	_, err := client.Spawn(context.Background(), &proto.SpawnSessionRequest{
		Backend:  "does-not-exist",
		TenantId: "tenant-a",
	})
	if err == nil {
		t.Fatal("expected unknown-backend error")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.NotFound {
		t.Fatalf("expected NotFound, got %v (%s)", st.Code(), st.Message())
	}
}

// =============================================================================
// Addr / lifecycle
// =============================================================================

func TestGRPCSpawnServer_AddrBeforeStart(t *testing.T) {
	t.Parallel()
	rt, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"acp": &spawnBackend{}},
		Logger:   &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	srv, err := NewGRPCSpawnServer(GRPCSpawnServerDeps{
		Runtime:  rt,
		BindAddr: "127.0.0.1:0",
		Logger:   &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("spawn server: %v", err)
	}
	if got := srv.Addr(); got != "" {
		t.Fatalf("expected empty Addr before Start, got %q", got)
	}
}

func TestGRPCSpawnServer_StopIsIdempotent(t *testing.T) {
	t.Parallel()
	rt, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"acp": &spawnBackend{}},
		Logger:   &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	lis := bufconn.Listen(spawnServerBufSize)
	srv, err := NewGRPCSpawnServer(GRPCSpawnServerDeps{
		Runtime:          rt,
		BindAddr:         "bufnet",
		Logger:           &testLogger{t: t},
		ListenerOverride: lis,
	})
	if err != nil {
		t.Fatalf("spawn server: %v", err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Stop(ctx); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := srv.Stop(ctx); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}
