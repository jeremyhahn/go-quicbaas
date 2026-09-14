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
	"strings"
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

// TestGRPCSpawnServer_SpawnCarriesExtraArgsToTheBackend closes the remote half
// of go-quicbaas#218.
//
// The SDLC module states its worker's permission grant on the spawn. The wire
// message had no field for it, so the control plane's grant was dropped at the
// daemon boundary: a LOCAL worker got it and a REMOTE one was spawned exactly
// the way the fault was measured, with nothing to indicate the difference.
func TestGRPCSpawnServer_SpawnCarriesExtraArgsToTheBackend(t *testing.T) {
	t.Parallel()
	var got []string
	backend := &spawnBackend{
		spawnFn: func(_ context.Context, req *orchestration.SpawnRequest) (*orchestration.Session, error) {
			got = append([]string(nil), req.ExtraArgs...)
			return &orchestration.Session{ID: "sess-1", Backend: "acp", Status: orchestration.SessionStateActive}, nil
		},
	}
	_, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
	defer cleanup()

	if _, err := client.Spawn(context.Background(), &proto.SpawnSessionRequest{
		Backend:   "acp",
		TenantId:  "tenant-a",
		ProjectId: "proj-1",
		ExtraArgs: []string{"--permission-mode", "acceptEdits"},
	}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if len(got) != 2 || got[0] != "--permission-mode" || got[1] != "acceptEdits" {
		t.Fatalf("backend received ExtraArgs %v, want the permission grant the caller stated", got)
	}
}

// TestGRPCSpawnServer_SpawnCarriesTheAccountSelectionToTheBackend closes the
// remote half of go-quicbaas#235, and it is the same failure shape as the one
// above.
//
// The credential a worker runs on cannot cross the wire — it lives in this
// daemon's own secret store — so what crosses is the REQUEST, and this daemon
// resolves it. The wire message carried the fields and the daemon dropped them
// when it rebuilt the SpawnRequest, which does not fail: the backend only
// consults its AccountResolver when AccountID or AccountAuto is set, so a
// remote worker started with whatever ambient identity the daemon had and
// nothing said the caller's choice had been discarded.
func TestGRPCSpawnServer_SpawnCarriesTheAccountSelectionToTheBackend(t *testing.T) {
	t.Parallel()
	var got *orchestration.SpawnRequest
	backend := &spawnBackend{
		spawnFn: func(_ context.Context, req *orchestration.SpawnRequest) (*orchestration.Session, error) {
			copied := *req
			got = &copied
			return &orchestration.Session{ID: "sess-1", Backend: "acp", Status: orchestration.SessionStateActive}, nil
		},
	}
	_, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
	defer cleanup()

	if _, err := client.Spawn(context.Background(), &proto.SpawnSessionRequest{
		Backend:         "acp",
		TenantId:        "tenant-a",
		ProjectId:       "proj-1",
		AccountAuto:     true,
		AccountStrategy: "least_usage",
		AccountAllow:    []string{"acct-1", "acct-2"},
	}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if got == nil {
		t.Fatal("the backend was never spawned")
	}
	if !got.AccountAuto {
		t.Error("the daemon dropped AccountAuto; the backend never consults the pool " +
			"unless it is set, so the worker runs on whatever identity the daemon has")
	}
	if got.AccountStrategy != "least_usage" {
		t.Errorf("backend received AccountStrategy %q, want least_usage", got.AccountStrategy)
	}
	if len(got.AccountAllow) != 2 {
		t.Errorf("backend received AccountAllow %v, want the caller's rotation pool", got.AccountAllow)
	}
}

// TestGRPCSpawnServer_SpawnCarriesAPinnedAccountToTheBackend is the pinned
// counterpart: a caller that named one account must not have it silently
// replaced by whatever the daemon would otherwise have used.
func TestGRPCSpawnServer_SpawnCarriesAPinnedAccountToTheBackend(t *testing.T) {
	t.Parallel()
	var got *orchestration.SpawnRequest
	backend := &spawnBackend{
		spawnFn: func(_ context.Context, req *orchestration.SpawnRequest) (*orchestration.Session, error) {
			copied := *req
			got = &copied
			return &orchestration.Session{ID: "sess-1", Backend: "acp", Status: orchestration.SessionStateActive}, nil
		},
	}
	_, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
	defer cleanup()

	if _, err := client.Spawn(context.Background(), &proto.SpawnSessionRequest{
		Backend:   "acp",
		TenantId:  "tenant-a",
		ProjectId: "proj-1",
		AccountId: "acct-pinned",
	}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if got == nil {
		t.Fatal("the backend was never spawned")
	}
	if got.AccountID != "acct-pinned" {
		t.Errorf("backend received AccountID %q, want acct-pinned", got.AccountID)
	}
	if got.AccountAuto {
		t.Error("a pinned spawn must not arrive asking for strategy-based selection")
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
	// Configure the runtime with no container runtime under a policy that
	// still admits an explicit SandboxModeRequired request — the spawn below
	// then violates for want of an engine. (SandboxPolicyRequired cannot be
	// used here: NewRuntime refuses to build a required-policy runtime with
	// no engine at all, see TestNewRuntime_SandboxRequiredWithoutRuntime.)
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

// TestGRPCSpawnServer_PromptCarriesTurnFailureEvidence closes the remote half
// of go-quicbaas#235, over a real gRPC connection.
//
// The daemon that runs the agent process is the only party that sees the CLI's
// result object. Until #235 it mapped a failed turn to a bare status and the
// structured account went no further, so a REMOTE worker burned its attempt on
// exactly the pre-model failure a local one is refunded for — the third time
// the same boundary quietly dropped something the control plane had stated
// (argv in #218, the account selection earlier in #235, this).
//
// Driven through bufconn rather than by calling the codec directly, because
// what is in doubt is whether the detail survives a genuine marshal, transit
// and unmarshal — a test that skipped that would agree with itself about a
// wire format neither side ever used.
func TestGRPCSpawnServer_PromptCarriesTurnFailureEvidence(t *testing.T) {
	t.Parallel()
	backend := &spawnBackend{
		promptFn: func(context.Context, *orchestration.PromptRequest) (*orchestration.PromptResponse, error) {
			return nil, &orchestration.PromptError{
				SessionID: "sess-1",
				Cause: &orchestration.StreamJSONError{
					Command: "claude",
					// The field a credential would be in. It must not travel.
					Result: "auth failed for Bearer sk-ant-api03-0123456789abcdefghij",
					TurnFailureEvidence: orchestration.TurnFailureEvidence{
						TerminalReason: "api_error",
						NumTurns:       1,
						Usage:          &orchestration.TokenUsage{},
					},
				},
			}
		},
	}
	_, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
	defer cleanup()

	stream, err := client.Prompt(context.Background(), &proto.PromptSessionRequest{
		Backend: "acp", TenantId: "tenant-a", SessionId: "sess-1", Prompt: "go",
	})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	_, recvErr := stream.Recv()
	if recvErr == nil {
		t.Fatal("the failing turn was reported as a successful stream")
	}

	evidence, carried := orchestration.TurnFailureEvidenceFromStatus(recvErr)
	if !carried {
		t.Fatalf("the daemon attached no turn-failure evidence; a remote worker cannot "+
			"tell a pre-model failure from a failed attempt (err %v)", recvErr)
	}
	if evidence.TerminalReason != "api_error" {
		t.Errorf("terminal reason = %q, want api_error", evidence.TerminalReason)
	}
	if evidence.NumTurns != 1 {
		t.Errorf("num turns = %d, want 1", evidence.NumTurns)
	}
	if evidence.Usage == nil {
		t.Fatal("usage arrived nil; 'reported zero' and 'reported nothing' must stay " +
			"distinguishable or a turn that measured nothing is refunded")
	}
	if !evidence.PreFirstTurn() {
		t.Error("the round trip lost the verdict: this is a pre-first-turn failure")
	}

	// THE PAYLOAD STAYS BEHIND. Only the closed-enum reason and the counts
	// travel; the CLI's own text is withheld on the daemon side and putting it
	// on the wire to be logged at the other end would undo that.
	if strings.Contains(recvErr.Error(), "sk-ant-api03") {
		t.Errorf("the CLI's result text crossed the daemon boundary: %v", recvErr)
	}
}

// TestGRPCSpawnServer_PromptWithoutEvidenceAttachesNoDetail is the
// backward-compatible direction: an ordinary failure travels exactly as it did
// before, so a control plane reading details finds none and classifies the
// failure the way it always has.
func TestGRPCSpawnServer_PromptWithoutEvidenceAttachesNoDetail(t *testing.T) {
	t.Parallel()
	backend := &spawnBackend{
		promptFn: func(context.Context, *orchestration.PromptRequest) (*orchestration.PromptResponse, error) {
			return nil, errors.New("the agent process exited")
		},
	}
	_, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
	defer cleanup()

	stream, err := client.Prompt(context.Background(), &proto.PromptSessionRequest{
		Backend: "acp", TenantId: "tenant-a", SessionId: "sess-1", Prompt: "go",
	})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	_, recvErr := stream.Recv()
	if recvErr == nil {
		t.Fatal("the failing turn was reported as a successful stream")
	}
	if _, carried := orchestration.TurnFailureEvidenceFromStatus(recvErr); carried {
		t.Error("a failure with no structured account arrived carrying evidence")
	}
	if _, ok := orchestration.PreFirstTurnFault(recvErr); ok {
		t.Error("an ordinary failure classified as a pre-first-turn fault")
	}
}

// TestGRPCSpawnServer_SpawnCarriesTheConfigurationFaultClass is #235's other
// half across the same boundary.
//
// The pooled account is resolved BEFORE the process is started, so a tenant
// with no eligible account produces no worker at all and the SDLC module parks the
// item rather than charging it. That decision is made from the resolver's typed
// error, which does not survive a gRPC status — so a remote daemon's "no
// account for this tenant" arrived as a generic transport failure and the item
// was charged for a worker that never existed.
func TestGRPCSpawnServer_SpawnCarriesTheConfigurationFaultClass(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{
		{
			name: "the pool has no eligible account",
			err: &orchestration.NoAccountAvailableError{
				TenantID: "acme", ProjectID: "proj-1",
				Provider: orchestration.AccountProvider("claude"),
			},
			want: orchestration.ErrNoAccountAvailable,
		},
		{
			name: "no pool was wired at all",
			err:  &orchestration.AccountResolverNotConfiguredError{Backend: "acp"},
			want: orchestration.ErrAccountResolverNotConfigured,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &spawnBackend{
				spawnFn: func(context.Context, *orchestration.SpawnRequest) (*orchestration.Session, error) {
					return nil, tc.err
				},
			}
			_, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
			defer cleanup()

			_, err := client.Spawn(context.Background(), &proto.SpawnSessionRequest{
				Backend: "acp", TenantId: "tenant-a", ProjectId: "proj-1", AccountAuto: true,
			})
			if err == nil {
				t.Fatal("a spawn with no resolvable account was reported as successful")
			}

			// The control plane's own reconstruction, exercised as the client
			// does it — the sentinel is what every downstream errors.Is asks
			// about, so this is the property that matters rather than the
			// wire encoding.
			wrapped := orchestration.WrapRemoteSpawnFaultForTest("Spawn", err)
			if !errors.Is(wrapped, tc.want) {
				t.Errorf("the reconstructed error does not match %v; a remote dispatch "+
					"with no account is charged for a worker that never existed (got %v)",
					tc.want, wrapped)
			}
		})
	}
}

// TestGRPCSpawnServer_SpawnWithAnOrdinaryFailureCarriesNoFaultClass is the
// asymmetric counterpart: a real spawn failure must stay a real spawn failure,
// or every failed dispatch would stop being charged.
func TestGRPCSpawnServer_SpawnWithAnOrdinaryFailureCarriesNoFaultClass(t *testing.T) {
	t.Parallel()
	backend := &spawnBackend{
		spawnFn: func(context.Context, *orchestration.SpawnRequest) (*orchestration.Session, error) {
			return nil, errors.New("exec: \"claude\": executable file not found in $PATH")
		},
	}
	_, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
	defer cleanup()

	_, err := client.Spawn(context.Background(), &proto.SpawnSessionRequest{
		Backend: "acp", TenantId: "tenant-a", ProjectId: "proj-1", AccountAuto: true,
	})
	if err == nil {
		t.Fatal("a failed spawn was reported as successful")
	}
	wrapped := orchestration.WrapRemoteSpawnFaultForTest("Spawn", err)
	if errors.Is(wrapped, orchestration.ErrNoAccountAvailable) ||
		errors.Is(wrapped, orchestration.ErrAccountResolverNotConfigured) {
		t.Errorf("an ordinary spawn failure was reconstructed as a configuration fault: %v", wrapped)
	}
}
