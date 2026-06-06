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

package orchestration

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"

	proto "github.com/jeremyhahn/go-quicbaas/api/proto"
)

const remoteBackendBufSize = 1024 * 1024

// fakeDaemonServer is a minimal in-memory AgentRuntimeServer for exercising
// the RemoteACPBackend client path. Each RPC records call counts and returns
// the configured response / error.
type fakeDaemonServer struct {
	proto.UnimplementedAgentRuntimeServer

	spawnCalls      atomic.Int32
	promptCalls     atomic.Int32
	statusCalls     atomic.Int32
	closeCalls      atomic.Int32
	listModelsCalls atomic.Int32

	spawnResp      *proto.SpawnSessionResponse
	spawnErr       error
	promptResp     *proto.PromptSessionResponse
	promptErr      error
	statusResp     *proto.SessionStatusResponse
	statusErr      error
	closeResp      *proto.CloseSessionResponse
	closeErr       error
	listModelsResp *proto.ListModelsResponse
	listModelsErr  error
}

func (f *fakeDaemonServer) Spawn(ctx context.Context, req *proto.SpawnSessionRequest) (*proto.SpawnSessionResponse, error) {
	f.spawnCalls.Add(1)
	if f.spawnErr != nil {
		return nil, f.spawnErr
	}
	if f.spawnResp != nil {
		return f.spawnResp, nil
	}
	return &proto.SpawnSessionResponse{
		SessionId: "daemon-sess-1",
		Backend:   req.GetBackend(),
		Model:     req.GetModel(),
		Timestamp: timestamppb.Now(),
	}, nil
}

func (f *fakeDaemonServer) Prompt(req *proto.PromptSessionRequest, stream grpc.ServerStreamingServer[proto.PromptSessionResponse]) error {
	f.promptCalls.Add(1)
	if f.promptErr != nil {
		return f.promptErr
	}
	resp := f.promptResp
	if resp == nil {
		resp = &proto.PromptSessionResponse{
			SessionId:  req.GetSessionId(),
			Content:    []*proto.ContentBlock{{Type: "text", Text: "hello from daemon"}},
			Model:      req.GetModel(),
			StopReason: "end_turn",
			Timestamp:  timestamppb.Now(),
		}
	}
	return stream.Send(resp)
}

func (f *fakeDaemonServer) Status(ctx context.Context, req *proto.SessionStatusRequest) (*proto.SessionStatusResponse, error) {
	f.statusCalls.Add(1)
	if f.statusErr != nil {
		return nil, f.statusErr
	}
	if f.statusResp != nil {
		return f.statusResp, nil
	}
	return &proto.SessionStatusResponse{
		SessionId:     req.GetSessionId(),
		State:         proto.SessionState_SESSION_STATE_ACTIVE,
		LastActivity:  timestamppb.Now(),
		UptimeSeconds: 30,
		Timestamp:     timestamppb.Now(),
	}, nil
}

func (f *fakeDaemonServer) Close(ctx context.Context, req *proto.CloseSessionRequest) (*proto.CloseSessionResponse, error) {
	f.closeCalls.Add(1)
	if f.closeErr != nil {
		return nil, f.closeErr
	}
	if f.closeResp != nil {
		return f.closeResp, nil
	}
	return &proto.CloseSessionResponse{
		SessionId: req.GetSessionId(),
		Timestamp: timestamppb.Now(),
	}, nil
}

func (f *fakeDaemonServer) ListModels(ctx context.Context, req *proto.ListModelsRequest) (*proto.ListModelsResponse, error) {
	f.listModelsCalls.Add(1)
	if f.listModelsErr != nil {
		return nil, f.listModelsErr
	}
	if f.listModelsResp != nil {
		return f.listModelsResp, nil
	}
	return &proto.ListModelsResponse{
		Backend: req.GetBackend(),
		Models:  []string{"default-model:latest"},
	}, nil
}

// bufconnDaemon wires a bufconn listener + grpc.Server + fakeDaemonServer
// together. Callers get the dialer closure the backend uses to reach it.
type bufconnDaemon struct {
	lis    *bufconn.Listener
	server *grpc.Server
	svc    *fakeDaemonServer
}

func startBufconnDaemon(t *testing.T, svc *fakeDaemonServer) *bufconnDaemon {
	t.Helper()
	if svc == nil {
		svc = &fakeDaemonServer{}
	}
	lis := bufconn.Listen(remoteBackendBufSize)
	server := grpc.NewServer()
	proto.RegisterAgentRuntimeServer(server, svc)
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(func() {
		server.Stop()
	})
	return &bufconnDaemon{lis: lis, server: server, svc: svc}
}

func (d *bufconnDaemon) dialer() func(ctx context.Context, target string) (*grpc.ClientConn, error) {
	return func(ctx context.Context, target string) (*grpc.ClientConn, error) {
		return grpc.NewClient(
			"passthrough:///"+target,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
				return d.lis.Dial()
			}),
		)
	}
}

func newDaemonRegistration() *AgentRegistration {
	return &AgentRegistration{
		ID:       "agent-remote",
		TenantID: "tenant-a",
		Name:     "remote-daemon",
		Host:     "daemon.test:9091",
		Backend:  "acp",
		Kind:     AgentKindDaemon,
		Status:   AgentStatusRunning,
	}
}

// =============================================================================
// Constructor guards
// =============================================================================

func TestNewRemoteACPBackend_RequiresRegistration(t *testing.T) {
	t.Parallel()
	_, err := NewRemoteACPBackend(RemoteACPBackendDeps{Logger: &agentTestLogger{}})
	if !errors.Is(err, ErrRemoteBackendNoRegistration) {
		t.Fatalf("expected ErrRemoteBackendNoRegistration, got %v", err)
	}
}

func TestNewRemoteACPBackend_RequiresLogger(t *testing.T) {
	t.Parallel()
	_, err := NewRemoteACPBackend(RemoteACPBackendDeps{Registration: newDaemonRegistration()})
	if !errors.Is(err, ErrRemoteBackendNoLogger) {
		t.Fatalf("expected ErrRemoteBackendNoLogger, got %v", err)
	}
}

func TestNewRemoteACPBackend_RequiresHost(t *testing.T) {
	t.Parallel()
	reg := newDaemonRegistration()
	reg.Host = ""
	_, err := NewRemoteACPBackend(RemoteACPBackendDeps{
		Registration: reg,
		Logger:       &agentTestLogger{},
	})
	if !errors.Is(err, ErrRemoteBackendNoHost) {
		t.Fatalf("expected ErrRemoteBackendNoHost, got %v", err)
	}
}

func TestNewRemoteACPBackend_RejectsNonDaemonKind(t *testing.T) {
	t.Parallel()
	reg := newDaemonRegistration()
	reg.Kind = AgentKindContainerScript
	_, err := NewRemoteACPBackend(RemoteACPBackendDeps{
		Registration: reg,
		Logger:       &agentTestLogger{},
	})
	if !errors.Is(err, ErrRemoteBackendNotDaemon) {
		t.Fatalf("expected ErrRemoteBackendNotDaemon, got %v", err)
	}
}

func TestNewRemoteACPBackend_AcceptsEmbeddedKind(t *testing.T) {
	t.Parallel()
	reg := newDaemonRegistration()
	reg.Kind = AgentKindEmbedded
	_, err := NewRemoteACPBackend(RemoteACPBackendDeps{
		Registration: reg,
		Logger:       &agentTestLogger{},
	})
	if err != nil {
		t.Fatalf("expected AgentKindEmbedded to be accepted by NewRemoteACPBackend, got: %v", err)
	}
}

// =============================================================================
// Happy-path RPC round-trips
// =============================================================================

func TestRemoteACPBackend_SpawnRoundTrip(t *testing.T) {
	t.Parallel()
	daemon := startBufconnDaemon(t, nil)
	backend, err := NewRemoteACPBackend(RemoteACPBackendDeps{
		Registration: newDaemonRegistration(),
		Logger:       &agentTestLogger{},
		ConnDialer:   daemon.dialer(),
	})
	if err != nil {
		t.Fatalf("NewRemoteACPBackend: %v", err)
	}
	t.Cleanup(func() { _ = backend.CloseAll() })

	session, err := backend.Spawn(context.Background(), &SpawnRequest{
		ProjectID: "proj-1",
		WorkDir:   "/workspace",
		Model:     "claude-3.5",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if session.ID != "daemon-sess-1" {
		t.Fatalf("session ID: got %q want daemon-sess-1", session.ID)
	}
	if daemon.svc.spawnCalls.Load() != 1 {
		t.Fatalf("expected 1 spawn call on daemon, got %d", daemon.svc.spawnCalls.Load())
	}
}

func TestRemoteACPBackend_PromptAccumulatesStream(t *testing.T) {
	t.Parallel()
	daemon := startBufconnDaemon(t, &fakeDaemonServer{
		promptResp: &proto.PromptSessionResponse{
			SessionId:  "sess-1",
			Content:    []*proto.ContentBlock{{Type: "text", Text: "hi"}, {Type: "text", Text: "there"}},
			Model:      "m",
			StopReason: "end_turn",
		},
	})
	backend, err := NewRemoteACPBackend(RemoteACPBackendDeps{
		Registration: newDaemonRegistration(),
		Logger:       &agentTestLogger{},
		ConnDialer:   daemon.dialer(),
	})
	if err != nil {
		t.Fatalf("NewRemoteACPBackend: %v", err)
	}
	t.Cleanup(func() { _ = backend.CloseAll() })

	resp, err := backend.Prompt(context.Background(), &PromptRequest{
		SessionID: "sess-1",
		Prompt:    "hi",
	})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if resp.SessionID != "sess-1" {
		t.Fatalf("session ID: got %q", resp.SessionID)
	}
	if len(resp.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(resp.Content))
	}
	if resp.StopReason != "end_turn" {
		t.Fatalf("stop_reason: got %q", resp.StopReason)
	}
}

func TestRemoteACPBackend_PromptEmptyStreamFails(t *testing.T) {
	t.Parallel()
	daemon := startBufconnDaemon(t, &fakeDaemonServer{
		promptErr: status.Error(codes.OK, ""), // nil-style; but empty body
	})
	// Override: we need an empty-but-successful stream. Use a server that
	// sends nothing and returns nil. Replace promptErr with a nil-returning
	// override via an emptyPromptServer.
	daemon.server.Stop()
	lis := bufconn.Listen(remoteBackendBufSize)
	server := grpc.NewServer()
	proto.RegisterAgentRuntimeServer(server, &emptyPromptServer{})
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(func() { server.Stop() })

	backend, err := NewRemoteACPBackend(RemoteACPBackendDeps{
		Registration: newDaemonRegistration(),
		Logger:       &agentTestLogger{},
		ConnDialer: func(ctx context.Context, target string) (*grpc.ClientConn, error) {
			return grpc.NewClient(
				"passthrough:///"+target,
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
					return lis.Dial()
				}),
			)
		},
	})
	if err != nil {
		t.Fatalf("NewRemoteACPBackend: %v", err)
	}
	t.Cleanup(func() { _ = backend.CloseAll() })

	_, err = backend.Prompt(context.Background(), &PromptRequest{Prompt: "hi"})
	if !errors.Is(err, ErrRemoteBackendEmptyStream) {
		t.Fatalf("expected ErrRemoteBackendEmptyStream, got %v", err)
	}
}

// emptyPromptServer returns an empty stream for Prompt; other methods are
// unimplemented.
type emptyPromptServer struct {
	proto.UnimplementedAgentRuntimeServer
}

func (*emptyPromptServer) Prompt(req *proto.PromptSessionRequest, stream grpc.ServerStreamingServer[proto.PromptSessionResponse]) error {
	return nil
}

func TestRemoteACPBackend_StatusRoundTrip(t *testing.T) {
	t.Parallel()
	daemon := startBufconnDaemon(t, nil)
	backend, err := NewRemoteACPBackend(RemoteACPBackendDeps{
		Registration: newDaemonRegistration(),
		Logger:       &agentTestLogger{},
		ConnDialer:   daemon.dialer(),
	})
	if err != nil {
		t.Fatalf("NewRemoteACPBackend: %v", err)
	}
	t.Cleanup(func() { _ = backend.CloseAll() })

	st, err := backend.Status(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.State != SessionStateActive {
		t.Fatalf("state: got %q want active", st.State)
	}
	if st.Uptime != 30 {
		t.Fatalf("uptime: got %d want 30", st.Uptime)
	}
}

func TestRemoteACPBackend_CloseRoundTrip(t *testing.T) {
	t.Parallel()
	daemon := startBufconnDaemon(t, nil)
	backend, err := NewRemoteACPBackend(RemoteACPBackendDeps{
		Registration: newDaemonRegistration(),
		Logger:       &agentTestLogger{},
		ConnDialer:   daemon.dialer(),
	})
	if err != nil {
		t.Fatalf("NewRemoteACPBackend: %v", err)
	}
	t.Cleanup(func() { _ = backend.CloseAll() })

	if err := backend.Close(context.Background(), "sess-1"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if daemon.svc.closeCalls.Load() != 1 {
		t.Fatalf("expected 1 close call, got %d", daemon.svc.closeCalls.Load())
	}
}

// =============================================================================
// Connection reuse + pool lifecycle
// =============================================================================

func TestRemoteACPBackend_ReusesConnectionAcrossCalls(t *testing.T) {
	t.Parallel()
	daemon := startBufconnDaemon(t, nil)

	var dialCalls atomic.Int32
	reuseDialer := func(ctx context.Context, target string) (*grpc.ClientConn, error) {
		dialCalls.Add(1)
		return daemon.dialer()(ctx, target)
	}

	backend, err := NewRemoteACPBackend(RemoteACPBackendDeps{
		Registration: newDaemonRegistration(),
		Logger:       &agentTestLogger{},
		ConnDialer:   reuseDialer,
	})
	if err != nil {
		t.Fatalf("NewRemoteACPBackend: %v", err)
	}
	t.Cleanup(func() { _ = backend.CloseAll() })

	// Multiple calls against the same host should dial exactly once.
	for i := 0; i < 5; i++ {
		if _, err := backend.Spawn(context.Background(), &SpawnRequest{}); err != nil {
			t.Fatalf("Spawn[%d]: %v", i, err)
		}
	}
	if got := dialCalls.Load(); got != 1 {
		t.Fatalf("expected 1 dial, got %d", got)
	}
	if got := daemon.svc.spawnCalls.Load(); got != 5 {
		t.Fatalf("expected 5 spawn calls, got %d", got)
	}
}

func TestRemoteACPBackend_HandlesDaemonDisconnect(t *testing.T) {
	t.Parallel()
	daemon := startBufconnDaemon(t, nil)
	backend, err := NewRemoteACPBackend(RemoteACPBackendDeps{
		Registration: newDaemonRegistration(),
		Logger:       &agentTestLogger{},
		ConnDialer:   daemon.dialer(),
	})
	if err != nil {
		t.Fatalf("NewRemoteACPBackend: %v", err)
	}
	t.Cleanup(func() { _ = backend.CloseAll() })

	// Prime the conn pool with a good call.
	if _, err := backend.Spawn(context.Background(), &SpawnRequest{}); err != nil {
		t.Fatalf("initial Spawn: %v", err)
	}

	// Abruptly kill the daemon's server.
	daemon.server.Stop()

	// Subsequent call should surface a RemoteBackendRPCError wrapping the
	// underlying gRPC failure.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = backend.Spawn(ctx, &SpawnRequest{})
	if err == nil {
		t.Fatal("expected RPC error after daemon shutdown")
	}
	var rpcErr *RemoteBackendRPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected RemoteBackendRPCError, got %T: %v", err, err)
	}
}

// =============================================================================
// Nil-request validation
// =============================================================================

func TestRemoteACPBackend_SpawnNilRequest(t *testing.T) {
	t.Parallel()
	daemon := startBufconnDaemon(t, nil)
	backend, err := NewRemoteACPBackend(RemoteACPBackendDeps{
		Registration: newDaemonRegistration(),
		Logger:       &agentTestLogger{},
		ConnDialer:   daemon.dialer(),
	})
	if err != nil {
		t.Fatalf("NewRemoteACPBackend: %v", err)
	}
	t.Cleanup(func() { _ = backend.CloseAll() })

	_, err = backend.Spawn(context.Background(), nil)
	var ve *InputValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected InputValidationError, got %v", err)
	}
}

func TestRemoteACPBackend_PromptNilRequest(t *testing.T) {
	t.Parallel()
	daemon := startBufconnDaemon(t, nil)
	backend, err := NewRemoteACPBackend(RemoteACPBackendDeps{
		Registration: newDaemonRegistration(),
		Logger:       &agentTestLogger{},
		ConnDialer:   daemon.dialer(),
	})
	if err != nil {
		t.Fatalf("NewRemoteACPBackend: %v", err)
	}
	t.Cleanup(func() { _ = backend.CloseAll() })

	_, err = backend.Prompt(context.Background(), nil)
	var ve *InputValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected InputValidationError, got %v", err)
	}
}

// =============================================================================
// Capabilities / Type / ToolGuidance
// =============================================================================

func TestRemoteACPBackend_CapabilitiesAndType(t *testing.T) {
	t.Parallel()
	daemon := startBufconnDaemon(t, nil)
	backend, err := NewRemoteACPBackend(RemoteACPBackendDeps{
		Registration: newDaemonRegistration(),
		Logger:       &agentTestLogger{},
		ConnDialer:   daemon.dialer(),
	})
	if err != nil {
		t.Fatalf("NewRemoteACPBackend: %v", err)
	}
	t.Cleanup(func() { _ = backend.CloseAll() })

	caps := backend.Capabilities()
	if caps.Kind != BackendKindAgent {
		t.Fatalf("kind: got %q want %q", caps.Kind, BackendKindAgent)
	}
	if !caps.PersistentSessions {
		t.Fatal("expected persistent sessions")
	}
	if backend.Type() != "remote_acp" {
		t.Fatalf("type: got %q", backend.Type())
	}
	if backend.ToolGuidance() != "" {
		t.Fatalf("expected empty tool guidance, got %q", backend.ToolGuidance())
	}
	if backend.Registration() == nil {
		t.Fatal("expected registration pointer")
	}
}

// =============================================================================
// ListModels — proxies to the remote daemon
// =============================================================================

// TestRemoteACPBackend_ListModels_ProxiesToDaemon verifies that ListModels
// issues the ListModels RPC to the remote daemon and returns the model list.
func TestRemoteACPBackend_ListModels_ProxiesToDaemon(t *testing.T) {
	t.Parallel()

	svc := &fakeDaemonServer{
		listModelsResp: &proto.ListModelsResponse{
			Backend: "ollama",
			Models:  []string{"qwen3:1.7b", "llama3.2:1b"},
		},
	}
	daemon := startBufconnDaemon(t, svc)
	reg := newDaemonRegistration()
	reg.Backend = "ollama"
	backend, err := NewRemoteACPBackend(RemoteACPBackendDeps{
		Registration: reg,
		Logger:       &agentTestLogger{},
		ConnDialer:   daemon.dialer(),
	})
	if err != nil {
		t.Fatalf("NewRemoteACPBackend: %v", err)
	}
	t.Cleanup(func() { _ = backend.CloseAll() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	models, err := backend.ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels error: %v", err)
	}
	if svc.listModelsCalls.Load() != 1 {
		t.Errorf("expected 1 ListModels RPC call, got %d", svc.listModelsCalls.Load())
	}
	if len(models) != 2 {
		t.Fatalf("model count: got %d, want 2", len(models))
	}
	if models[0] != "qwen3:1.7b" {
		t.Errorf("models[0]: got %q, want %q", models[0], "qwen3:1.7b")
	}
}

// TestRemoteACPBackend_ListModels_DaemonError verifies that a daemon-side
// error is wrapped in a RemoteBackendRPCError and returned to the caller.
func TestRemoteACPBackend_ListModels_DaemonError(t *testing.T) {
	t.Parallel()

	svc := &fakeDaemonServer{
		listModelsErr: status.Error(codes.Internal, "ollama not reachable"),
	}
	daemon := startBufconnDaemon(t, svc)
	reg := newDaemonRegistration()
	reg.Backend = "ollama"
	backend, err := NewRemoteACPBackend(RemoteACPBackendDeps{
		Registration: reg,
		Logger:       &agentTestLogger{},
		ConnDialer:   daemon.dialer(),
	})
	if err != nil {
		t.Fatalf("NewRemoteACPBackend: %v", err)
	}
	t.Cleanup(func() { _ = backend.CloseAll() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = backend.ListModels(ctx)
	if err == nil {
		t.Fatal("expected error from ListModels, got nil")
	}
	var rpcErr *RemoteBackendRPCError
	if !errors.As(err, &rpcErr) {
		t.Errorf("expected RemoteBackendRPCError, got %T: %v", err, err)
	}
}
