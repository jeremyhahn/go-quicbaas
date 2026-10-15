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

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// TestOrchestratorGRPC_ListBackendModels pins the RPC to the REST route's
// service call: the node is threaded as host affinity, the caller's tenant
// reaches the service, and every response field is filled.
func TestOrchestratorGRPC_ListBackendModels(t *testing.T) {
	var gotBackend, gotAffinity, gotTenant string
	svc := &grpcTestAgentService{
		listBackendModelsFunc: func(ctx context.Context, backendName string) ([]string, error) {
			gotBackend = backendName
			gotAffinity = orchestration.HostAffinityFromContext(ctx)
			gotTenant = orchestration.TenantFromContext(ctx)
			return []string{"llama3", "qwen2"}, nil
		},
	}
	srv := NewOrchestratorServer(svc, createOrchestratorGRPCTestLogger())
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	resp, err := srv.ListBackendModels(ctx, &pb.ListBackendModelsRequest{NodeId: "node-7", BackendName: "ollama"})
	require.NoError(t, err)
	assert.Equal(t, "ollama", resp.GetBackend())
	assert.Equal(t, []string{"llama3", "qwen2"}, resp.GetModels())
	assert.Equal(t, int32(2), resp.GetCount())
	assert.Equal(t, "ollama", gotBackend)
	assert.Equal(t, "node-7", gotAffinity)
	assert.Equal(t, "t1", gotTenant)
}

// TestOrchestratorGRPC_ListBackendModels_AnyNodeAndNoModels covers the
// "any" node, which sets no affinity, and a backend with no model lister,
// which answers an empty list rather than nil.
func TestOrchestratorGRPC_ListBackendModels_AnyNodeAndNoModels(t *testing.T) {
	gotAffinity := "unset"
	svc := &grpcTestAgentService{
		listBackendModelsFunc: func(ctx context.Context, _ string) ([]string, error) {
			gotAffinity = orchestration.HostAffinityFromContext(ctx)
			return nil, nil
		},
	}
	srv := NewOrchestratorServer(svc, createOrchestratorGRPCTestLogger())
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	resp, err := srv.ListBackendModels(ctx, &pb.ListBackendModelsRequest{NodeId: "any", BackendName: "ollama"})
	require.NoError(t, err)
	assert.Empty(t, gotAffinity)
	assert.NotNil(t, resp.GetModels())
	assert.Empty(t, resp.GetModels())
	assert.Equal(t, int32(0), resp.GetCount())
}

func TestOrchestratorGRPC_ListBackendModels_Errors(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	t.Run("missing backend name is InvalidArgument", func(t *testing.T) {
		srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
		_, err := srv.ListBackendModels(ctx, &pb.ListBackendModelsRequest{NodeId: "node-7"})
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("unknown backend is NotFound", func(t *testing.T) {
		svc := &grpcTestAgentService{
			listBackendModelsFunc: func(context.Context, string) ([]string, error) {
				return nil, &orchestration.BackendNotFoundError{Backend: "nope"}
			},
		}
		srv := NewOrchestratorServer(svc, createOrchestratorGRPCTestLogger())
		_, err := srv.ListBackendModels(ctx, &pb.ListBackendModelsRequest{BackendName: "nope"})
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("bad input from the service is InvalidArgument", func(t *testing.T) {
		svc := &grpcTestAgentService{
			listBackendModelsFunc: func(context.Context, string) ([]string, error) {
				return nil, orchestration.ErrBackendRequired
			},
		}
		srv := NewOrchestratorServer(svc, createOrchestratorGRPCTestLogger())
		_, err := srv.ListBackendModels(ctx, &pb.ListBackendModelsRequest{BackendName: "x"})
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}
