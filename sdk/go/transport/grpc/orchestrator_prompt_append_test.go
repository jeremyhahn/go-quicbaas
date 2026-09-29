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
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// promptAppendServer records the requests that carry run-time prompt material
// and answers with records that carry it back.
type promptAppendServer struct {
	pb.UnimplementedOrchestratorServiceServer

	mu     sync.Mutex
	spawn  *pb.SpawnSessionRequest
	start  *pb.OrchestratorStartJobRequest
	create *pb.CreateTaskRequest
}

func (s *promptAppendServer) SpawnSession(_ context.Context, req *pb.SpawnSessionRequest) (*pb.SpawnSessionResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spawn = req
	return &pb.SpawnSessionResponse{SessionId: "s1"}, nil
}

func (s *promptAppendServer) StartJob(_ context.Context, req *pb.OrchestratorStartJobRequest) (*pb.OrchestratorStartJobResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.start = req
	return &pb.OrchestratorStartJobResponse{JobId: "j1"}, nil
}

func (s *promptAppendServer) CreateTask(_ context.Context, req *pb.CreateTaskRequest) (*pb.CreateTaskResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.create = req
	return &pb.CreateTaskResponse{Task: &pb.Task{Id: "t1", PromptAppend: req.GetPromptAppend()}}, nil
}

func (s *promptAppendServer) GetJobStatus(_ context.Context, req *pb.OrchestratorGetJobStatusRequest) (*pb.OrchestratorGetJobStatusResponse, error) {
	return &pb.OrchestratorGetJobStatusResponse{JobId: req.GetJobId(), PromptAppend: "job material"}, nil
}

func TestOrchestratorHandler_PromptAppendCrossesTheWire(t *testing.T) {
	fake := &promptAppendServer{}
	srv := newHandlerServer(t, func(s *grpc.Server) { pb.RegisterOrchestratorServiceServer(s, fake) })
	h := newOrchestratorHandler(srv.dial(t))
	ctx := context.Background()

	_, err := h.handleRequest(ctx, "quicbaas.Orchestrator", "SpawnSession",
		&orchestrator.SpawnSessionRequest{AgentID: "claude-coder", PromptAppend: "spawn material"})
	require.NoError(t, err)
	_, err = h.handleRequest(ctx, "quicbaas.Orchestrator", "StartJob",
		&orchestrator.StartJobRequest{TenantID: "t", Description: "d", PromptAppend: "job material"})
	require.NoError(t, err)
	task, err := h.handleRequest(ctx, "quicbaas.Orchestrator", "CreateTask",
		&orchestrator.CreateTaskRequest{TenantID: "t", Subject: "s", PromptAppend: "task material"})
	require.NoError(t, err)
	status, err := h.handleRequest(ctx, "quicbaas.Orchestrator", "GetJobStatus",
		&orchestrator.GetJobStatusRequest{TenantID: "t", JobID: "j1"})
	require.NoError(t, err)

	fake.mu.Lock()
	defer fake.mu.Unlock()
	assert.Equal(t, "spawn material", fake.spawn.GetPromptAppend())
	assert.Equal(t, "job material", fake.start.GetPromptAppend())
	assert.Equal(t, "task material", fake.create.GetPromptAppend())
	assert.Equal(t, "task material", task.(*orchestrator.Task).PromptAppend)
	assert.Equal(t, "job material", status.(*orchestrator.JobStatus).PromptAppend)
}
