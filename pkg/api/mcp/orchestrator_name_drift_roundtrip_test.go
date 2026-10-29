//go:build mcp

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

package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	mcpservertest "github.com/mark3labs/mcp-go/server/servertest"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	sdkmcp "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// errDriftServiceRefused is what the mock services refuse with in the
// bad-input cases, standing in for a service-side rejection.
var errDriftServiceRefused = orchestration.ErrNoMatchingFleetNode

// serveRegistryOverMCP serves the named registry tools through a real mcp-go
// streamable HTTP server, wired the way the server module wires them, and
// returns an SDK orchestrator client that reaches them over the SDK MCP
// transport. It is the whole SDK -> wire -> tool -> service path in memory,
// called as a caller of tenant t1.
func serveRegistryOverMCP(t *testing.T, reg *ToolRegistry, tools ...string) orchestrator.Client {
	t.Helper()
	return orchestrator.NewClient(registryMCPTransport(t, reg, nil, tools...))
}

// registryMCPTransport serves the named registry tools over a real MCP
// server and returns an SDK MCP transport connected to it. onCall, when not
// nil, is told the name of every tool the server receives.
func registryMCPTransport(t *testing.T, reg *ToolRegistry, onCall func(tool string), tools ...string) *sdkmcp.Transport {
	t.Helper()

	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	require.NoError(t, err)

	srv := mcpserver.NewMCPServer("NameDriftServer", "1.0.0")
	for _, name := range tools {
		srv.AddTool(mcpgo.NewTool(name), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			if onCall != nil {
				onCall(req.Params.Name)
			}
			toolCtx := orchestration.ContextWithTenant(tenantctx.WithTenant(ctx, "t1"), "t1")
			result, execErr := reg.ExecuteTool(toolCtx, req.Params.Name, req.GetArguments())
			if execErr != nil {
				return ToolErrorResult(log, req.Params.Name, execErr), nil
			}
			text, marshalErr := json.Marshal(result)
			if marshalErr != nil {
				return nil, marshalErr
			}
			return mcpgo.NewToolResultText(string(text)), nil
		})
	}

	httpSrv := mcpservertest.NewTestStreamableHTTPServer(srv)
	tr, err := sdkmcp.New(&transport.Config{Address: httpSrv.URL, Timeout: 5 * time.Second})
	if err != nil {
		httpSrv.Close()
		t.Fatalf("failed to create MCP transport: %v", err)
	}
	t.Cleanup(func() {
		require.NoError(t, tr.Close())
		httpSrv.Close()
	})
	return tr
}

// requireTypedError asserts the SDK surfaced a coded error, not a decode or
// type-assertion failure.
func requireTypedError(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	require.NotEqual(t, sdkerrors.CodeUnknown, sdkerrors.Code(err), "error carries no code: %v", err)
	require.NotContains(t, err.Error(), "invalid response type")
}

func TestNameDrift_RegisterFleetNode(t *testing.T) {
	var got *orchestration.RegisterFleetNodeRequest
	svc := &fleetMockAgentService{
		registerFn: func(_ context.Context, req *orchestration.RegisterFleetNodeRequest) (*orchestration.RegisterFleetNodeResponse, error) {
			got = req
			return &orchestration.RegisterFleetNodeResponse{Node: &orchestration.FleetNode{
				ID: "node-1", TenantID: req.TenantID, Name: req.Name, Host: req.Host,
				Backend: req.Backend, Model: req.Model, NodeType: req.NodeType,
				Status: orchestration.FleetNodeStatus("online"),
			}}, nil
		},
	}
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_fleet_register")

	resp, err := client.RegisterFleetNode(context.Background(), &orchestrator.RegisterFleetNodeRequest{
		TenantID: "t1", Name: "worker", Host: "10.0.0.5", NodeType: "agent-runtime",
		Backend: "acp", Model: "opus", WorkDir: "/srv/work",
		Metadata: map[string]string{"rack": "r1"},
	})
	require.NoError(t, err)

	require.NotNil(t, got)
	require.Equal(t, "t1", got.TenantID)
	require.Equal(t, "worker", got.Name)
	require.Equal(t, "10.0.0.5", got.Host)
	require.Equal(t, "agent-runtime", got.NodeType)
	require.Equal(t, "acp", got.Backend)
	require.Equal(t, "opus", got.Model)
	require.Equal(t, "/srv/work", got.WorkDir)
	require.Equal(t, map[string]string{"rack": "r1"}, got.Metadata)

	require.NotNil(t, resp.Node)
	require.Equal(t, "node-1", resp.Node.ID)
	require.Equal(t, "t1", resp.Node.TenantID)
	require.Equal(t, "worker", resp.Node.Name)
	require.Equal(t, "10.0.0.5", resp.Node.Host)
	require.Equal(t, "acp", resp.Node.Backend)
	require.Equal(t, "opus", resp.Node.Model)
	require.Equal(t, "agent-runtime", resp.Node.NodeType)
	require.Equal(t, "online", string(resp.Node.Status))
}

func TestNameDrift_RegisterFleetNode_ServiceRefusal(t *testing.T) {
	svc := &fleetMockAgentService{
		registerFn: func(context.Context, *orchestration.RegisterFleetNodeRequest) (*orchestration.RegisterFleetNodeResponse, error) {
			return nil, errDriftServiceRefused
		},
	}
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_fleet_register")

	_, err := client.RegisterFleetNode(context.Background(), &orchestrator.RegisterFleetNodeRequest{
		TenantID: "t1", Name: "worker", Host: "10.0.0.5", NodeType: "agent-runtime",
	})
	requireTypedError(t, err)
}

func TestNameDrift_RemoveFleetNode(t *testing.T) {
	var gotTenant, gotNode string
	svc := &fleetMockAgentService{
		removeFn: func(_ context.Context, tenantID, nodeID string) error {
			gotTenant, gotNode = tenantID, nodeID
			return nil
		},
	}
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_fleet_remove")

	resp, err := client.RemoveFleetNode(context.Background(), &orchestrator.RemoveFleetNodeRequest{
		TenantID: "t1", NodeID: "node-1",
	})
	require.NoError(t, err)
	require.Equal(t, "t1", gotTenant)
	require.Equal(t, "node-1", gotNode)
	require.Equal(t, "node-1", resp.NodeID)
	require.True(t, resp.Removed)
}

func TestNameDrift_RemoveFleetNode_ServiceRefusal(t *testing.T) {
	svc := &fleetMockAgentService{
		removeFn: func(context.Context, string, string) error { return errDriftServiceRefused },
	}
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_fleet_remove")

	_, err := client.RemoveFleetNode(context.Background(), &orchestrator.RemoveFleetNodeRequest{
		TenantID: "t1", NodeID: "node-1",
	})
	requireTypedError(t, err)
}

func TestNameDrift_DisableFleetNode(t *testing.T) {
	disabledAt := time.Date(2026, 10, 14, 9, 30, 0, 0, time.UTC)
	stamp := disabledAt.Add(time.Second)
	var got *service.DisableFleetNodeRequest
	admin := &mockFleetAdminService{
		disableFunc: func(_ context.Context, req *service.DisableFleetNodeRequest) (*service.DisableFleetNodeResponse, error) {
			got = req
			return &service.DisableFleetNodeResponse{
				NodeID: req.NodeID, Disabled: true, DisabledAt: disabledAt, Timestamp: stamp,
			}, nil
		},
	}
	client := serveRegistryOverMCP(t, newProjectAdminRegistry(t, nil, admin), "quicbaas_fleet_disable")

	resp, err := client.DisableFleetNode(context.Background(), &orchestrator.DisableFleetNodeRequest{
		TenantID: "t1", NodeID: "node-1",
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "t1", got.TenantID)
	require.Equal(t, "node-1", got.NodeID)
	require.Equal(t, "node-1", resp.NodeID)
	require.True(t, resp.Disabled)
	require.True(t, disabledAt.Equal(resp.DisabledAt), "disabled_at = %v", resp.DisabledAt)
	require.True(t, stamp.Equal(resp.Timestamp), "timestamp = %v", resp.Timestamp)
}

func TestNameDrift_DisableFleetNode_ServiceRefusal(t *testing.T) {
	admin := &mockFleetAdminService{
		disableFunc: func(context.Context, *service.DisableFleetNodeRequest) (*service.DisableFleetNodeResponse, error) {
			return nil, errDriftServiceRefused
		},
	}
	client := serveRegistryOverMCP(t, newProjectAdminRegistry(t, nil, admin), "quicbaas_fleet_disable")

	_, err := client.DisableFleetNode(context.Background(), &orchestrator.DisableFleetNodeRequest{
		TenantID: "t1", NodeID: "node-1",
	})
	requireTypedError(t, err)
}

func TestNameDrift_EnableFleetNode(t *testing.T) {
	stamp := time.Date(2026, 10, 14, 9, 31, 0, 0, time.UTC)
	var got *service.EnableFleetNodeRequest
	admin := &mockFleetAdminService{
		enableFunc: func(_ context.Context, req *service.EnableFleetNodeRequest) (*service.EnableFleetNodeResponse, error) {
			got = req
			return &service.EnableFleetNodeResponse{NodeID: req.NodeID, Enabled: true, Timestamp: stamp}, nil
		},
	}
	client := serveRegistryOverMCP(t, newProjectAdminRegistry(t, nil, admin), "quicbaas_fleet_enable")

	resp, err := client.EnableFleetNode(context.Background(), &orchestrator.EnableFleetNodeRequest{
		TenantID: "t1", NodeID: "node-1",
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "t1", got.TenantID)
	require.Equal(t, "node-1", got.NodeID)
	require.Equal(t, "node-1", resp.NodeID)
	require.True(t, resp.Enabled)
	require.True(t, stamp.Equal(resp.Timestamp), "timestamp = %v", resp.Timestamp)
}

func TestNameDrift_EnableFleetNode_ServiceRefusal(t *testing.T) {
	admin := &mockFleetAdminService{
		enableFunc: func(context.Context, *service.EnableFleetNodeRequest) (*service.EnableFleetNodeResponse, error) {
			return nil, errDriftServiceRefused
		},
	}
	client := serveRegistryOverMCP(t, newProjectAdminRegistry(t, nil, admin), "quicbaas_fleet_enable")

	_, err := client.EnableFleetNode(context.Background(), &orchestrator.EnableFleetNodeRequest{
		TenantID: "t1", NodeID: "node-1",
	})
	requireTypedError(t, err)
}

// driftTaskCreator answers CreateTask with a fixed task and error and
// records the request the tool built.
type driftTaskCreator struct {
	fleetMockAgentService
	got  *orchestration.CreateTaskRequest
	task *orchestration.Task
	err  error
}

func (c *driftTaskCreator) CreateTask(_ context.Context, req *orchestration.CreateTaskRequest) (*orchestration.Task, error) {
	c.got = req
	return c.task, c.err
}

func TestNameDrift_CreateTask(t *testing.T) {
	svc := &driftTaskCreator{task: &orchestration.Task{
		ID: "task-1", TenantID: "t1", Subject: "ship it", Description: "the whole thing",
		Status: orchestration.TaskStatusPending, AssignedTo: "alice",
		Metadata: map[string]string{"issue": "124"}, CreatedAt: 1700000000,
	}}
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_orchestrator_task_create")

	task, err := client.CreateTask(context.Background(), &orchestrator.CreateTaskRequest{
		TenantID: "t1", TeamName: "core", Subject: "ship it", Description: "the whole thing",
		AssignedTo: "alice", Dependencies: []string{"task-0"},
		Metadata: map[string]string{"issue": "124"}, ProjectID: "proj-1", AutoSpawn: true,
		Backend: "acp", GpuRequired: true, HostAffinity: "gpu-1",
		Source:   &orchestrator.TaskSource{Kind: "issue", Ref: "124", URL: "https://example.invalid/124"},
		Priority: 3, AttemptBudget: 2, Runner: "claude", PromptAppend: "be brief",
	})
	require.NoError(t, err)

	got := svc.got
	require.NotNil(t, got)
	require.Equal(t, "t1", got.TenantID)
	require.Equal(t, "core", got.TeamName)
	require.Equal(t, "ship it", got.Subject)
	require.Equal(t, "the whole thing", got.Description)
	require.Equal(t, "alice", got.AssignedTo)
	require.Equal(t, []string{"task-0"}, got.Dependencies)
	require.Equal(t, map[string]string{"issue": "124"}, got.Metadata)
	require.Equal(t, "proj-1", got.ProjectID)
	require.True(t, got.AutoSpawn)
	require.Equal(t, "acp", got.Backend)
	require.True(t, got.GpuRequired)
	require.Equal(t, "gpu-1", got.HostAffinity)
	require.NotNil(t, got.Source)
	require.Equal(t, "issue", string(got.Source.Kind))
	require.Equal(t, "124", got.Source.Ref)
	require.Equal(t, "https://example.invalid/124", got.Source.URL)
	require.Equal(t, 3, got.Priority)
	require.Equal(t, 2, got.AttemptBudget)
	require.Equal(t, "claude", got.Runner)
	require.Equal(t, "be brief", got.PromptAppend)

	require.Equal(t, "task-1", task.ID)
	require.Equal(t, "t1", task.TenantID)
	require.Equal(t, "ship it", task.Subject)
	require.Equal(t, "the whole thing", task.Description)
	require.Equal(t, string(orchestration.TaskStatusPending), task.Status)
	require.Equal(t, "alice", task.AssignedTo)
	require.Equal(t, map[string]string{"issue": "124"}, task.Metadata)
	require.Equal(t, int64(1700000000), task.CreatedAt)
}

func TestNameDrift_CreateTask_ServiceRefusal(t *testing.T) {
	svc := &driftTaskCreator{err: errDriftServiceRefused}
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_orchestrator_task_create")

	_, err := client.CreateTask(context.Background(), &orchestrator.CreateTaskRequest{
		TenantID: "t1", Subject: "ship it",
	})
	requireTypedError(t, err)
	require.True(t, sdkerrors.IsInvalidInput(err), "code = %v", sdkerrors.Code(err))
	require.Contains(t, err.Error(), errDriftServiceRefused.Error())
}

func TestNameDrift_DecomposeTask(t *testing.T) {
	stamp := time.Date(2026, 10, 14, 9, 32, 0, 123456789, time.UTC).UnixNano()
	var got *orchestration.DecomposeTaskRequest
	svc := &fleetMockAgentService{mockAgentService: mockAgentService{
		decomposeFn: func(_ context.Context, req *orchestration.DecomposeTaskRequest) (*orchestration.DecomposeTaskResponse, error) {
			got = req
			return &orchestration.DecomposeTaskResponse{
				WorkItems: []*orchestration.WorkItem{{
					ID: "wi-1", ProjectID: "proj-1", Title: "step one", Description: "do it",
					Priority: 2, Dependencies: []string{"wi-0"},
				}},
				Timestamp: stamp,
			}, nil
		},
	}}
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_orchestrator_decompose")

	resp, err := client.DecomposeTask(context.Background(), &orchestrator.DecomposeTaskRequest{
		Backend: "acp", Model: "opus", Task: "build the thing", ProjectIDs: []string{"proj-1", "proj-2"},
	})
	require.NoError(t, err)

	require.NotNil(t, got)
	require.Equal(t, "acp", got.Backend)
	require.Equal(t, "opus", got.Model)
	require.Equal(t, "build the thing", got.Task)
	require.Equal(t, []string{"proj-1", "proj-2"}, got.ProjectIDs)

	require.Equal(t, stamp, resp.Timestamp)
	require.Len(t, resp.WorkItems, 1)
	item := resp.WorkItems[0]
	require.Equal(t, "wi-1", item.ID)
	require.Equal(t, "proj-1", item.ProjectID)
	require.Equal(t, "step one", item.Title)
	require.Equal(t, "do it", item.Description)
	require.Equal(t, 2, item.Priority)
	require.Equal(t, []string{"wi-0"}, item.Dependencies)
}

// The SDK leaves backend optional, but the tool requires it: the refusal
// must reach the caller as a coded error.
func TestNameDrift_DecomposeTask_MissingBackend(t *testing.T) {
	client := serveRegistryOverMCP(t, newFleetRegistry(t, &fleetMockAgentService{}), "quicbaas_orchestrator_decompose")

	_, err := client.DecomposeTask(context.Background(), &orchestrator.DecomposeTaskRequest{Task: "build the thing"})
	requireTypedError(t, err)
	require.Contains(t, err.Error(), "backend")
}

func TestNameDrift_GetTaskArtifact(t *testing.T) {
	var gotTenant, gotTask, gotName string
	svc := &fleetMockAgentService{
		getArtifactFn: func(_ context.Context, tenantID, taskID, name string) (*orchestration.Artifact, error) {
			gotTenant, gotTask, gotName = tenantID, taskID, name
			return &orchestration.Artifact{
				Kind: orchestration.ArtifactKindInline, Name: name, MIMEType: "text/plain",
				Size: 5, Content: []byte("hello"),
				Ref:      orchestration.ArtifactRef{AgentID: "agent-1", Path: "/out.txt", SHA: "abc", Branch: "main"},
				Metadata: map[string]string{"outcome": "ok"},
			}, nil
		},
	}
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_task_get_artifact")

	artifact, err := client.GetTaskArtifact(context.Background(), &orchestrator.GetTaskArtifactRequest{
		TenantID: "t1", TaskID: "task-1", Name: "report",
	})
	require.NoError(t, err)
	require.Equal(t, "t1", gotTenant)
	require.Equal(t, "task-1", gotTask)
	require.Equal(t, "report", gotName)

	require.Equal(t, "inline", artifact.Kind)
	require.Equal(t, "report", artifact.Name)
	require.Equal(t, "text/plain", artifact.MIMEType)
	require.Equal(t, int64(5), artifact.Size)
	require.Equal(t, []byte("hello"), artifact.Content)
	require.NotNil(t, artifact.Ref)
	require.Equal(t, orchestrator.TaskArtifactRef{AgentID: "agent-1", Path: "/out.txt", SHA: "abc", Branch: "main"}, *artifact.Ref)
	require.Equal(t, map[string]string{"outcome": "ok"}, artifact.Metadata)
}

func TestNameDrift_GetTaskArtifact_NotFound(t *testing.T) {
	svc := &fleetMockAgentService{
		getArtifactFn: func(context.Context, string, string, string) (*orchestration.Artifact, error) {
			return nil, nil
		},
	}
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_task_get_artifact")

	_, err := client.GetTaskArtifact(context.Background(), &orchestrator.GetTaskArtifactRequest{
		TenantID: "t1", TaskID: "task-1", Name: "missing",
	})
	requireTypedError(t, err)
	require.True(t, sdkerrors.IsNotFound(err), "code = %v", sdkerrors.Code(err))
}
