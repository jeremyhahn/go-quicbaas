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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

func TestOrchestratorGRPC_DeleteTask_Success(t *testing.T) {
	var gotTenant, gotTask string
	agentSvc := &grpcTestAgentService{
		deleteTaskFunc: func(_ context.Context, tenantID, taskID string) error {
			gotTenant, gotTask = tenantID, taskID
			return nil
		},
	}
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := server.DeleteTask(context.Background(), &pb.DeleteTaskRequest{TenantId: "tenant-1", TaskId: "task-1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Deleted)
	assert.Equal(t, "task-1", resp.TaskId)
	assert.Equal(t, "tenant-1", gotTenant)
	assert.Equal(t, "task-1", gotTask)
}

func TestOrchestratorGRPC_DeleteTask_Errors(t *testing.T) {
	t.Run("missing task id", func(t *testing.T) {
		server := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
		resp, err := server.DeleteTask(context.Background(), &pb.DeleteTaskRequest{TenantId: "t"})
		assert.Nil(t, resp)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
	t.Run("not found", func(t *testing.T) {
		agentSvc := &grpcTestAgentService{
			deleteTaskFunc: func(context.Context, string, string) error {
				return &orchestration.TaskNotFoundError{TenantID: "t", TaskID: "x"}
			},
		}
		server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())
		resp, err := server.DeleteTask(context.Background(), &pb.DeleteTaskRequest{TenantId: "t", TaskId: "x"})
		assert.Nil(t, resp)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
	})
	// A live job lease is a precondition that clears on its own, so
	// FailedPrecondition rather than the Internal fallback.
	t.Run("leased task", func(t *testing.T) {
		agentSvc := &grpcTestAgentService{
			deleteTaskFunc: func(context.Context, string, string) error {
				return &orchestration.TaskLeasedError{TenantID: "t", TaskID: "x", JobID: "job-9", Node: "node-a"}
			},
		}
		server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())
		resp, err := server.DeleteTask(context.Background(), &pb.DeleteTaskRequest{TenantId: "t", TaskId: "x"})
		assert.Nil(t, resp)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.FailedPrecondition, st.Code())
		assert.Contains(t, st.Message(), "job-9")
	})
}

// TestOrchestratorGRPC_DeleteTask_AuthzMapped proves the RPC is gated. An
// unmapped method reaches the service with no permission check at all.
func TestOrchestratorGRPC_DeleteTask_AuthzMapped(t *testing.T) {
	perm, ok := grpcMethodPermissions["/quicbaas.orchestrator.v1.OrchestratorService/DeleteTask"]
	require.True(t, ok, "DeleteTask has no authz mapping")
	assert.Equal(t, "orchestrator", perm.resource)
	assert.Equal(t, "task_create", perm.action)
}
