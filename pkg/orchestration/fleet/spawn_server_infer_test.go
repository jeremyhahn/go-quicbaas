// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package fleet

import (
	"context"
	"errors"
	"io"
	"net"
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

// =============================================================================
// GRPCSpawnServer.Infer tests
// =============================================================================

// TestGRPCSpawnServer_Infer_HappyPath verifies that the Infer handler routes a
// valid request through Runtime.Infer and streams the result back.
func TestGRPCSpawnServer_Infer_HappyPath(t *testing.T) {
	t.Parallel()

	ollamaBackend := &fakeOllamaBackend{
		promptResp: &orchestration.PromptResponse{
			Content:    []orchestration.ContentBlock{{Type: "text", Text: "mTLS haiku"}},
			Model:      "qwen3:1.7b",
			StopReason: "end_turn",
		},
	}

	_, client, cleanup := buildInferSpawnServer(t, ollamaBackend, newInferTestIdentity())
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.Infer(ctx, &proto.InferRequest{
		Backend:  "ollama",
		Prompt:   "write a haiku about mTLS",
		Model:    "qwen3:1.7b",
		TenantId: "tenant-a",
	})
	if err != nil {
		t.Fatalf("Infer returned error: %v", err)
	}

	var frames []*proto.InferResponse
	for {
		f, recvErr := stream.Recv()
		if errors.Is(recvErr, errEOF) {
			break
		}
		if recvErr != nil {
			t.Fatalf("Recv error: %v", recvErr)
		}
		frames = append(frames, f)
	}

	if len(frames) == 0 {
		t.Fatal("expected at least one frame")
	}
	last := frames[len(frames)-1]
	if !last.GetFinal() {
		t.Error("expected final=true on last frame")
	}
	if last.GetModel() != "qwen3:1.7b" {
		t.Errorf("expected model 'qwen3:1.7b', got %q", last.GetModel())
	}
}

// TestGRPCSpawnServer_Infer_EmptyPrompt verifies that the handler rejects an
// empty prompt with InvalidArgument.
func TestGRPCSpawnServer_Infer_EmptyPrompt(t *testing.T) {
	t.Parallel()

	_, client, cleanup := buildInferSpawnServer(t, &fakeOllamaBackend{}, newInferTestIdentity())
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	stream, err := client.Infer(ctx, &proto.InferRequest{
		Backend:  "ollama",
		Prompt:   "",
		TenantId: "tenant-a",
	})
	if err != nil {
		// Some transports surface the error on the call itself.
		st, _ := status.FromError(err)
		if st.Code() != codes.InvalidArgument {
			t.Errorf("expected InvalidArgument, got %v", st.Code())
		}
		return
	}

	_, recvErr := stream.Recv()
	if recvErr == nil {
		t.Fatal("expected error for empty prompt, got nil")
	}
	st, _ := status.FromError(recvErr)
	if st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v: %v", st.Code(), recvErr)
	}
}

// TestGRPCSpawnServer_Infer_EmptyBackend verifies that the handler rejects an
// empty backend field with InvalidArgument.
func TestGRPCSpawnServer_Infer_EmptyBackend(t *testing.T) {
	t.Parallel()

	_, client, cleanup := buildInferSpawnServer(t, &fakeOllamaBackend{}, newInferTestIdentity())
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	stream, err := client.Infer(ctx, &proto.InferRequest{
		Backend:  "",
		Prompt:   "hello",
		TenantId: "tenant-a",
	})
	if err != nil {
		st, _ := status.FromError(err)
		if st.Code() != codes.InvalidArgument {
			t.Errorf("expected InvalidArgument, got %v", st.Code())
		}
		return
	}

	_, recvErr := stream.Recv()
	if recvErr == nil {
		t.Fatal("expected error for empty backend, got nil")
	}
	st, _ := status.FromError(recvErr)
	if st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", st.Code())
	}
}

// TestGRPCSpawnServer_Infer_NoIdentity verifies that an unauthenticated caller
// receives Unauthenticated.
func TestGRPCSpawnServer_Infer_NoIdentity(t *testing.T) {
	t.Parallel()

	_, client, cleanup := buildInferSpawnServer(t, &fakeOllamaBackend{}, nil)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	stream, err := client.Infer(ctx, &proto.InferRequest{
		Backend:  "ollama",
		Prompt:   "hello",
		TenantId: "tenant-a",
	})
	if err != nil {
		st, _ := status.FromError(err)
		if st.Code() != codes.Unauthenticated {
			t.Errorf("expected Unauthenticated, got %v", st.Code())
		}
		return
	}

	_, recvErr := stream.Recv()
	if recvErr == nil {
		t.Fatal("expected Unauthenticated error, got nil")
	}
	st, _ := status.FromError(recvErr)
	if st.Code() != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated, got %v", st.Code())
	}
}

// TestGRPCSpawnServer_Infer_ACPBackendRejected verifies that requesting Infer
// against a session-based backend returns Unimplemented.
func TestGRPCSpawnServer_Infer_ACPBackendRejected(t *testing.T) {
	t.Parallel()

	acpBackend := &fakeACPBackend{}

	// Build a server where the registered backend is session-based.
	_, client, cleanup := buildCustomInferSpawnServer(t,
		map[string]orchestration.AgentBackend{"acp": acpBackend},
		newInferTestIdentity(),
	)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	stream, err := client.Infer(ctx, &proto.InferRequest{
		Backend:  "acp",
		Prompt:   "hello",
		TenantId: "tenant-a",
	})
	if err != nil {
		st, _ := status.FromError(err)
		if st.Code() != codes.Unimplemented {
			t.Errorf("expected Unimplemented, got %v: %v", st.Code(), err)
		}
		return
	}

	_, recvErr := stream.Recv()
	if recvErr == nil {
		t.Fatal("expected Unimplemented error, got nil")
	}
	st, _ := status.FromError(recvErr)
	if st.Code() != codes.Unimplemented {
		t.Errorf("expected Unimplemented, got %v", st.Code())
	}
}

// =============================================================================
// GRPCSpawnServer.ListModels tests
// =============================================================================

// fakeModelBackend is a ModelLister-capable backend for ListModels tests.
type fakeModelBackend struct {
	models    []string
	modelsErr error
}

func (b *fakeModelBackend) Spawn(_ context.Context, _ *orchestration.SpawnRequest) (*orchestration.Session, error) {
	return nil, &orchestration.UnsupportedOperationError{Backend: "fake-model", Operation: "spawn"}
}
func (b *fakeModelBackend) Prompt(_ context.Context, _ *orchestration.PromptRequest) (*orchestration.PromptResponse, error) {
	return &orchestration.PromptResponse{
		Content: []orchestration.ContentBlock{{Type: "text", Text: "ok"}},
	}, nil
}
func (b *fakeModelBackend) Status(_ context.Context, _ string) (*orchestration.SessionStatus, error) {
	return &orchestration.SessionStatus{}, nil
}
func (b *fakeModelBackend) Close(_ context.Context, _ string) error { return nil }
func (b *fakeModelBackend) Capabilities() orchestration.BackendCapabilities {
	return orchestration.BackendCapabilities{Kind: orchestration.BackendKindModelServer}
}
func (b *fakeModelBackend) Type() string         { return "fake-model" }
func (b *fakeModelBackend) ToolGuidance() string { return "" }

// ListModels implements orchestration.ModelLister.
func (b *fakeModelBackend) ListModels(_ context.Context) ([]string, error) {
	if b.modelsErr != nil {
		return nil, b.modelsErr
	}
	return b.models, nil
}

// TestGRPCSpawnServer_ListModels_HappyPath verifies that the ListModels RPC
// returns the model catalog from the backend's ModelLister.
func TestGRPCSpawnServer_ListModels_HappyPath(t *testing.T) {
	t.Parallel()

	backend := &fakeModelBackend{models: []string{"qwen3:1.7b", "llama3.2:1b"}}
	_, client, cleanup := buildCustomInferSpawnServer(t,
		map[string]orchestration.AgentBackend{"ollama": backend},
		newInferTestIdentity(),
	)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.ListModels(ctx, &proto.ListModelsRequest{
		Backend:  "ollama",
		TenantId: "tenant-a",
	})
	if err != nil {
		t.Fatalf("ListModels error: %v", err)
	}
	if resp.GetBackend() != "ollama" {
		t.Errorf("backend: got %q, want %q", resp.GetBackend(), "ollama")
	}
	if len(resp.GetModels()) != 2 {
		t.Fatalf("models count: got %d, want 2", len(resp.GetModels()))
	}
	if resp.GetModels()[0] != "qwen3:1.7b" {
		t.Errorf("models[0]: got %q, want %q", resp.GetModels()[0], "qwen3:1.7b")
	}
}

// TestGRPCSpawnServer_ListModels_EmptyBackend verifies that an empty backend
// field returns InvalidArgument before auth is checked.
func TestGRPCSpawnServer_ListModels_EmptyBackend(t *testing.T) {
	t.Parallel()

	_, client, cleanup := buildInferSpawnServer(t, &fakeOllamaBackend{}, newInferTestIdentity())
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := client.ListModels(ctx, &proto.ListModelsRequest{Backend: ""})
	if err == nil {
		t.Fatal("expected InvalidArgument error, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", st.Code())
	}
}

// TestGRPCSpawnServer_ListModels_NoIdentity verifies that an unauthenticated
// caller receives Unauthenticated.
func TestGRPCSpawnServer_ListModels_NoIdentity(t *testing.T) {
	t.Parallel()

	_, client, cleanup := buildInferSpawnServer(t, &fakeOllamaBackend{}, nil)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := client.ListModels(ctx, &proto.ListModelsRequest{
		Backend:  "ollama",
		TenantId: "tenant-a",
	})
	if err == nil {
		t.Fatal("expected Unauthenticated error, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated, got %v", st.Code())
	}
}

// TestGRPCSpawnServer_ListModels_NonListerBackend verifies that a backend
// that does not implement ModelLister returns an empty list without error.
func TestGRPCSpawnServer_ListModels_NonListerBackend(t *testing.T) {
	t.Parallel()

	// fakeACPBackend does not implement ModelLister.
	_, client, cleanup := buildCustomInferSpawnServer(t,
		map[string]orchestration.AgentBackend{"acp": &fakeACPBackend{}},
		newInferTestIdentity(),
	)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resp, err := client.ListModels(ctx, &proto.ListModelsRequest{
		Backend:  "acp",
		TenantId: "tenant-a",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.GetModels()) != 0 {
		t.Errorf("expected empty model list for non-lister backend, got %v", resp.GetModels())
	}
}

// =============================================================================
// Helpers
// =============================================================================

// errEOF aliases io.EOF for readability in stream-drain loops.
var errEOF = io.EOF

// drainInferStream reads all frames until EOF. Defined inline in tests.

// newInferTestIdentity returns an identity with all runtime scopes including
// the new AgentScopeRuntimeInfer.
func newInferTestIdentity() *orchestration.AgentIdentity {
	return &orchestration.AgentIdentity{
		AgentID:          "agent-infer-test",
		ServiceAccountID: "sa-test",
		TenantID:         "tenant-a",
		Role:             "worker",
		Scopes: []string{
			orchestration.AgentScopeRuntimeSpawn,
			orchestration.AgentScopeRuntimePrompt,
			orchestration.AgentScopeRuntimeStatus,
			orchestration.AgentScopeRuntimeInfer,
		},
		ClientID: "client-test",
	}
}

// buildInferSpawnServer creates a bufconn-backed spawn server with the given
// backend registered under the name "ollama".
func buildInferSpawnServer(
	t *testing.T,
	backend orchestration.AgentBackend,
	identity *orchestration.AgentIdentity,
) (*GRPCSpawnServer, proto.FleetRuntimeClient, func()) {
	t.Helper()
	return buildCustomInferSpawnServer(t,
		map[string]orchestration.AgentBackend{"ollama": backend},
		identity,
	)
}

// buildCustomInferSpawnServer creates a bufconn-backed spawn server with the
// given backends map. Used when we need non-ollama backend names.
func buildCustomInferSpawnServer(
	t *testing.T,
	backends map[string]orchestration.AgentBackend,
	identity *orchestration.AgentIdentity,
) (*GRPCSpawnServer, proto.FleetRuntimeClient, func()) {
	t.Helper()

	rt, err := NewRuntime(RuntimeConfig{
		Backends: backends,
		Logger:   &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	lis := bufconn.Listen(1 << 20)
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
	if startErr := srv.Start(context.Background()); startErr != nil {
		t.Fatalf("Start: %v", startErr)
	}

	conn, dialErr := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
			return lis.Dial()
		}),
	)
	if dialErr != nil {
		t.Fatalf("client dial: %v", dialErr)
	}

	client := proto.NewFleetRuntimeClient(conn)
	cleanup := func() {
		conn.Close()
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Stop(stopCtx)
	}
	return srv, client, cleanup
}
