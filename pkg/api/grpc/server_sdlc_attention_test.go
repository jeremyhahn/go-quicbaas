//go:build codec_json

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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/qrdbnode"
)

type attentionMemoryResolver struct {
	mu     sync.Mutex
	stores map[string]qrdbsdk.KVStore
}

func (r *attentionMemoryResolver) Resolve(_ context.Context, tenantID string) (qrdbsdk.KVStore, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stores == nil {
		r.stores = map[string]qrdbsdk.KVStore{}
	}
	if _, ok := r.stores[tenantID]; !ok {
		r.stores[tenantID] = qrdbsdk.NewMemoryKVStore()
	}
	return r.stores[tenantID], nil
}

// attentionProjects answers no project: the inbox resolves display names
// through it and tolerates none.
type attentionProjects struct{ service.ProjectService }

func (attentionProjects) GetProject(_ context.Context, req *service.GetProjectRequest) (*service.GetProjectResponse, error) {
	return nil, &service.ProjectNotFoundError{TenantID: req.TenantID, ProjectID: req.ProjectID}
}

func (attentionProjects) Update(context.Context, *orchestration.Project) error { return nil }

type attentionSecrets struct{ service.SecretsService }

// attentionAgents is an empty tenant agent catalog: the inbox binds no
// project, so no Agent reference is ever checked against it.
type attentionAgents struct{}

func (attentionAgents) Catalogue(context.Context, string) (orchestration.AgentCatalogue, error) {
	return orchestration.AgentCatalogue{Agents: map[string]orchestration.Agent{}, Pools: map[string]orchestration.AgentPool{}}, nil
}

// attentionRig is an SDLCServer over a real SDLC service whose attention
// inbox reads a real in-memory task store.
type attentionRig struct {
	server *SDLCServer
	tasks  orchestration.AgentService
	store  orchestration.TaskStore
}

func newAttentionRig(t *testing.T) *attentionRig {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout", Format: "console"})
	require.NoError(t, err)
	tasks, err := orchestration.NewAgentService(orchestration.NewInMemoryAgentStore(), nil, logger.NewAdapter(log))
	require.NoError(t, err)
	taskStore := orchestration.NewInMemoryTaskStore()
	tasks.(interface{ SetTaskStore(orchestration.TaskStore) }).SetTaskStore(taskStore)

	store, err := sdlc.NewStore(&attentionMemoryResolver{})
	require.NoError(t, err)
	n := qrdbnode.Shared(t)
	routes, err := sdlc.NewWebhookRouteIndex(n.Transaction, n.KV)
	require.NoError(t, err)
	guard, err := sdlc.NewRepoHostGuard([]string{"forge.test"}, false)
	require.NoError(t, err)
	svc, err := service.NewSDLCService(service.SDLCServiceConfig{
		Store: store, Routes: routes, Guard: guard, Projects: attentionProjects{}, ProjectWriter: attentionProjects{},
		Tasks: tasks, Secrets: attentionSecrets{}, Agents: attentionAgents{}, WebhookPublicURL: "https://factory.test",
	})
	require.NoError(t, err)
	return &attentionRig{server: NewSDLCServer(fixedSDLCProvider{svc: svc}, sdlcTestLogger(t)), tasks: tasks, store: taskStore}
}

// park creates a task adopted from no forge issue and blocks it, so the
// inbox lists it and a note posts nothing.
func (r *attentionRig) park(t *testing.T, tenant, projectID, taskID string) {
	t.Helper()
	ctx := tenantctx.WithSystem(context.Background())
	require.NoError(t, r.store.CreateTask(ctx, &orchestration.Task{
		ID: taskID, TenantID: tenant, ProjectID: projectID, Subject: "a parked task", Status: orchestration.TaskStatusInProgress,
	}))
	require.NoError(t, r.tasks.BlockTask(ctx, tenant, taskID, orchestration.TaskBlockKindNeedsInfo, "which curve?"))
}

func TestSDLCServer_ListAttentionPagesTheInbox(t *testing.T) {
	rig := newAttentionRig(t)
	rig.park(t, "tenant-a", "p1", "task-1")
	rig.park(t, "tenant-a", "p2", "task-2")
	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")

	resp, err := rig.server.ListAttention(ctx, &pb.ListAttentionRequest{TenantId: "tenant-a"})
	require.NoError(t, err)
	assert.Equal(t, int32(2), resp.GetTotal())
	require.Len(t, resp.GetItems(), 2)
	assert.Equal(t, "parked_task", resp.GetItems()[0].GetKind())
	assert.Contains(t, resp.GetItems()[0].GetActions(), "note")
	require.NotNil(t, resp.GetFacets()["kind"])
	assert.Equal(t, int32(2), resp.GetFacets()["kind"].GetCounts()["parked_task"])

	resp, err = rig.server.ListAttention(ctx, &pb.ListAttentionRequest{TenantId: "tenant-a", ProjectId: "p1", Limit: 1})
	require.NoError(t, err)
	require.Len(t, resp.GetItems(), 1)
	assert.Equal(t, "task-1", resp.GetItems()[0].GetId())
	assert.Zero(t, resp.GetNextOffset(), "one match is the last page")
}

func TestSDLCServer_ListAttentionRefusesBadInput(t *testing.T) {
	rig := newAttentionRig(t)
	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")
	for name, req := range map[string]*pb.ListAttentionRequest{
		"unknown kind":   {TenantId: "tenant-a", Kind: "not-a-kind"},
		"negative limit": {TenantId: "tenant-a", Limit: -1},
	} {
		_, err := rig.server.ListAttention(ctx, req)
		assert.Equal(t, codes.InvalidArgument, status.Code(err), "%s: %v", name, err)
	}
}

// TestSDLCServer_ResolveAttentionNotesAndReadmits: a note leaves the task
// parked, a readmit returns it to the queue, and each refusal is its own code.
func TestSDLCServer_ResolveAttentionNotesAndReadmits(t *testing.T) {
	rig := newAttentionRig(t)
	rig.park(t, "tenant-a", "p1", "task-1")
	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")

	_, err := rig.server.ResolveAttention(ctx, &pb.ResolveAttentionRequest{TenantId: "tenant-a", TaskId: "task-1", Action: "note", Answer: " "})
	assert.Equal(t, codes.InvalidArgument, status.Code(err), "an empty note: %v", err)

	resp, err := rig.server.ResolveAttention(ctx, &pb.ResolveAttentionRequest{
		TenantId: "tenant-a", TaskId: "task-1", Action: "note", Answer: "Decided: P-256.",
	})
	require.NoError(t, err)
	assert.Equal(t, "task-1", resp.GetItem().GetId())
	task, err := rig.tasks.GetTask(ctx, "tenant-a", "task-1")
	require.NoError(t, err)
	assert.Equal(t, orchestration.TaskStatusBlocked, task.Status, "a note keeps the task parked")

	_, err = rig.server.ResolveAttention(ctx, &pb.ResolveAttentionRequest{TenantId: "tenant-a", TaskId: "task-1", Action: "readmit"})
	require.NoError(t, err)
	task, err = rig.tasks.GetTask(ctx, "tenant-a", "task-1")
	require.NoError(t, err)
	assert.Equal(t, orchestration.TaskStatusPending, task.Status)

	_, err = rig.server.ResolveAttention(ctx, &pb.ResolveAttentionRequest{TenantId: "tenant-a", TaskId: "task-1", Action: "readmit"})
	assert.Equal(t, codes.FailedPrecondition, status.Code(err), "a task no longer blocked: %v", err)
	_, err = rig.server.ResolveAttention(ctx, &pb.ResolveAttentionRequest{TenantId: "tenant-a", TaskId: "nope", Action: "readmit"})
	assert.Equal(t, codes.NotFound, status.Code(err), "an unknown task: %v", err)
	_, err = rig.server.ResolveAttention(ctx, &pb.ResolveAttentionRequest{TenantId: "tenant-a", Action: "readmit"})
	assert.Equal(t, codes.InvalidArgument, status.Code(err), "no task id: %v", err)
}
