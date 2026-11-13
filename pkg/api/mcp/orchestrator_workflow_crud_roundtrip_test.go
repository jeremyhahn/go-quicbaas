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
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// workflowCRUDService answers the workflow verbs from a real in-memory
// WorkflowLoader, so validation and not-found errors are the store's own.
type workflowCRUDService struct {
	fleetMockAgentService
	loader *orchestration.WorkflowLoader
}

func newWorkflowCRUDService() *workflowCRUDService {
	return &workflowCRUDService{loader: orchestration.NewWorkflowLoader()}
}

func (s *workflowCRUDService) GetWorkflow(ctx context.Context, name string) (*orchestration.WorkflowDefinition, error) {
	return s.loader.Get(orchestration.TenantFromContext(ctx), name)
}

func (s *workflowCRUDService) RegisterWorkflow(ctx context.Context, def *orchestration.WorkflowDefinition) error {
	return s.loader.Register(orchestration.TenantFromContext(ctx), def)
}

func (s *workflowCRUDService) UpdateWorkflow(ctx context.Context, def *orchestration.WorkflowDefinition) error {
	return s.loader.Update(orchestration.TenantFromContext(ctx), def)
}

func (s *workflowCRUDService) DeleteWorkflow(ctx context.Context, name string) error {
	return s.loader.Delete(orchestration.TenantFromContext(ctx), name)
}

// sdkWorkflow is a valid constrained workflow as an SDK caller writes it.
func sdkWorkflow(name, description string) *orchestrator.WorkflowDefinition {
	return &orchestrator.WorkflowDefinition{
		Name:        name,
		Description: description,
		Mode:        orchestrator.WorkflowModeConstrained,
		Agents:      []orchestrator.AgentRoleSpec{{Role: "lead", Backend: "claude", Model: "opus"}},
		Settings:    orchestrator.WorkflowSettings{MaxIterations: 3},
	}
}

// storeWorkflow registers a valid workflow for tenant t1 directly in the store.
func storeWorkflow(t *testing.T, svc *workflowCRUDService, name string) {
	t.Helper()
	require.NoError(t, svc.loader.Register("t1", &orchestration.WorkflowDefinition{
		Name:        name,
		Description: "stored",
		Mode:        orchestration.WorkflowModeConstrained,
		Agents:      []orchestration.AgentRoleSpec{{Role: "lead", Backend: "claude", Model: "opus"}},
	}))
}

func requireNotFound(t *testing.T, err error) {
	t.Helper()
	requireTypedError(t, err)
	require.True(t, sdkerrors.IsNotFound(err), "want not found, got %v", err)
}

func TestWorkflowCRUD_CreateWorkflow(t *testing.T) {
	svc := newWorkflowCRUDService()
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_orchestrator_create_workflow")

	wf, err := client.CreateWorkflow(context.Background(), &orchestrator.CreateWorkflowRequest{
		TenantID: "t1", Definition: sdkWorkflow("build", "build it"),
	})
	require.NoError(t, err)
	require.Equal(t, "build", wf.Name)
	require.Equal(t, "build it", wf.Description)
	require.Equal(t, orchestrator.WorkflowModeConstrained, wf.Mode)
	require.Equal(t, 3, wf.Settings.MaxIterations)

	stored, err := svc.loader.Get("t1", "build")
	require.NoError(t, err)
	require.Equal(t, "build it", stored.Description)
	require.Equal(t, "lead", stored.Agents[0].Role)
}

func TestWorkflowCRUD_CreateWorkflow_InvalidDefinition(t *testing.T) {
	svc := newWorkflowCRUDService()
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_orchestrator_create_workflow")

	def := sdkWorkflow("build", "no lead")
	def.Agents[0].Role = "worker"
	_, err := client.CreateWorkflow(context.Background(), &orchestrator.CreateWorkflowRequest{
		TenantID: "t1", Definition: def,
	})
	requireTypedError(t, err)
	require.True(t, sdkerrors.IsInvalidInput(err), "want invalid input, got %v", err)

	_, err = svc.loader.Get("t1", "build")
	require.ErrorIs(t, err, orchestration.ErrWorkflowNotFound, "an invalid definition must not be stored")
}

func TestWorkflowCRUD_CreateWorkflow_AlreadyExists(t *testing.T) {
	svc := newWorkflowCRUDService()
	storeWorkflow(t, svc, "build")
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_orchestrator_create_workflow")

	_, err := client.CreateWorkflow(context.Background(), &orchestrator.CreateWorkflowRequest{
		TenantID: "t1", Definition: sdkWorkflow("build", "duplicate"),
	})
	requireTypedError(t, err)
	require.True(t, sdkerrors.IsAlreadyExists(err), "want already exists, got %v", err)

	stored, err := svc.loader.Get("t1", "build")
	require.NoError(t, err)
	require.Equal(t, "stored", stored.Description, "a duplicate create must not replace the stored workflow")
}

func TestWorkflowCRUD_GetWorkflow(t *testing.T) {
	svc := newWorkflowCRUDService()
	storeWorkflow(t, svc, "build")
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_orchestrator_get_workflow")

	wf, err := client.GetWorkflow(context.Background(), &orchestrator.GetWorkflowRequest{TenantID: "t1", Name: "build"})
	require.NoError(t, err)
	require.Equal(t, "build", wf.Name)
	require.Equal(t, "stored", wf.Description)
	require.Equal(t, []orchestrator.AgentRoleSpec{{Role: "lead", Backend: "claude", Model: "opus"}}, wf.Agents)
}

func TestWorkflowCRUD_GetWorkflow_NotFound(t *testing.T) {
	svc := newWorkflowCRUDService()
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_orchestrator_get_workflow")

	_, err := client.GetWorkflow(context.Background(), &orchestrator.GetWorkflowRequest{TenantID: "t1", Name: "ghost"})
	requireNotFound(t, err)
}

func TestWorkflowCRUD_UpdateWorkflow(t *testing.T) {
	svc := newWorkflowCRUDService()
	storeWorkflow(t, svc, "build")
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_orchestrator_update_workflow")

	// The request's name wins over the definition's own name.
	wf, err := client.UpdateWorkflow(context.Background(), &orchestrator.UpdateWorkflowRequest{
		TenantID: "t1", Name: "build", Definition: sdkWorkflow("other", "rebuilt"),
	})
	require.NoError(t, err)
	require.Equal(t, "build", wf.Name)
	require.Equal(t, "rebuilt", wf.Description)

	stored, err := svc.loader.Get("t1", "build")
	require.NoError(t, err)
	require.Equal(t, "rebuilt", stored.Description)
	_, err = svc.loader.Get("t1", "other")
	require.ErrorIs(t, err, orchestration.ErrWorkflowNotFound)
}

func TestWorkflowCRUD_UpdateWorkflow_NotFound(t *testing.T) {
	svc := newWorkflowCRUDService()
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_orchestrator_update_workflow")

	_, err := client.UpdateWorkflow(context.Background(), &orchestrator.UpdateWorkflowRequest{
		TenantID: "t1", Name: "ghost", Definition: sdkWorkflow("ghost", "never stored"),
	})
	requireNotFound(t, err)
}

func TestWorkflowCRUD_DeleteWorkflow(t *testing.T) {
	svc := newWorkflowCRUDService()
	storeWorkflow(t, svc, "build")
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_orchestrator_delete_workflow")

	err := client.DeleteWorkflow(context.Background(), &orchestrator.DeleteWorkflowRequest{TenantID: "t1", Name: "build"})
	require.NoError(t, err)

	_, err = svc.loader.Get("t1", "build")
	require.ErrorIs(t, err, orchestration.ErrWorkflowNotFound)
}

func TestWorkflowCRUD_DeleteWorkflow_NotFound(t *testing.T) {
	svc := newWorkflowCRUDService()
	client := serveRegistryOverMCP(t, newFleetRegistry(t, svc), "quicbaas_orchestrator_delete_workflow")

	err := client.DeleteWorkflow(context.Background(), &orchestrator.DeleteWorkflowRequest{TenantID: "t1", Name: "ghost"})
	requireNotFound(t, err)
}

// The SDK client refuses a missing name or definition before the wire, so
// the tools' own argument checks are exercised directly.
func TestWorkflowCRUD_ToolsRejectMissingArguments(t *testing.T) {
	reg := newFleetRegistry(t, newWorkflowCRUDService())
	ctx := context.Background()

	cases := map[string]map[string]interface{}{
		"quicbaas_orchestrator_get_workflow":    {},
		"quicbaas_orchestrator_delete_workflow": {},
		"quicbaas_orchestrator_create_workflow": {"name": "build"},
		"quicbaas_orchestrator_update_workflow": {"name": "build"},
	}
	for tool, args := range cases {
		_, err := reg.ExecuteTool(ctx, tool, args)
		require.Error(t, err, tool)
	}

	_, err := reg.ExecuteTool(ctx, "quicbaas_orchestrator_create_workflow", map[string]interface{}{
		"definition": map[string]interface{}{"mode": "constrained"},
	})
	require.Error(t, err, "a definition with no name and no name argument must be refused")

	_, err = reg.ExecuteTool(ctx, "quicbaas_orchestrator_update_workflow", map[string]interface{}{
		"name": "build", "definition": "not an object",
	})
	require.Error(t, err, "a definition that is not an object must be refused")
}

// A definition still setting the retired system_prompt is refused, not
// silently dropped. The SDK client no longer has the field, so the tools are
// called with the raw arguments an older caller sends.
func TestWorkflowCRUD_ToolsRefuseRetiredSystemPrompt(t *testing.T) {
	svc := newWorkflowCRUDService()
	storeWorkflow(t, svc, "build")
	reg := newFleetRegistry(t, svc)

	for _, tool := range []string{"quicbaas_orchestrator_create_workflow", "quicbaas_orchestrator_update_workflow"} {
		_, err := reg.ExecuteTool(tenantctx.WithTenant(context.Background(), "t1"), tool, map[string]interface{}{
			"tenant_id": "t1",
			"name":      "build",
			"definition": map[string]interface{}{
				"name": "build", "mode": "constrained", "description": "retired",
				"agents": []interface{}{map[string]interface{}{
					"role": "lead", "backend": "claude", "system_prompt": "be careful",
				}},
			},
		})
		require.ErrorIs(t, err, orchestration.ErrWorkflowRetiredField, tool)
	}

	stored, err := svc.loader.Get("t1", "build")
	require.NoError(t, err)
	require.Equal(t, "stored", stored.Description, "a refused definition must not be stored")
}

// The workflow tools require the permissions the REST routes declare.
func TestWorkflowCRUD_Permissions(t *testing.T) {
	want := map[string]qrdbToolResource{
		"quicbaas_orchestrator_get_workflow":    {"orchestrator", "workflow_read"},
		"quicbaas_orchestrator_create_workflow": {"orchestrator", "workflow_write"},
		"quicbaas_orchestrator_update_workflow": {"orchestrator", "workflow_write"},
		"quicbaas_orchestrator_delete_workflow": {"orchestrator", "workflow_write"},
	}
	for tool, perm := range want {
		got, ok := qrdbToolExactMapping[tool]
		require.True(t, ok, "%s has no permission", tool)
		require.Equal(t, perm, got, tool)
	}
}
