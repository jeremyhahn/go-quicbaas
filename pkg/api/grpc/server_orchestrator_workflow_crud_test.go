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
	"google.golang.org/protobuf/encoding/protowire"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// workflowLoaderAgent keeps workflows in a real WorkflowLoader, so the RPCs
// meet the service's own validation and not-found and duplicate errors.
type workflowLoaderAgent struct {
	*grpcTestAgentService
	loader *orchestration.WorkflowLoader
}

func (a *workflowLoaderAgent) RegisterWorkflow(ctx context.Context, def *orchestration.WorkflowDefinition) error {
	return a.loader.Register(orchestration.StorageTenant(ctx), def)
}

func (a *workflowLoaderAgent) GetWorkflow(ctx context.Context, name string) (*orchestration.WorkflowDefinition, error) {
	return a.loader.Get(orchestration.StorageTenant(ctx), name)
}

func (a *workflowLoaderAgent) UpdateWorkflow(ctx context.Context, def *orchestration.WorkflowDefinition) error {
	return a.loader.Update(orchestration.StorageTenant(ctx), def)
}

func (a *workflowLoaderAgent) DeleteWorkflow(ctx context.Context, name string) error {
	return a.loader.Delete(orchestration.StorageTenant(ctx), name)
}

func newWorkflowCRUDServer() *OrchestratorServer {
	agent := &workflowLoaderAgent{grpcTestAgentService: &grpcTestAgentService{}, loader: orchestration.NewWorkflowLoader()}
	return NewOrchestratorServer(agent, createOrchestratorGRPCTestLogger())
}

func validWorkflowProto(name string) *pb.OrchestratorWorkflowDefinition {
	return &pb.OrchestratorWorkflowDefinition{
		Name:   name,
		Mode:   "constrained",
		Agents: []*pb.OrchestratorWorkflowAgentRoleSpec{{Role: "lead", Backend: "claude"}},
	}
}

// TestOrchestratorGRPC_WorkflowCRUD runs the four RPCs against the service
// in the caller's tenant: update renames to the URL name, delete reports
// what it removed, and the workflow is gone afterwards.
func TestOrchestratorGRPC_WorkflowCRUD(t *testing.T) {
	srv := newWorkflowCRUDServer()
	ctx := tenantctx.WithTenant(context.Background(), "t1")

	created, err := srv.CreateWorkflow(ctx, &pb.OrchestratorCreateWorkflowRequest{Definition: validWorkflowProto("w")})
	require.NoError(t, err)
	assert.Equal(t, "w", created.GetWorkflow().GetName())

	updated := validWorkflowProto("ignored")
	updated.Description = "second draft"
	got, err := srv.UpdateWorkflow(ctx, &pb.OrchestratorUpdateWorkflowRequest{Name: "w", Definition: updated})
	require.NoError(t, err)
	assert.Equal(t, "w", got.GetWorkflow().GetName(), "the request name wins over the body's")

	fetched, err := srv.GetWorkflow(ctx, &pb.OrchestratorGetWorkflowRequest{Name: "w"})
	require.NoError(t, err)
	assert.Equal(t, "second draft", fetched.GetWorkflow().GetDescription())

	deleted, err := srv.DeleteWorkflow(ctx, &pb.OrchestratorDeleteWorkflowRequest{Name: "w"})
	require.NoError(t, err)
	assert.Equal(t, "w", deleted.GetName())
	assert.True(t, deleted.GetDeleted())

	_, err = srv.GetWorkflow(ctx, &pb.OrchestratorGetWorkflowRequest{Name: "w"})
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestOrchestratorGRPC_WorkflowCRUD_Errors(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "t1")
	invalid := &pb.OrchestratorWorkflowDefinition{Name: "w", Mode: "autonomous"}

	cases := []struct {
		name string
		call func(srv *OrchestratorServer) error
		want codes.Code
	}{
		{"create without a definition", func(srv *OrchestratorServer) error {
			_, err := srv.CreateWorkflow(ctx, &pb.OrchestratorCreateWorkflowRequest{})
			return err
		}, codes.InvalidArgument},
		{"create an invalid definition", func(srv *OrchestratorServer) error {
			_, err := srv.CreateWorkflow(ctx, &pb.OrchestratorCreateWorkflowRequest{Definition: invalid})
			return err
		}, codes.InvalidArgument},
		{"create a duplicate", func(srv *OrchestratorServer) error {
			_, err := srv.CreateWorkflow(ctx, &pb.OrchestratorCreateWorkflowRequest{Definition: validWorkflowProto("existing")})
			return err
		}, codes.AlreadyExists},
		{"create in a foreign tenant", func(srv *OrchestratorServer) error {
			_, err := srv.CreateWorkflow(ctx, &pb.OrchestratorCreateWorkflowRequest{TenantId: "t2", Definition: validWorkflowProto("w")})
			return err
		}, codes.PermissionDenied},
		{"get without a name", func(srv *OrchestratorServer) error {
			_, err := srv.GetWorkflow(ctx, &pb.OrchestratorGetWorkflowRequest{})
			return err
		}, codes.InvalidArgument},
		{"get a missing workflow", func(srv *OrchestratorServer) error {
			_, err := srv.GetWorkflow(ctx, &pb.OrchestratorGetWorkflowRequest{Name: "missing"})
			return err
		}, codes.NotFound},
		{"update without a name", func(srv *OrchestratorServer) error {
			_, err := srv.UpdateWorkflow(ctx, &pb.OrchestratorUpdateWorkflowRequest{Definition: validWorkflowProto("w")})
			return err
		}, codes.InvalidArgument},
		{"update without a definition", func(srv *OrchestratorServer) error {
			_, err := srv.UpdateWorkflow(ctx, &pb.OrchestratorUpdateWorkflowRequest{Name: "existing"})
			return err
		}, codes.InvalidArgument},
		{"update to an invalid definition", func(srv *OrchestratorServer) error {
			_, err := srv.UpdateWorkflow(ctx, &pb.OrchestratorUpdateWorkflowRequest{Name: "existing", Definition: invalid})
			return err
		}, codes.InvalidArgument},
		{"update a missing workflow", func(srv *OrchestratorServer) error {
			_, err := srv.UpdateWorkflow(ctx, &pb.OrchestratorUpdateWorkflowRequest{Name: "missing", Definition: validWorkflowProto("missing")})
			return err
		}, codes.NotFound},
		{"delete without a name", func(srv *OrchestratorServer) error {
			_, err := srv.DeleteWorkflow(ctx, &pb.OrchestratorDeleteWorkflowRequest{})
			return err
		}, codes.InvalidArgument},
		{"delete a missing workflow", func(srv *OrchestratorServer) error {
			_, err := srv.DeleteWorkflow(ctx, &pb.OrchestratorDeleteWorkflowRequest{Name: "missing"})
			return err
		}, codes.NotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newWorkflowCRUDServer()
			_, err := srv.CreateWorkflow(ctx, &pb.OrchestratorCreateWorkflowRequest{Definition: validWorkflowProto("existing")})
			require.NoError(t, err)
			assert.Equal(t, tc.want, status.Code(tc.call(srv)))
		})
	}
}

// retiredSystemPromptProto is a valid definition whose first agent still sets
// system_prompt as the field number 6 it had, the way an older client sends it.
func retiredSystemPromptProto(name string) *pb.OrchestratorWorkflowDefinition {
	def := validWorkflowProto(name)
	def.Description = "retired"
	agent := def.GetAgents()[0].ProtoReflect()
	retired := protowire.AppendTag(nil, retiredAgentRoleSpecSystemPrompt, protowire.BytesType)
	agent.SetUnknown(protowire.AppendString(retired, "be careful"))
	return def
}

func TestOrchestratorGRPC_WorkflowCRUD_RefusesRetiredSystemPrompt(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "t1")
	srv := newWorkflowCRUDServer()
	_, err := srv.CreateWorkflow(ctx, &pb.OrchestratorCreateWorkflowRequest{Definition: validWorkflowProto("existing")})
	require.NoError(t, err)

	_, err = srv.CreateWorkflow(ctx, &pb.OrchestratorCreateWorkflowRequest{Definition: retiredSystemPromptProto("w")})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Contains(t, status.Convert(err).Message(), "agents[0].system_prompt")

	_, err = srv.UpdateWorkflow(ctx, &pb.OrchestratorUpdateWorkflowRequest{
		Name: "existing", Definition: retiredSystemPromptProto("existing"),
	})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	got, err := srv.GetWorkflow(ctx, &pb.OrchestratorGetWorkflowRequest{Name: "existing"})
	require.NoError(t, err)
	assert.NotEqual(t, "retired", got.GetWorkflow().GetDescription(), "a refused definition must not be stored")
}
