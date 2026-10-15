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

package grpc

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// An unknown backend is a lookup miss on every transport: REST answers 404
// and MCP not-found, so gRPC must answer NotFound however the service wraps
// it, never the Internal fallback.
func TestConvertOrchestratorError_BackendNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want codes.Code
	}{
		{
			name: "typed BackendNotFoundError",
			err:  &orchestration.BackendNotFoundError{Backend: "nope"},
			want: codes.NotFound,
		},
		{
			name: "bare sentinel",
			err:  orchestration.ErrBackendNotFound,
			want: codes.NotFound,
		},
		{
			name: "fmt-wrapped typed error",
			err:  fmt.Errorf("spawn session: %w", &orchestration.BackendNotFoundError{Backend: "nope"}),
			want: codes.NotFound,
		},
		{
			name: "wrapped inside an AgentServiceError",
			err: &orchestration.AgentServiceError{
				Sentinel: fmt.Errorf("lookup backend: %w", &orchestration.BackendNotFoundError{Backend: "nope"}),
				Message:  "nope",
			},
			want: codes.NotFound,
		},
		{
			name: "other AgentServiceError stays Internal",
			err:  &orchestration.AgentServiceError{Sentinel: orchestration.ErrProtocolViolation},
			want: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertOrchestratorError(tt.err)
			assert.Equal(t, tt.want, status.Code(got), "error: %v", got)
		})
	}
}

// Every orchestrator RPC whose service call resolves a backend answers an
// unknown one as NotFound.
func TestOrchestratorGRPC_UnknownBackendIsNotFound(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "t1")
	notFound := func() error {
		return fmt.Errorf("resolve backend: %w", &orchestration.BackendNotFoundError{Backend: "nope"})
	}

	t.Run("SpawnSession", func(t *testing.T) {
		svc := &grpcTestAgentService{
			spawnSessionFunc: func(context.Context, *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
				return nil, notFound()
			},
		}
		srv := NewOrchestratorServer(svc, createOrchestratorGRPCTestLogger())
		_, err := srv.SpawnSession(ctx, &pb.SpawnSessionRequest{Backend: "nope", TenantId: "t1"})
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("PromptSession without a session", func(t *testing.T) {
		svc := &grpcTestAgentService{
			promptSessionFunc: func(context.Context, *orchestration.PromptSessionRequest) (*orchestration.PromptSessionResponse, error) {
				return nil, notFound()
			},
		}
		srv := NewOrchestratorServer(svc, createOrchestratorGRPCTestLogger())
		_, err := srv.PromptSession(ctx, &pb.PromptSessionRequest{Backend: "nope", Prompt: "hi", TenantId: "t1"})
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("PromptSession on a session", func(t *testing.T) {
		svc := &grpcTestAgentService{
			promptSessionAsyncFunc: func(context.Context, *orchestration.PromptSessionRequest) (*orchestration.PromptTurnAck, error) {
				return nil, notFound()
			},
		}
		srv := NewOrchestratorServer(svc, createOrchestratorGRPCTestLogger())
		_, err := srv.PromptSession(ctx, &pb.PromptSessionRequest{SessionId: "s1", Prompt: "hi", TenantId: "t1"})
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("SessionStatus", func(t *testing.T) {
		svc := &grpcTestAgentService{
			sessionStatusFunc: func(context.Context, *orchestration.SessionStatusRequest) (*orchestration.SessionStatusResponse, error) {
				return nil, notFound()
			},
		}
		srv := NewOrchestratorServer(svc, createOrchestratorGRPCTestLogger())
		_, err := srv.SessionStatus(ctx, &pb.SessionStatusRequest{SessionId: "s1", TenantId: "t1"})
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("CloseSession", func(t *testing.T) {
		svc := &grpcTestAgentService{
			closeSessionFunc: func(context.Context, *orchestration.CloseSessionRequest) (*orchestration.CloseSessionResponse, error) {
				return nil, notFound()
			},
		}
		srv := NewOrchestratorServer(svc, createOrchestratorGRPCTestLogger())
		_, err := srv.CloseSession(ctx, &pb.CloseSessionRequest{SessionId: "s1", TenantId: "t1"})
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("RotateSessionAccount", func(t *testing.T) {
		server, svc := newRotatingOrchestratorServer()
		svc.rotateFunc = func(context.Context, *orchestration.RotateSessionAccountRequest) (*orchestration.RotateSessionAccountResult, error) {
			return nil, notFound()
		}
		_, err := server.RotateSessionAccount(tenantctx.WithTenant(context.Background(), "tenant-a"), &pb.RotateSessionAccountRequest{
			SessionId: "s1",
			TenantId:  "tenant-a",
		})
		require.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("ListBackendModels", func(t *testing.T) {
		svc := &grpcTestAgentService{
			listBackendModelsFunc: func(context.Context, string) ([]string, error) {
				return nil, notFound()
			},
		}
		srv := NewOrchestratorServer(svc, createOrchestratorGRPCTestLogger())
		_, err := srv.ListBackendModels(ctx, &pb.ListBackendModelsRequest{BackendName: "nope"})
		assert.Equal(t, codes.NotFound, status.Code(err))
	})
}
