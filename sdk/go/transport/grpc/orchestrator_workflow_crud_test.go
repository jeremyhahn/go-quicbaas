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
	"math"
	"testing"
	"time"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/internal/workflowcrud"
)

func TestWorkflowCRUD_RoundTripsOverGRPC(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	store := workflowcrud.NewStore()
	// The server's authentication interceptor binds the caller's tenant;
	// this one binds the fixture's.
	bindTenant := func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(tenantctx.WithTenant(ctx, workflowcrud.TenantID), req)
	}
	server := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterOrchestratorServiceServer(s, grpcapi.NewOrchestratorServer(store.Agent(), log))
	}, grpc.UnaryInterceptor(bindTenant))
	tr := &Transport{cfg: &transport.Config{Timeout: 5 * time.Second}, conn: server.dial(t)}

	workflowcrud.Exercise(t, orchestrator.NewClient(tr), errors.IsAlreadyExists)
}

func TestWorkflowCRUDHandlers_RejectWrongRequestType(t *testing.T) {
	handlers := map[string]func(context.Context, *grpc.ClientConn, interface{}) (interface{}, error){
		"CreateWorkflow": handleOrchestratorCreateWorkflow,
		"GetWorkflow":    handleOrchestratorGetWorkflow,
		"UpdateWorkflow": handleOrchestratorUpdateWorkflow,
		"DeleteWorkflow": handleOrchestratorDeleteWorkflow,
	}
	for verb, handle := range handlers {
		if _, err := handle(context.Background(), nil, &orchestrator.ListWorkflowsRequest{}); !errors.IsInvalidInput(err) {
			t.Errorf("%s with the wrong request type: got %v, want invalid input", verb, err)
		}
	}
}

func TestWorkflowDefinitionToProto_RejectsCountBeyondInt32(t *testing.T) {
	definition := &orchestrator.WorkflowDefinition{
		Name:   "w",
		Agents: []orchestrator.AgentRoleSpec{{Role: "lead", Count: math.MaxInt32 + 1}},
	}
	if _, err := workflowDefinitionToProto("test", definition); !errors.IsInvalidInput(err) {
		t.Fatalf("an agent count beyond int32: got %v, want invalid input", err)
	}
}
