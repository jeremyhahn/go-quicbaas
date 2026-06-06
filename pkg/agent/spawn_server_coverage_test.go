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
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	proto "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// requireCode asserts err carries the expected gRPC status code.
func requireCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("not a gRPC status error: %T %v", err, err)
	}
	if st.Code() != want {
		t.Fatalf("code=%v want %v (%s)", st.Code(), want, st.Message())
	}
}

// =============================================================================
// Spawn validation + error mapping
// =============================================================================

func TestGRPCSpawnServer_Spawn_EmptyBackend(t *testing.T) {
	t.Parallel()
	_, client, cleanup := buildSpawnServer(t, &spawnBackend{}, newTestIdentity())
	defer cleanup()

	_, err := client.Spawn(context.Background(), &proto.SpawnSessionRequest{
		TenantId: "tenant-a",
	})
	requireCode(t, err, codes.InvalidArgument)
}

func TestGRPCSpawnServer_Spawn_BackendErrorMapped(t *testing.T) {
	t.Parallel()
	backend := &spawnBackend{
		spawnFn: func(_ context.Context, _ *orchestration.SpawnRequest) (*orchestration.Session, error) {
			return nil, errors.New("backend boom")
		},
	}
	_, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
	defer cleanup()

	_, err := client.Spawn(context.Background(), &proto.SpawnSessionRequest{
		Backend:  "acp",
		TenantId: "tenant-a",
	})
	if err == nil {
		t.Fatal("expected mapped backend error")
	}
}

// =============================================================================
// Prompt validation + error mapping
// =============================================================================

func TestGRPCSpawnServer_Prompt_EmptyPrompt(t *testing.T) {
	t.Parallel()
	_, client, cleanup := buildSpawnServer(t, &spawnBackend{}, newTestIdentity())
	defer cleanup()

	stream, err := client.Prompt(context.Background(), &proto.PromptSessionRequest{
		Backend:   "acp",
		TenantId:  "tenant-a",
		SessionId: "s1",
	})
	if err != nil {
		t.Fatalf("opening stream: %v", err)
	}
	_, recvErr := stream.Recv()
	requireCode(t, recvErr, codes.InvalidArgument)
}

func TestGRPCSpawnServer_Prompt_EmptyBackend(t *testing.T) {
	t.Parallel()
	_, client, cleanup := buildSpawnServer(t, &spawnBackend{}, newTestIdentity())
	defer cleanup()

	stream, err := client.Prompt(context.Background(), &proto.PromptSessionRequest{
		TenantId:  "tenant-a",
		SessionId: "s1",
		Prompt:    "hi",
	})
	if err != nil {
		t.Fatalf("opening stream: %v", err)
	}
	_, recvErr := stream.Recv()
	requireCode(t, recvErr, codes.InvalidArgument)
}

func TestGRPCSpawnServer_Prompt_BackendErrorMapped(t *testing.T) {
	t.Parallel()
	backend := &spawnBackend{
		promptFn: func(_ context.Context, _ *orchestration.PromptRequest) (*orchestration.PromptResponse, error) {
			return nil, errors.New("prompt boom")
		},
	}
	_, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
	defer cleanup()

	stream, err := client.Prompt(context.Background(), &proto.PromptSessionRequest{
		Backend:   "acp",
		TenantId:  "tenant-a",
		SessionId: "s1",
		Prompt:    "hi",
	})
	if err != nil {
		t.Fatalf("opening stream: %v", err)
	}
	if _, recvErr := stream.Recv(); recvErr == nil {
		t.Fatal("expected mapped prompt error")
	}
}

// =============================================================================
// Status / Close validation + not-found
// =============================================================================

func TestGRPCSpawnServer_Status_EmptySession(t *testing.T) {
	t.Parallel()
	_, client, cleanup := buildSpawnServer(t, &spawnBackend{}, newTestIdentity())
	defer cleanup()

	_, err := client.Status(context.Background(), &proto.SessionStatusRequest{
		TenantId: "tenant-a",
	})
	requireCode(t, err, codes.InvalidArgument)
}

func TestGRPCSpawnServer_Status_NotFound(t *testing.T) {
	t.Parallel()
	backend := &spawnBackend{
		statusFn: func(_ context.Context, _ string) (*orchestration.SessionStatus, error) {
			return nil, errors.New("no such session")
		},
	}
	_, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
	defer cleanup()

	_, err := client.Status(context.Background(), &proto.SessionStatusRequest{
		TenantId:  "tenant-a",
		SessionId: "ghost",
	})
	if err == nil {
		t.Fatal("expected not-found / mapped error")
	}
}

func TestGRPCSpawnServer_Close_EmptySession(t *testing.T) {
	t.Parallel()
	_, client, cleanup := buildSpawnServer(t, &spawnBackend{}, newTestIdentity())
	defer cleanup()

	_, err := client.Close(context.Background(), &proto.CloseSessionRequest{
		TenantId: "tenant-a",
	})
	requireCode(t, err, codes.InvalidArgument)
}

func TestGRPCSpawnServer_Close_NotFound(t *testing.T) {
	t.Parallel()
	backend := &spawnBackend{
		closeFn: func(_ context.Context, _ string) error {
			return errors.New("no such session")
		},
	}
	_, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
	defer cleanup()

	_, err := client.Close(context.Background(), &proto.CloseSessionRequest{
		TenantId:  "tenant-a",
		SessionId: "ghost",
	})
	if err == nil {
		t.Fatal("expected not-found / mapped error")
	}
}

// =============================================================================
// Infer / ListModels error mapping
// =============================================================================

// TestGRPCSpawnServer_Infer_BackendErrorMapped drives the non-unsupported
// runtime-error branch of Infer (backend Prompt fails → InferBackendError
// → mapRuntimeError).
func TestGRPCSpawnServer_Infer_BackendErrorMapped(t *testing.T) {
	t.Parallel()
	backend := &fakeOllamaBackend{promptErr: errors.New("ollama down")}
	_, client, cleanup := buildInferSpawnServer(t, backend, newInferTestIdentity())
	defer cleanup()

	stream, err := client.Infer(context.Background(), &proto.InferRequest{
		Backend:  "ollama",
		Prompt:   "hi",
		TenantId: "tenant-a",
	})
	if err != nil {
		// Some transports surface immediately.
		return
	}
	if _, recvErr := stream.Recv(); recvErr == nil {
		t.Fatal("expected mapped backend error from Infer")
	}
}

// TestGRPCSpawnServer_ListModels_BackendErrorMapped drives the
// runtime-error branch of ListModels.
func TestGRPCSpawnServer_ListModels_BackendErrorMapped(t *testing.T) {
	t.Parallel()
	backend := &fakeModelBackend{modelsErr: errors.New("catalog unavailable")}
	_, client, cleanup := buildCustomInferSpawnServer(t,
		map[string]orchestration.AgentBackend{"ollama": backend},
		newInferTestIdentity(),
	)
	defer cleanup()

	_, err := client.ListModels(context.Background(), &proto.ListModelsRequest{
		Backend:  "ollama",
		TenantId: "tenant-a",
	})
	if err == nil {
		t.Fatal("expected mapped ListModels backend error")
	}
}
